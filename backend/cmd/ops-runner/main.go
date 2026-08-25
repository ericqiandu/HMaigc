package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsrunner"
	"infinite-canvas/backend/internal/opsstate"
)

const (
	defaultStateRoot  = "/var/lib/hmaigc-ops"
	defaultStagePath  = "/opt/hmaigc/deploy/hmaigc-stage.sh"
	maximumSecretSize = 4 << 10
	heartbeatInterval = 5 * time.Second
	leaseLifetime     = 20 * time.Second
)

type runnerOptions struct {
	operationID string
	generation  uint64
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) error {
	options, showHelp, err := parseOptions(arguments, stderr)
	if err != nil {
		return err
	}
	if showHelp {
		writeUsage(stdout)
		return nil
	}

	stateRoot := environment("HMAIGC_OPS_STATE_DIR", defaultStateRoot)
	journal, err := opsstate.NewJournal(stateRoot)
	if err != nil {
		return fmt.Errorf("open operations journal: %w", err)
	}
	secret, err := readSecret(environment("HMAIGC_OPS_SHARED_SECRET_FILE", filepath.Join(stateRoot, "shared-secret")))
	if err != nil {
		return err
	}
	launch, err := journal.ReadLaunchCommand(secret, options.operationID)
	if err != nil {
		return fmt.Errorf("validate signed launch command: %w", err)
	}
	if launch.Generation != options.generation {
		return fmt.Errorf("signed launch generation mismatch: command=%d process=%d", launch.Generation, options.generation)
	}

	lockPath := filepath.Join(stateRoot, "deploy.lock")
	lock, err := opsrunner.AcquireDeploymentLock(lockPath)
	if err != nil {
		if errors.Is(err, opsrunner.ErrDeploymentLockHeld) {
			return fmt.Errorf("%s: %w", opsprotocol.ErrorStateConflict, err)
		}
		return err
	}
	defer lock.Close()
	if err := lock.RecordOwner(options.operationID, options.generation); err != nil {
		return err
	}

	now := time.Now().UTC()
	lease := opsprotocol.RunnerLease{
		OperationID:  options.operationID,
		Generation:   options.generation,
		TokenHash:    hashToken(launch.FencingToken),
		RunnerDigest: launch.RunnerDigest,
		AcquiredAt:   now,
		ExpiresAt:    now.Add(leaseLifetime),
	}
	if err := journal.WriteLease(options.operationID, lease); err != nil {
		return fmt.Errorf("persist runner lease: %w", err)
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancelRunner := context.WithCancel(signalContext)
	defer cancelRunner()
	heartbeatContext, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go maintainHeartbeat(heartbeatContext, heartbeatDone, cancelRunner, journal, lease)

	runtime := opsrunner.NewShellRuntime(environment("HMAIGC_STAGE_SCRIPT", defaultStagePath), nil)
	engine := opsrunner.NewEngine(journal, runtime, time.Now)
	runErr := engine.Run(ctx, opsrunner.RunInput{
		OperationID:    options.operationID,
		Generation:     options.generation,
		FencingToken:   launch.FencingToken,
		RunnerDigest:   launch.RunnerDigest,
		RecoveryAction: launch.RecoveryAction,
		CommandSecret:  secret,
	})
	stopHeartbeat()
	<-heartbeatDone
	if runErr != nil {
		return fmt.Errorf("runner execution failed: %w", runErr)
	}
	return nil
}

func parseOptions(arguments []string, output io.Writer) (runnerOptions, bool, error) {
	flags := flag.NewFlagSet("hmaigc-ops-runner", flag.ContinueOnError)
	flags.SetOutput(output)
	operationID := flags.String("operation-id", "", "durable operation identifier")
	generation := flags.Uint64("generation", 0, "runner fencing generation")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return runnerOptions{}, true, nil
		}
		return runnerOptions{}, false, err
	}
	if flags.NArg() != 0 {
		return runnerOptions{}, false, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *operationID == "" || *generation == 0 {
		return runnerOptions{}, false, errors.New("--operation-id and --generation are required")
	}
	return runnerOptions{operationID: *operationID, generation: *generation}, false, nil
}

func writeUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage: hmaigc-ops-runner --operation-id <id> --generation <positive integer>")
}

func maintainHeartbeat(
	ctx context.Context,
	done chan<- struct{},
	cancelRunner context.CancelFunc,
	journal *opsstate.Journal,
	lease opsprotocol.RunnerLease,
) {
	defer close(done)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	writeHeartbeat(journal, lease)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lease.ExpiresAt = time.Now().UTC().Add(leaseLifetime)
			if err := journal.WriteLease(lease.OperationID, lease); err != nil {
				fmt.Fprintf(os.Stderr, "renew runner lease: %v\n", err)
				cancelRunner()
				return
			}
			writeHeartbeat(journal, lease)
		}
	}
}

func writeHeartbeat(journal *opsstate.Journal, lease opsprotocol.RunnerLease) {
	checkpoint, err := journal.ReadCheckpoint(lease.OperationID)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "read runner checkpoint for heartbeat: %v\n", err)
		}
		return
	}
	heartbeat := opsprotocol.RunnerHeartbeat{
		OperationID:  lease.OperationID,
		Generation:   lease.Generation,
		Sequence:     checkpoint.Sequence,
		Stage:        checkpoint.Stage,
		ServiceState: checkpoint.ServiceState,
		ObservedAt:   time.Now().UTC(),
	}
	if err := journal.WriteHeartbeat(lease.OperationID, heartbeat); err != nil {
		fmt.Fprintf(os.Stderr, "persist runner heartbeat: %v\n", err)
	}
}

func readSecret(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open command signing secret: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximumSecretSize+1))
	if err != nil {
		return nil, fmt.Errorf("read command signing secret: %w", err)
	}
	if len(data) > maximumSecretSize {
		return nil, errors.New("command signing secret exceeds the maximum size")
	}
	secret := []byte(strings.TrimSpace(string(data)))
	if len(secret) < 32 {
		return nil, errors.New("command signing secret must contain at least 32 bytes")
	}
	return secret, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func environment(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
