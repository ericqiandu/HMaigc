package opsrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

const maxCommandOutputBytes = 1 << 20

type CommandOutput struct {
	Stdout []byte
	Stderr []byte
}

type commandRunner interface {
	Run(context.Context, string, []string, []string) (CommandOutput, error)
}

type ShellRuntime struct {
	ScriptPath  string
	Environment []string
	Timeouts    map[opsprotocol.OperationStage]time.Duration
	command     commandRunner
}

func NewShellRuntime(scriptPath string, environment []string) *ShellRuntime {
	return &ShellRuntime{
		ScriptPath:  scriptPath,
		Environment: append([]string(nil), environment...),
		Timeouts:    defaultStageTimeouts(),
		command:     osCommandRunner{},
	}
}

func (r *ShellRuntime) Execute(ctx context.Context, input StageInput) (StageOutput, error) {
	if r == nil {
		return StageOutput{}, errors.New("shell runtime is nil")
	}
	if strings.TrimSpace(r.ScriptPath) == "" {
		return StageOutput{}, errors.New("stage script path is required")
	}
	command := r.command
	if command == nil {
		command = osCommandRunner{}
	}
	arguments, err := stageArguments(input)
	if err != nil {
		return StageOutput{}, err
	}
	timeout := r.stageTimeout(input.Stage)
	stageContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, runErr := command.Run(stageContext, r.ScriptPath, arguments, append([]string(nil), r.Environment...))
	if runErr != nil {
		if stageContext.Err() != nil {
			return StageOutput{}, fmt.Errorf("stage %s exceeded %s: %w", input.Stage, timeout, stageContext.Err())
		}
		return StageOutput{}, fmt.Errorf("stage %s failed: %w: %s", input.Stage, runErr, boundedText(result.Stderr))
	}
	output, err := decodeStageOutput(result.Stdout)
	if err != nil {
		return StageOutput{}, fmt.Errorf("stage %s returned invalid facts: %w", input.Stage, err)
	}
	return output, nil
}

func (r *ShellRuntime) stageTimeout(stage opsprotocol.OperationStage) time.Duration {
	if timeout := r.Timeouts[stage]; timeout > 0 {
		return timeout
	}
	if timeout := defaultStageTimeouts()[stage]; timeout > 0 {
		return timeout
	}
	return 2 * time.Minute
}

func stageArguments(input StageInput) ([]string, error) {
	command, err := stageCommand(input.Stage)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Request.OperationID) == "" {
		return nil, errors.New("operation id is required")
	}
	if input.Checkpoint.Generation == 0 {
		return nil, errors.New("runner generation is required")
	}
	arguments := []string{
		command,
		"--operation-id", input.Request.OperationID,
		"--generation", strconv.FormatUint(input.Checkpoint.Generation, 10),
		"--current-version", input.Request.CurrentVersion,
		"--target-version", stageTargetVersion(input.Request),
		"--backend-image", input.Checkpoint.BackendImage,
		"--web-image", input.Checkpoint.WebImage,
	}
	arguments = appendOptionalArgument(arguments, "--backup-helper-image", input.Checkpoint.BackupHelperImage)
	arguments = appendOptionalArgument(arguments, "--backup-path", stageBackupPath(input))
	if input.Stage == opsprotocol.StageBackingUp {
		arguments = appendOptionalArgument(arguments, "--protected-backup-path", input.Request.RollbackBackup)
	}
	controllerImage := input.Checkpoint.CandidateControllerImage
	if input.Request.Request.Action == opsprotocol.ActionRollback {
		controllerImage = input.Checkpoint.RunnerDigest
	}
	if controllerImage == "" {
		controllerImage = input.Checkpoint.RunnerDigest
	}
	arguments = appendOptionalArgument(arguments, "--controller-image", controllerImage)
	return arguments, nil
}

func stageBackupPath(input StageInput) string {
	if input.Stage == opsprotocol.StageRestoringRollbackBackup {
		return input.Request.RollbackBackup
	}
	return input.Checkpoint.BackupPath
}

func stageTargetVersion(request opsprotocol.OperationRequestFile) string {
	switch request.Request.Action {
	case opsprotocol.ActionInstall, opsprotocol.ActionUpgrade, opsprotocol.ActionRollback:
		return request.ExpectedVersion
	default:
		return ""
	}
}

func appendOptionalArgument(arguments []string, name, value string) []string {
	if value == "" {
		return arguments
	}
	return append(arguments, name, value)
}

func stageCommand(stage opsprotocol.OperationStage) (string, error) {
	commands := map[opsprotocol.OperationStage]string{
		opsprotocol.StageOnlinePreflight:         "online-preflight",
		opsprotocol.StagePublicVerifying:         "public-verify",
		opsprotocol.StageQuiescing:               "quiesce",
		opsprotocol.StageQuiescedAudit:           "quiesced-audit",
		opsprotocol.StageBackingUp:               "backup",
		opsprotocol.StageStartingTarget:          "start-target",
		opsprotocol.StageVerifyingTarget:         "verify-target",
		opsprotocol.StageRestoringCurrent:        "restore-current",
		opsprotocol.StageRestoringBackup:         "restore-backup",
		opsprotocol.StageRestoringRollbackBackup: "restore-rollback-backup",
		opsprotocol.StageCommittingRelease:       "commit-release",
		opsprotocol.StageControllerHandoff:       "handoff-controller",
	}
	command, ok := commands[stage]
	if !ok {
		return "", fmt.Errorf("unsupported runtime stage %q", stage)
	}
	return command, nil
}

func decodeStageOutput(data []byte) (StageOutput, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var output StageOutput
	if err := decoder.Decode(&output); err != nil {
		return StageOutput{}, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return StageOutput{}, err
	}
	if !validServiceState(output.ServiceState) {
		return StageOutput{}, fmt.Errorf("invalid service state %q", output.ServiceState)
	}
	return output, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("stage output contains trailing JSON")
}

func validServiceState(state opsprotocol.ServiceState) bool {
	switch state {
	case opsprotocol.ServiceCurrentOnline,
		opsprotocol.ServiceMaintenance,
		opsprotocol.ServiceTargetOnline,
		opsprotocol.ServiceCurrentRestored,
		opsprotocol.ServiceUnknown:
		return true
	default:
		return false
	}
}

func defaultStageTimeouts() map[opsprotocol.OperationStage]time.Duration {
	return map[opsprotocol.OperationStage]time.Duration{
		opsprotocol.StageOnlinePreflight:         2 * time.Minute,
		opsprotocol.StagePublicVerifying:         2 * time.Minute,
		opsprotocol.StageQuiescing:               2 * time.Minute,
		opsprotocol.StageQuiescedAudit:           2 * time.Minute,
		opsprotocol.StageBackingUp:               20 * time.Minute,
		opsprotocol.StageStartingTarget:          10 * time.Minute,
		opsprotocol.StageVerifyingTarget:         5 * time.Minute,
		opsprotocol.StageRestoringCurrent:        10 * time.Minute,
		opsprotocol.StageRestoringBackup:         20 * time.Minute,
		opsprotocol.StageRestoringRollbackBackup: 20 * time.Minute,
		opsprotocol.StageCommittingRelease:       2 * time.Minute,
		opsprotocol.StageControllerHandoff:       5 * time.Minute,
	}
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, program string, arguments []string, environment []string) (CommandOutput, error) {
	command := exec.CommandContext(ctx, program, arguments...)
	command.Env = append(os.Environ(), environment...)
	var stdout limitedBuffer
	var stderr limitedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return CommandOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := maxCommandOutputBytes - b.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.buffer.Write(data)
	}
	return originalLength, nil
}

func (b *limitedBuffer) Bytes() []byte {
	return append([]byte(nil), b.buffer.Bytes()...)
}

func boundedText(data []byte) string {
	if len(data) == 0 {
		return "no stderr"
	}
	if len(data) > maxCommandOutputBytes {
		data = data[:maxCommandOutputBytes]
	}
	return strings.TrimSpace(string(data))
}
