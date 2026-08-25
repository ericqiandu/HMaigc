package opsrunner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestShellRuntimeMapsStageAndStrictlyDecodesFacts(t *testing.T) {
	t.Parallel()

	command := &recordingCommand{
		stdout: []byte(`{"serviceState":"target_online","resultVersion":"v1.0.58","targetBackendHealthy":true,"targetWebHealthy":true}`),
	}
	runtime := &ShellRuntime{
		ScriptPath:  "/opt/hmaigc/deploy/hmaigc-stage.sh",
		Environment: []string{"HMAIGC_ENV_FILE=/state/production.env"},
		Timeouts: map[opsprotocol.OperationStage]time.Duration{
			opsprotocol.StageVerifyingTarget: time.Minute,
		},
		command: command,
	}
	output, err := runtime.Execute(context.Background(), StageInput{
		Request: opsprotocol.OperationRequestFile{
			OperationID:     "op-shell",
			Request:         opsprotocol.StartOperationRequest{Action: opsprotocol.ActionUpgrade, TargetVersion: "v1.0.58"},
			CurrentVersion:  "v1.0.57",
			ExpectedVersion: "v1.0.58",
		},
		Checkpoint: opsprotocol.OperationCheckpoint{
			OperationID: "op-shell", Generation: 4,
			RunnerDigest:      "example.invalid/ops@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			BackendImage:      "example.invalid/backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			WebImage:          "example.invalid/web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			BackupHelperImage: "example.invalid/backup@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
		Stage: opsprotocol.StageVerifyingTarget, FencingToken: "must-not-enter-argv-or-env",
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.ServiceState != opsprotocol.ServiceTargetOnline || !output.TargetBackendHealthy || !output.TargetWebHealthy {
		t.Fatalf("output=%+v", output)
	}
	wantArguments := []string{
		"verify-target",
		"--operation-id", "op-shell",
		"--generation", "4",
		"--current-version", "v1.0.57",
		"--target-version", "v1.0.58",
		"--backend-image", "example.invalid/backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--web-image", "example.invalid/web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--backup-helper-image", "example.invalid/backup@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"--controller-image", "example.invalid/ops@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	if command.program != runtime.ScriptPath || !reflect.DeepEqual(command.arguments, wantArguments) {
		t.Fatalf("program=%q arguments=%v", command.program, command.arguments)
	}
	for _, value := range append(command.arguments, command.environment...) {
		if value == "must-not-enter-argv-or-env" {
			t.Fatal("raw fencing token escaped into the child process metadata")
		}
	}
}

func TestStageArgumentsUseExpectedVersionOnlyForVersionChangingActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		action     opsprotocol.Action
		expected   string
		wantTarget string
	}{
		{name: "upgrade", action: opsprotocol.ActionUpgrade, expected: "v1.0.58", wantTarget: "v1.0.58"},
		{name: "rollback", action: opsprotocol.ActionRollback, expected: "v1.0.56", wantTarget: "v1.0.56"},
		{name: "backup", action: opsprotocol.ActionBackup, expected: "v1.0.57"},
		{name: "verify", action: opsprotocol.ActionVerify, expected: "v1.0.57"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			arguments, err := stageArguments(StageInput{
				Request: opsprotocol.OperationRequestFile{
					OperationID: "op-target-args", ExpectedVersion: test.expected,
					Request: opsprotocol.StartOperationRequest{Action: test.action},
				},
				Checkpoint: opsprotocol.OperationCheckpoint{Generation: 1},
				Stage:      opsprotocol.StageStartingTarget,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := argumentValue(arguments, "--target-version")
			if got != test.wantTarget {
				t.Fatalf("target argument=%q want=%q args=%v", got, test.wantTarget, arguments)
			}
		})
	}
}

func TestRollbackStagesUseDistinctRecoveryPoints(t *testing.T) {
	t.Parallel()

	request := opsprotocol.OperationRequestFile{
		OperationID:    "op-rollback-paths",
		Request:        opsprotocol.StartOperationRequest{Action: opsprotocol.ActionRollback},
		CurrentVersion: "v1.0.57", ExpectedVersion: "v1.0.56",
		RollbackBackup: "/var/lib/hmaigc-ops/backups/old-v1.0.56",
	}
	checkpoint := opsprotocol.OperationCheckpoint{
		Generation: 1, BackupPath: "/var/lib/hmaigc-ops/backups/current-v1.0.57",
		RunnerDigest:             "example.invalid/ops@sha256:" + strings.Repeat("a", 64),
		CandidateControllerImage: "example.invalid/ops@sha256:" + strings.Repeat("b", 64),
	}
	tests := []struct {
		name          string
		stage         opsprotocol.OperationStage
		wantBackup    string
		wantProtected string
	}{
		{name: "current safety backup protects rollback source", stage: opsprotocol.StageBackingUp,
			wantBackup: checkpoint.BackupPath, wantProtected: request.RollbackBackup},
		{name: "rollback restores old source", stage: opsprotocol.StageRestoringRollbackBackup,
			wantBackup: request.RollbackBackup},
		{name: "failure recovery restores current safety backup", stage: opsprotocol.StageRestoringBackup,
			wantBackup: checkpoint.BackupPath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arguments, err := stageArguments(StageInput{Request: request, Checkpoint: checkpoint, Stage: test.stage})
			if err != nil {
				t.Fatal(err)
			}
			if got := argumentValue(arguments, "--backup-path"); got != test.wantBackup {
				t.Fatalf("backup path=%q want=%q args=%v", got, test.wantBackup, arguments)
			}
			if got := argumentValue(arguments, "--protected-backup-path"); got != test.wantProtected {
				t.Fatalf("protected path=%q want=%q args=%v", got, test.wantProtected, arguments)
			}
			if got := argumentValue(arguments, "--controller-image"); got != checkpoint.RunnerDigest {
				t.Fatalf("rollback attempted to downgrade the stable controller: got=%q want=%q", got, checkpoint.RunnerDigest)
			}
		})
	}
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}

func TestShellRuntimeRejectsUnknownOutputFields(t *testing.T) {
	t.Parallel()

	runtime := &ShellRuntime{
		ScriptPath: "/opt/hmaigc/deploy/hmaigc-stage.sh",
		command:    &recordingCommand{stdout: []byte(`{"serviceState":"current_online","pretendSuccess":true}`)},
	}
	_, err := runtime.Execute(context.Background(), StageInput{
		Request:    opsprotocol.OperationRequestFile{OperationID: "op-shell"},
		Checkpoint: opsprotocol.OperationCheckpoint{OperationID: "op-shell", Generation: 1},
		Stage:      opsprotocol.StageOnlinePreflight,
	})
	if err == nil {
		t.Fatal("unknown stage output field was accepted")
	}
}

func TestShellRuntimeEnforcesPerStageTimeout(t *testing.T) {
	t.Parallel()

	runtime := &ShellRuntime{
		ScriptPath: "/opt/hmaigc/deploy/hmaigc-stage.sh",
		Timeouts: map[opsprotocol.OperationStage]time.Duration{
			opsprotocol.StagePublicVerifying: 20 * time.Millisecond,
		},
		command: &recordingCommand{waitForContext: true},
	}
	_, err := runtime.Execute(context.Background(), StageInput{
		Request:    opsprotocol.OperationRequestFile{OperationID: "op-timeout"},
		Checkpoint: opsprotocol.OperationCheckpoint{OperationID: "op-timeout", Generation: 1},
		Stage:      opsprotocol.StagePublicVerifying,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

type recordingCommand struct {
	program        string
	arguments      []string
	environment    []string
	stdout         []byte
	stderr         []byte
	err            error
	waitForContext bool
}

func (c *recordingCommand) Run(ctx context.Context, program string, arguments []string, environment []string) (CommandOutput, error) {
	c.program = program
	c.arguments = append([]string(nil), arguments...)
	c.environment = append([]string(nil), environment...)
	if c.waitForContext {
		<-ctx.Done()
		return CommandOutput{}, ctx.Err()
	}
	return CommandOutput{Stdout: append([]byte(nil), c.stdout...), Stderr: append([]byte(nil), c.stderr...)}, c.err
}
