package opsrunner

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"
)

func TestUpgradePersistsFactBeforeNextStage(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	want := []opsprotocol.OperationStage{
		opsprotocol.StageOnlinePreflight,
		opsprotocol.StageQuiescing,
		opsprotocol.StageQuiescedAudit,
		opsprotocol.StageBackingUp,
		opsprotocol.StageStartingTarget,
		opsprotocol.StageVerifyingTarget,
		opsprotocol.StageCommittingRelease,
		opsprotocol.StageControllerHandoff,
	}
	if !reflect.DeepEqual(want, fixture.runtime.stages) {
		t.Fatalf("want=%v got=%v", want, fixture.runtime.stages)
	}
	for index, input := range fixture.runtime.inputs {
		if index == 0 {
			if input.Checkpoint.Stage != opsprotocol.StageAccepted {
				t.Fatalf("first checkpoint=%+v", input.Checkpoint)
			}
			continue
		}
		if input.Checkpoint.Stage != want[index-1] || input.Checkpoint.StageCompletedAt == nil {
			t.Fatalf("stage %s started without completed checkpoint for %s: %+v", want[index], want[index-1], input.Checkpoint)
		}
	}
	events, err := fixture.journal.ReadEvents(fixture.input.OperationID, fixture.input.Generation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(want)*2 {
		t.Fatalf("events=%+v", events)
	}
	for index := range want {
		intent := events[index*2]
		fact := events[index*2+1]
		if intent.Kind != EventKindIntent || fact.Kind != EventKindFact || intent.Stage != want[index] || fact.Stage != want[index] {
			t.Fatalf("stage events=%+v %+v", intent, fact)
		}
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationSucceeded || result.ResultVersion != fixture.request.Request.TargetVersion {
		t.Fatalf("result=%+v", result)
	}
}

func TestRollbackUsesPersistedExpectedVersionWhenUserRequestHasNoTarget(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionRollback)
	if fixture.request.Request.TargetVersion != "" || fixture.request.ExpectedVersion != "v1.0.56" {
		t.Fatalf("invalid rollback fixture: %+v", fixture.request)
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	wantStages := []opsprotocol.OperationStage{
		opsprotocol.StageOnlinePreflight,
		opsprotocol.StageQuiescing,
		opsprotocol.StageBackingUp,
		opsprotocol.StageRestoringRollbackBackup,
		opsprotocol.StageStartingTarget,
		opsprotocol.StageVerifyingTarget,
		opsprotocol.StageCommittingRelease,
		opsprotocol.StageControllerHandoff,
	}
	if !slices.Equal(fixture.runtime.stages, wantStages) {
		t.Fatalf("rollback stages=%v want=%v", fixture.runtime.stages, wantStages)
	}
	for _, input := range fixture.runtime.inputs {
		if input.Checkpoint.ExpectedVersion != "v1.0.56" {
			t.Fatalf("stage %s lost expected version: %+v", input.Stage, input.Checkpoint)
		}
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationSucceeded || result.ResultVersion != "v1.0.56" {
		t.Fatalf("rollback result=%+v", result)
	}
}

func TestStartingTargetFailureWithoutStageFactsRestoresVerifiedBackup(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	fixture.runtime.failuresWithoutOutput = map[opsprotocol.OperationStage]error{
		opsprotocol.StageStartingTarget: errors.New("shell exited after migration began"),
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fixture.runtime.stages, opsprotocol.StageRestoringBackup) {
		t.Fatalf("destructive failure did not restore safety backup: %v", fixture.runtime.stages)
	}
	checkpoint, err := fixture.journal.ReadCheckpoint(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if !checkpoint.DataMigrationStarted {
		t.Fatalf("destructive stage entry was not persisted conservatively: %+v", checkpoint)
	}
}

func TestRunnerRejectsCheckpointThatDivergesFromImmutableExpectedVersion(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	checkpoint := opsprotocol.OperationCheckpoint{
		OperationID: fixture.input.OperationID, Action: opsprotocol.ActionUpgrade,
		TargetVersion: "v1.0.99", RunnerDigest: fixture.input.RunnerDigest,
		Generation: fixture.input.Generation, FencingTokenHash: hashToken(fixture.input.FencingToken),
		Stage: opsprotocol.StageAccepted, CurrentVersion: fixture.request.CurrentVersion,
		ExpectedVersion: "v1.0.99", ServiceState: opsprotocol.ServiceCurrentOnline,
		UpdatedAt: time.Date(2026, 8, 25, 1, 30, 0, 0, time.UTC),
	}
	if err := fixture.journal.WriteCheckpoint(fixture.input.OperationID, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); !errors.Is(err, opsstate.ErrCorruptFact) {
		t.Fatalf("expected corrupt checkpoint rejection, got %v", err)
	}
}

func TestStageOutputCannotRewriteImmutableExpectedVersion(t *testing.T) {
	t.Parallel()

	checkpoint := opsprotocol.OperationCheckpoint{ExpectedVersion: "v1.0.58"}
	advanceCheckpoint(
		&checkpoint,
		opsprotocol.StageOnlinePreflight,
		StageOutput{ResultVersion: "v1.0.99"},
		time.Date(2026, 8, 25, 1, 30, 0, 0, time.UTC),
		2,
	)
	if checkpoint.ExpectedVersion != "v1.0.58" {
		t.Fatalf("stage output rewrote expected version: %+v", checkpoint)
	}
}

func TestRunnerRestartResumesAfterEveryCompletedCheckpoint(t *testing.T) {
	t.Parallel()

	stages, err := stagesFor(opsprotocol.ActionUpgrade)
	if err != nil {
		t.Fatal(err)
	}
	for completedIndex, completedStage := range stages {
		completedIndex := completedIndex
		completedStage := completedStage
		t.Run(string(completedStage), func(t *testing.T) {
			t.Parallel()

			fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
			completedAt := time.Date(2026, 8, 25, 1, 30, 0, 0, time.UTC)
			checkpoint := opsprotocol.OperationCheckpoint{
				OperationID: fixture.input.OperationID, Action: opsprotocol.ActionUpgrade,
				TargetVersion: fixture.request.Request.TargetVersion, RunnerDigest: fixture.input.RunnerDigest,
				Generation: fixture.input.Generation, FencingTokenHash: hashToken(fixture.input.FencingToken),
				Stage: opsprotocol.StageAccepted, CurrentVersion: fixture.request.CurrentVersion,
				ExpectedVersion: fixture.request.Request.TargetVersion,
				ServiceState:    opsprotocol.ServiceCurrentOnline, UpdatedAt: completedAt,
			}
			for index := 0; index <= completedIndex; index++ {
				stage := stages[index]
				intentSequence := uint64(index*2 + 1)
				factSequence := intentSequence + 1
				for _, event := range []opsprotocol.OperationEvent{
					{OperationID: fixture.input.OperationID, Generation: fixture.input.Generation, Sequence: intentSequence, Kind: EventKindIntent, Stage: stage, Stream: "system", Message: "stage intent persisted", CreatedAt: completedAt},
					{OperationID: fixture.input.OperationID, Generation: fixture.input.Generation, Sequence: factSequence, Kind: EventKindFact, Stage: stage, Stream: "system", Message: "stage fact persisted", CreatedAt: completedAt},
				} {
					if err := fixture.journal.AppendEvent(event); err != nil {
						t.Fatal(err)
					}
				}
				output := outputForStage(StageInput{Request: fixture.request, Checkpoint: checkpoint, Stage: stage})
				advanceCheckpoint(&checkpoint, stage, output, completedAt, factSequence)
			}
			if err := fixture.journal.WriteCheckpoint(fixture.input.OperationID, checkpoint); err != nil {
				t.Fatal(err)
			}

			if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
				t.Fatal(err)
			}
			wantRemaining := stages[completedIndex+1:]
			if !slices.Equal(wantRemaining, fixture.runtime.stages) {
				t.Fatalf("completed %s: want remaining=%v got=%v", completedStage, wantRemaining, fixture.runtime.stages)
			}
			result, err := fixture.journal.ReadResult(fixture.input.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != opsprotocol.OperationSucceeded || result.ResultVersion != fixture.request.Request.TargetVersion {
				t.Fatalf("completed %s: result=%+v", completedStage, result)
			}
		})
	}
}

func TestCancellationAfterQuiesceRestoresCurrentBeforeCancelled(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	fixture.runtime.cancelAt = opsprotocol.StageQuiescing
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationCancelled || result.ServiceState != opsprotocol.ServiceCurrentRestored {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Contains(fixture.runtime.stages, opsprotocol.StageRestoringCurrent) {
		t.Fatalf("current release was not restored: %v", fixture.runtime.stages)
	}
	if slices.Contains(fixture.runtime.stages, opsprotocol.StageQuiescedAudit) {
		t.Fatalf("runner continued beyond the cancellation safe point: %v", fixture.runtime.stages)
	}
}

func TestCancellationBeforeMaintenanceDoesNotExecuteAnyStage(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	if err := fixture.journal.WriteCancelCommand(fixture.input.CommandSecret, opsprotocol.CancelOperationCommand{
		OperationID: fixture.input.OperationID, RequestHash: strings.Repeat("f", 64),
		ActorUserID: "admin", ActorDisplayName: "管理员",
		RequestedAt: time.Now().UTC(), Nonce: "cancel-before-start",
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	if len(fixture.runtime.stages) != 0 {
		t.Fatalf("runtime stages=%v", fixture.runtime.stages)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationCancelled || result.ServiceState != opsprotocol.ServiceCurrentOnline {
		t.Fatalf("result=%+v", result)
	}
}

func TestFailureAfterDataRewriteRestoresVerifiedBackup(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	fixture.runtime.failAt = opsprotocol.StageStartingTarget
	fixture.runtime.failure = errors.New("migration failed")
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationFailed || result.ServiceState != opsprotocol.ServiceCurrentRestored {
		t.Fatalf("result=%+v", result)
	}
	if !slices.Contains(fixture.runtime.stages, opsprotocol.StageRestoringBackup) {
		t.Fatalf("verified backup was not restored: %v", fixture.runtime.stages)
	}
}

func TestRestoreFailureRequiresOperatorAndPreservesEvidence(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	fixture.runtime.failures = map[opsprotocol.OperationStage]error{
		opsprotocol.StageStartingTarget:  errors.New("migration failed"),
		opsprotocol.StageRestoringBackup: errors.New("restore checksum mismatch"),
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationRecoveryRequired ||
		result.ServiceState != opsprotocol.ServiceUnknown ||
		result.ErrorCode != opsprotocol.ErrorRestoreFailed {
		t.Fatalf("result=%+v", result)
	}
	checkpoint, err := fixture.journal.ReadCheckpoint(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.NextSafeAction != opsprotocol.RecoveryRequireOperator || checkpoint.BackupPath == "" {
		t.Fatalf("checkpoint=%+v", checkpoint)
	}
}

func TestControllerHandoffRestoredPreviousRemainsSuccessfulWarning(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	fixture.runtime.controllerHandoff = opsprotocol.ControllerHandoffRestoredPrevious
	fixture.runtime.warnings = []opsprotocol.OperationWarning{{
		Code: opsprotocol.ErrorControllerHandoffFailed, Message: "目标控制器失败，已恢复旧控制器",
	}}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationSucceeded || result.ControllerHandoff != opsprotocol.ControllerHandoffRestoredPrevious {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != opsprotocol.ErrorControllerHandoffFailed {
		t.Fatalf("warnings=%+v", result.Warnings)
	}
}

func TestRunnerRefusesProductionStageAfterLeaseExpires(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	lease, err := fixture.journal.ReadLease(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	lease.ExpiresAt = time.Date(2026, 8, 25, 1, 59, 59, 0, time.UTC)
	if err := fixture.journal.WriteLease(fixture.input.OperationID, lease); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); !errors.Is(err, ErrRunnerLeaseLost) {
		t.Fatalf("expected lease loss, got %v", err)
	}
	if len(fixture.runtime.stages) != 0 {
		t.Fatalf("expired runner mutated production stages: %v", fixture.runtime.stages)
	}
}

func TestRestartAfterDestructiveIntentDoesNotBlindlyRepeatStage(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	completedAt := time.Date(2026, 8, 25, 1, 30, 0, 0, time.UTC)
	checkpoint := opsprotocol.OperationCheckpoint{
		OperationID: fixture.input.OperationID, Action: opsprotocol.ActionUpgrade,
		TargetVersion: fixture.request.Request.TargetVersion, RunnerDigest: fixture.input.RunnerDigest,
		Generation: fixture.input.Generation, FencingTokenHash: hashToken(fixture.input.FencingToken),
		Stage: opsprotocol.StagePublicVerifying, StageStartedAt: &completedAt, StageCompletedAt: &completedAt,
		CurrentVersion: fixture.request.CurrentVersion, ExpectedVersion: fixture.request.Request.TargetVersion,
		ServiceState: opsprotocol.ServiceCurrentOnline, UpdatedAt: completedAt,
	}
	if err := fixture.journal.WriteCheckpoint(fixture.input.OperationID, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.AppendEvent(opsprotocol.OperationEvent{
		OperationID: fixture.input.OperationID, Generation: fixture.input.Generation,
		Sequence: 1, Kind: EventKindIntent, Stage: opsprotocol.StageQuiescing,
		Stream: "system", Message: "stage intent persisted", CreatedAt: completedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.Run(context.Background(), fixture.input); err != nil {
		t.Fatal(err)
	}
	if len(fixture.runtime.stages) != 0 {
		t.Fatalf("destructive stage was repeated without a completion fact: %v", fixture.runtime.stages)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != opsprotocol.OperationRecoveryRequired || result.ErrorCode != opsprotocol.ErrorStateConflict {
		t.Fatalf("result=%+v", result)
	}
}

func TestHigherGenerationContinuesOnlyPersistedRecoveryAction(t *testing.T) {
	t.Parallel()

	fixture := newEngineFixture(t, opsprotocol.ActionUpgrade)
	failedAt := time.Date(2026, 8, 25, 1, 45, 0, 0, time.UTC)
	checkpoint := opsprotocol.OperationCheckpoint{
		OperationID: fixture.input.OperationID, Action: opsprotocol.ActionUpgrade,
		TargetVersion: fixture.request.Request.TargetVersion, RunnerDigest: fixture.input.RunnerDigest,
		Generation: 1, FencingTokenHash: hashToken(fixture.input.FencingToken),
		Sequence: 4, Stage: opsprotocol.StageStartingTarget,
		CurrentVersion: fixture.request.CurrentVersion, ExpectedVersion: fixture.request.Request.TargetVersion,
		ServiceState: opsprotocol.ServiceMaintenance, WritesQuiesced: true,
		VerifiedRecoveryPoint: true, BackupPath: "/var/lib/hmaigc-ops/backups/verified",
		BackupChecksumStatus: "verified", DataMigrationStarted: true,
		NextSafeAction: opsprotocol.RecoveryRestoreBackup,
		FailureCode:    opsprotocol.ErrorMigrationFailed, FailureMessage: "migration failed",
		UpdatedAt: failedAt,
	}
	if err := fixture.journal.WriteCheckpoint(fixture.input.OperationID, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := fixture.journal.WriteResult(fixture.input.OperationID, opsprotocol.OperationResult{
		OperationID: fixture.input.OperationID, Generation: 1,
		Status:        opsprotocol.OperationRecoveryRequired,
		ResultVersion: fixture.request.CurrentVersion, ServiceState: opsprotocol.ServiceUnknown,
		ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
		ErrorCode:         opsprotocol.ErrorRestoreFailed, Error: "operator recovery required", CompletedAt: failedAt,
	}); err != nil {
		t.Fatal(err)
	}
	recoveryInput := fixture.input
	recoveryInput.Generation = 2
	recoveryInput.FencingToken = "recovery-generation-token"
	if err := fixture.journal.WriteLease(fixture.input.OperationID, opsprotocol.RunnerLease{
		OperationID: fixture.input.OperationID, Generation: recoveryInput.Generation,
		TokenHash: hashToken(recoveryInput.FencingToken), RunnerDigest: recoveryInput.RunnerDigest,
		AcquiredAt: failedAt, ExpiresAt: failedAt.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.engine.Run(context.Background(), recoveryInput); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fixture.runtime.stages, []opsprotocol.OperationStage{opsprotocol.StageRestoringBackup}) {
		t.Fatalf("recovery stages=%v", fixture.runtime.stages)
	}
	result, err := fixture.journal.ReadResult(fixture.input.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Generation != 2 || result.Status != opsprotocol.OperationFailed || result.ServiceState != opsprotocol.ServiceCurrentRestored {
		t.Fatalf("result=%+v", result)
	}
}

type engineFixture struct {
	journal *opsstate.Journal
	runtime *recordingRuntime
	engine  *Engine
	request opsprotocol.OperationRequestFile
	input   RunInput
}

func newEngineFixture(t *testing.T, action opsprotocol.Action) engineFixture {
	t.Helper()
	journal, err := opsstate.NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	targetVersion := "v1.0.58"
	expectedVersion := targetVersion
	confirmation := "UPGRADE v1.0.58"
	runnerSource := opsprotocol.RunnerSourceTarget
	rollbackBackup := ""
	if action == opsprotocol.ActionRollback {
		targetVersion = ""
		expectedVersion = "v1.0.56"
		confirmation = "ROLLBACK"
		runnerSource = opsprotocol.RunnerSourceCurrent
		rollbackBackup = "/var/lib/hmaigc-ops/backups/rollback-v1.0.56"
	}
	request := opsprotocol.OperationRequestFile{
		OperationID: "op-runner", RequestHash: strings.Repeat("a", 64),
		Request: opsprotocol.StartOperationRequest{
			Action: action, TargetVersion: targetVersion, ActorUserID: "admin",
			ActorDisplayName: "管理员", IdempotencyKey: "idem-runner", Confirmation: confirmation,
		},
		CurrentVersion: "v1.0.57", PreviousVersion: "v1.0.56",
		RollbackBackup:  rollbackBackup,
		ExpectedVersion: expectedVersion, RunnerSource: runnerSource, ControllerVersionAtStart: "v1.0.57",
		CreatedAt: time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC),
	}
	if err := journal.CreateRequest(request); err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("s", 32))
	input := RunInput{
		OperationID: request.OperationID, Generation: 1, FencingToken: "runner-fencing-token",
		RunnerDigest: "example.invalid/runner@sha256:" + strings.Repeat("b", 64), CommandSecret: secret,
	}
	if err := journal.WriteLease(request.OperationID, opsprotocol.RunnerLease{
		OperationID: request.OperationID, Generation: input.Generation,
		TokenHash: hashToken(input.FencingToken), RunnerDigest: input.RunnerDigest,
		AcquiredAt: time.Date(2026, 8, 25, 1, 55, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, 8, 25, 3, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	runtime := &recordingRuntime{journal: journal, secret: secret}
	engine := NewEngine(journal, runtime, func() time.Time {
		return time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)
	})
	return engineFixture{
		journal: journal,
		runtime: runtime,
		engine:  engine,
		request: request,
		input:   input,
	}
}

type recordingRuntime struct {
	journal               *opsstate.Journal
	secret                []byte
	stages                []opsprotocol.OperationStage
	inputs                []StageInput
	cancelAt              opsprotocol.OperationStage
	failAt                opsprotocol.OperationStage
	failure               error
	failures              map[opsprotocol.OperationStage]error
	failuresWithoutOutput map[opsprotocol.OperationStage]error
	controllerHandoff     opsprotocol.ControllerHandoff
	warnings              []opsprotocol.OperationWarning
}

func (r *recordingRuntime) Execute(_ context.Context, input StageInput) (StageOutput, error) {
	r.stages = append(r.stages, input.Stage)
	r.inputs = append(r.inputs, input)
	output := outputForStage(input)
	if input.Stage == opsprotocol.StageControllerHandoff && r.controllerHandoff != "" {
		output.ControllerHandoff = r.controllerHandoff
		output.Warnings = append([]opsprotocol.OperationWarning(nil), r.warnings...)
	}
	if input.Stage == r.cancelAt {
		command := opsprotocol.CancelOperationCommand{
			OperationID: input.Request.OperationID, RequestHash: strings.Repeat("c", 64),
			ActorUserID: "admin", ActorDisplayName: "管理员",
			RequestedAt: time.Now().UTC(), Nonce: "cancel-after-stage",
		}
		if err := r.journal.WriteCancelCommand(r.secret, command); err != nil {
			return StageOutput{}, err
		}
	}
	if input.Stage == r.failAt {
		return output, r.failure
	}
	if failure, exists := r.failures[input.Stage]; exists {
		return output, failure
	}
	if failure, exists := r.failuresWithoutOutput[input.Stage]; exists {
		return StageOutput{}, failure
	}
	return output, nil
}

func outputForStage(input StageInput) StageOutput {
	output := StageOutput{
		ServiceState:   input.Checkpoint.ServiceState,
		CurrentVersion: input.Checkpoint.CurrentVersion,
		ResultVersion:  input.Checkpoint.ExpectedVersion,
	}
	switch input.Stage {
	case opsprotocol.StageOnlinePreflight, opsprotocol.StagePublicVerifying:
		output.ServiceState = opsprotocol.ServiceCurrentOnline
	case opsprotocol.StageQuiescing, opsprotocol.StageQuiescedAudit:
		output.ServiceState = opsprotocol.ServiceMaintenance
		output.WritesQuiesced = true
	case opsprotocol.StageBackingUp:
		output.ServiceState = opsprotocol.ServiceMaintenance
		output.WritesQuiesced = true
		output.VerifiedRecoveryPoint = true
		output.BackupPath = "/var/lib/hmaigc-ops/backups/verified"
		output.BackupChecksumStatus = "verified"
	case opsprotocol.StageRestoringRollbackBackup:
		output.ServiceState = opsprotocol.ServiceMaintenance
		output.WritesQuiesced = true
		output.DataMigrationStarted = true
	case opsprotocol.StageStartingTarget:
		output.ServiceState = opsprotocol.ServiceMaintenance
		output.WritesQuiesced = true
		output.VerifiedRecoveryPoint = true
		output.BackupPath = input.Checkpoint.BackupPath
		output.BackupChecksumStatus = input.Checkpoint.BackupChecksumStatus
		output.DataMigrationStarted = true
		output.BackendImage = "example.invalid/backend@sha256:" + strings.Repeat("d", 64)
	case opsprotocol.StageVerifyingTarget:
		output.ServiceState = opsprotocol.ServiceTargetOnline
		output.TargetBackendHealthy = true
		output.TargetWebHealthy = true
		output.WebImage = "example.invalid/web@sha256:" + strings.Repeat("e", 64)
	case opsprotocol.StageCommittingRelease:
		output.ServiceState = opsprotocol.ServiceTargetOnline
		output.ReleaseCommitted = true
	case opsprotocol.StageControllerHandoff:
		output.ServiceState = opsprotocol.ServiceTargetOnline
		output.ControllerHandoff = opsprotocol.ControllerHandoffUpdated
		output.ControllerVersion = input.Request.ExpectedVersion
	case opsprotocol.StageRestoringCurrent, opsprotocol.StageRestoringBackup:
		output.ServiceState = opsprotocol.ServiceCurrentRestored
		output.CurrentVersion = input.Request.CurrentVersion
	}
	return output
}
