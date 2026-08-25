# HMaigc Durable One-Click Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Repository rules prohibit subagent-driven implementation for this shared deployment state machine.

**Goal:** Replace the controller-owned long deployment script with a target-version, detached, checkpointed Runner so that one-time bootstrap enables reliable admin-console upgrades, safe cancellation, deterministic recovery, and controller handoff.

**Architecture:** The stable controller authenticates and schedules operations, writes immutable requests/commands, launches a target image by digest, and projects Runner facts into SQLite. The detached Runner owns the stage machine and durable execution files in `ops-state`, invokes narrow production stage commands, and recovers from checkpoints without depending on browser, backend, or controller lifetime. Business activation uses only local digest/version/health evidence; public CDN verification is a separate non-transactional action.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, Docker CLI + Docker Compose, Bash, PostgreSQL backup/restore tools, React 19, TypeScript 7, Ant Design 6, Bun tests.

**Spec:** `docs/superpowers/specs/2026-08-25-durable-one-click-upgrade-design.md`

## Global Constraints

- Target platform is one Linux host running Docker Compose; do not add Kubernetes, multi-host coordination, Raft, or cross-region rollout.
- A short controlled maintenance window is allowed; zero downtime is not a requirement.
- Hard cut over to the detached Runner. Do not retain `ScriptExecutor` as a second production path.
- `ops-state` is the only operational fact root. The business database and host version directories are not operational truth sources.
- The controller writes immutable `request.json`, signed `commands/*.json`, and SQLite projections. The Runner writes lease, heartbeat, checkpoint, result, and execution events. Neither may overwrite the other owner's facts.
- Persist every fact before publishing or advancing. Unknown evidence must remain `unknown` or `recovery_required`; never infer success.
- Backend, Web, Runner, backup helper, and controller executions use locally resolved immutable digests after preflight.
- Public/CDN verification is strict but non-transactional and reports `not_run`, `succeeded`, or `failed` independently.
- No production secret may enter operation JSON, events, logs, tests, screenshots, or Git.
- Keep the existing admin operations visual language; add only the status, stop, and recovery interactions required by the contract.
- Preserve unrelated work, especially untracked `web/src/pages/prototype/`; stage only files named in the active task.
- Do not execute the production bootstrap, publish images, create a tag, push, or deploy during implementation.

## Change Budget

- Production responsibilities: protocol/state machine, durable journal, Runner engine, Docker/stage runtime, controller projection/reconciliation, admin API/UI/bootstrap/docs.
- Expected production files: 22–26 touched or created; expected net production addition: 2,200–3,000 lines.
- Expected test/harness files: 10–14; test code is not compressed into production modules.
- Expensive gates: Docker fault injection after Tasks 4, 5, and 7; full Go/race/Web/Compose gates only after Task 9.
- Deferred: true zero-downtime switching, distributed locks, multi-node controllers, admin UI redesign, production bootstrap execution, release/tag/push.
- If scope grows beyond this budget, stop only for a real contract or responsibility expansion, document the reason, and re-check module ownership before continuing.

## File Responsibility Map

### Durable contracts

- Modify `backend/internal/opsprotocol/types.go`: public operation, stage, service-state, warning, handoff, cancel, and recovery DTOs.
- Create `backend/internal/opsprotocol/transitions.go`: pure status/stage transition rules and terminal-state helpers.
- Create `backend/internal/opsstate/layout.go`: validated paths below the configured state root.
- Create `backend/internal/opsstate/atomic.go`: `0600` atomic JSON write and immutable create primitives.
- Create `backend/internal/opsstate/journal.go`: request/command/event/lease/heartbeat/checkpoint/result persistence and reads.

### Runner

- Create `backend/internal/opsrunner/engine.go`: stage execution loop, checkpoint advancement, cancellation and final result.
- Create `backend/internal/opsrunner/recovery.go`: pure recovery decision from checkpoint plus observed service facts.
- Create `backend/internal/opsrunner/runtime.go`: narrow `Runtime` interface and stage input/output types.
- Create `backend/internal/opsrunner/shell_runtime.go`: production adapter for `hmaigc-stage.sh` and controller handoff.
- Create `backend/cmd/ops-runner/main.go`: detached Runner entry point.
- Create `deploy/hmaigc-stage.sh`: one deterministic subcommand per stage; no operation-state ownership.
- Create `deploy/lib/images.sh`: pull and resolve immutable local image references.

### Controller and migration

- Create `backend/internal/opscontroller/runner_manager.go`: target Runner image resolution, detached container launch, inspect, and labels.
- Create `backend/internal/opscontroller/projector.go`: idempotent journal-to-SQLite projection.
- Create `backend/internal/opscontroller/reconcile.go`: attach, import terminal result, or launch recovery Runner.
- Modify `backend/internal/opscontroller/controller.go`: request-first scheduling and reconciliation loop.
- Modify `backend/internal/opscontroller/store.go`: expanded projection schema and conditional transitions.
- Delete `backend/internal/opscontroller/executor.go`: remove the controller-owned long process path.
- Create `backend/internal/opsbootstrap/bootstrap.go` and `backend/cmd/ops-bootstrap/main.go`: canonical configuration and historical terminal-operation import.

### Deployment and product surfaces

- Modify `backend/Dockerfile.ops`, `deploy/docker-compose.ops.yml`, `deploy/ops-entrypoint.sh`, `deploy/hmaigc-ops.sh`, `docker-compose.production.yml`, and `.env.production.example`: ship Runner/bootstrap and use canonical digest-pinned config.
- Modify `backend/internal/opsprotocol/client.go`, `backend/internal/opscontroller/http.go`, `backend/internal/service/admin_operations.go`, and `backend/internal/handler/admin_operations.go`: cancel/recover APIs.
- Modify `web/src/services/api/operations.ts`: exact frontend contract.
- Create `web/src/pages/admin/operations/operation-active-panel.tsx`: active stage, heartbeat, service status, cancel/recovery actions.
- Modify `web/src/pages/admin/operations/operations-page.tsx` and `operations-presenters.tsx`: integrate the new component without visual redesign.
- Modify `.github/workflows/publish-images.yml`, `deploy/README.md`, `PRODUCTION.md`, and `CHANGELOG.md`: release gates and operator guidance.
- Create `deploy/tests/fixtures/production.env` and `deploy/tests/fixtures/ops.env`: non-secret deterministic Compose validation inputs.

---

### Task 1: Freeze the operation and transition contract

**Files:**
- Modify: `backend/internal/opsprotocol/types.go:1-107`
- Create: `backend/internal/opsprotocol/transitions.go`
- Create: `backend/internal/opsprotocol/transitions_test.go`
- Modify: `backend/internal/opscontroller/controller.go:117-143`
- Modify: `backend/internal/opscontroller/controller_test.go:1-166`

**Interfaces:**
- Produces: `OperationStage`, `OperationErrorCode`, `ServiceState`, `ControllerHandoff`, `RunnerSource`, `OperationWarning`, `OperationRequestFile`, `OperationCheckpoint`, `OperationResult`, `OperationEvent`, `RunnerLease`, `RunnerHeartbeat`, `SignedCommandFile`, `RunnerLaunchCommand`, `CancelOperationRequest`, `CancelOperationCommand`, `RecoverOperationRequest`, `PublicVerification`.
- Produces: `ValidateStatusTransition(from, to OperationStatus) error` and `IsTerminalStatus(status OperationStatus) bool`.
- Consumed by: all later tasks; field names in this task are the sole DTO vocabulary.

- [ ] **Step 1: Write failing transition and JSON-contract tests**

```go
func TestValidateStatusTransition(t *testing.T) {
	t.Parallel()
	allowed := [][2]OperationStatus{
		{OperationQueued, OperationRunning},
		{OperationRunning, OperationCancelling},
		{OperationRunning, OperationRecovering},
		{OperationRecovering, OperationSucceeded},
		{OperationRecoveryRequired, OperationRecovering},
	}
	for _, pair := range allowed {
		if err := ValidateStatusTransition(pair[0], pair[1]); err != nil {
			t.Fatalf("expected %s -> %s: %v", pair[0], pair[1], err)
		}
	}
	if err := ValidateStatusTransition(OperationSucceeded, OperationRunning); err == nil {
		t.Fatal("terminal operation must not restart")
	}
}

func TestOperationResultRoundTripPreservesRecoveryFacts(t *testing.T) {
	input := OperationResult{
		Status: OperationSucceeded, ResultVersion: "v1.0.58",
		ServiceState: ServiceTargetOnline,
		ControllerHandoff: ControllerHandoffRestoredPrevious,
		Warnings: []OperationWarning{{Code: "controller_handoff_failed", Message: "旧控制器已恢复"}},
	}
	encoded, err := json.Marshal(input)
	if err != nil { t.Fatal(err) }
	var output OperationResult
	if err := json.Unmarshal(encoded, &output); err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(input, output) { t.Fatalf("input=%+v output=%+v", input, output) }
}
```

- [ ] **Step 2: Run the focused RED test**

Run: `cd backend && go test ./internal/opsprotocol -run 'TestValidateStatusTransition|TestOperationResultRoundTrip' -count=1`

Expected: FAIL because the expanded statuses, DTOs, and transition functions do not exist.

- [ ] **Step 3: Add the exact typed contract and pure transition map**

```go
type OperationStatus string
const (
	OperationQueued OperationStatus = "queued"
	OperationRunning OperationStatus = "running"
	OperationCancelling OperationStatus = "cancelling"
	OperationRecovering OperationStatus = "recovering"
	OperationSucceeded OperationStatus = "succeeded"
	OperationFailed OperationStatus = "failed"
	OperationCancelled OperationStatus = "cancelled"
	OperationRecoveryRequired OperationStatus = "recovery_required"
)

type OperationStage string
const (
	StageAccepted OperationStage = "accepted"
	StageRunnerPreparing OperationStage = "runner_preparing"
	StageOnlinePreflight OperationStage = "online_preflight"
	StagePublicVerifying OperationStage = "public_verifying"
	StageQuiescing OperationStage = "quiescing"
	StageQuiescedAudit OperationStage = "quiesced_audit"
	StageBackingUp OperationStage = "backing_up"
	StageStartingTarget OperationStage = "starting_target"
	StageVerifyingTarget OperationStage = "verifying_target"
	StageRestoringCurrent OperationStage = "restoring_current"
	StageRestoringBackup OperationStage = "restoring_backup"
	StageCommittingRelease OperationStage = "committing_release"
	StageControllerHandoff OperationStage = "controller_handoff"
	StageCompleted OperationStage = "completed"
)

type OperationErrorCode string
const (
	ErrorPreflightFailed OperationErrorCode = "preflight_failed"
	ErrorPublicVerifyFailed OperationErrorCode = "public_verify_failed"
	ErrorRunnerStartFailed OperationErrorCode = "runner_start_failed"
	ErrorLeaseLost OperationErrorCode = "lease_lost"
	ErrorQuiesceFailed OperationErrorCode = "quiesce_failed"
	ErrorBackupFailed OperationErrorCode = "backup_failed"
	ErrorMigrationFailed OperationErrorCode = "migration_failed"
	ErrorTargetHealthFailed OperationErrorCode = "target_health_failed"
	ErrorRestoreFailed OperationErrorCode = "restore_failed"
	ErrorControllerHandoffFailed OperationErrorCode = "controller_handoff_failed"
	ErrorStateConflict OperationErrorCode = "state_conflict"
	ErrorCancelledAtSafePoint OperationErrorCode = "cancelled_at_safe_point"
)

var allowedStatusTransitions = map[OperationStatus]map[OperationStatus]struct{}{
	OperationQueued: {OperationRunning: {}, OperationCancelling: {}, OperationCancelled: {}, OperationFailed: {}},
	OperationRunning: {OperationCancelling: {}, OperationRecovering: {}, OperationSucceeded: {}, OperationFailed: {}, OperationRecoveryRequired: {}},
	OperationCancelling: {OperationRecovering: {}, OperationCancelled: {}, OperationFailed: {}, OperationRecoveryRequired: {}},
	OperationRecovering: {OperationSucceeded: {}, OperationFailed: {}, OperationCancelled: {}, OperationRecoveryRequired: {}},
	OperationRecoveryRequired: {OperationRecovering: {}},
}
```

Define `Operation` with the existing identity/audit timestamps plus these exact typed additions: `Stage OperationStage`, `RunnerVersion string`, `RunnerDigest string`, `RunnerGeneration uint64`, `HeartbeatAt *time.Time`, `ServiceState ServiceState`, `CheckpointSequence uint64`, `CancelRequestedAt *time.Time`, `RecoveryAction RecoveryAction`, `ControllerVersionAtStart string`, `ResultControllerVersion string`, `ControllerHandoff ControllerHandoff`, `Warnings []OperationWarning`, and `ErrorCode OperationErrorCode`. Define `OperationRequestFile` with operation ID, normalized start request, request hash, current/previous/expected release versions, an immutable verified `RollbackBackup` for rollback, `RunnerSource` (`target` only for upgrade; `current` for rollback/backup/verify), controller version at start, explicit `ImportedTerminal` for non-executable migrated history, and creation time. `install` is rejected by the live controller and CLI and is permitted only as imported terminal history; first deployment uses one-time bootstrap. Define `SignedCommandFile` as `SchemaVersion uint32`, `Payload json.RawMessage`, and `Signature string`; only this envelope may be decoded before signature verification. Define `RunnerLaunchCommand` with operation ID, generation, raw fencing token, Runner digest, and issued time; raw token must never appear in Docker environment/labels or projected facts. Define `RunnerLease` with operation ID, generation, token hash, Runner digest, acquired/expiry times; define `RunnerHeartbeat` with operation ID, generation, sequence, stage, service state, and observed time. Define cancel/recover API requests with actor ID/name, idempotency key, and confirmation; define `CancelOperationCommand` with operation ID, request hash, actor facts, request time, and nonce. Define `OperationCheckpoint` with operation/action/version identity, generation/token hash/sequence, current and previous stage timestamps, image digests, service state, quiesce/backup/migration/health facts, controller digests, cancel request, next safe action, and failure code/message. Define `OperationEvent` with operation ID, generation, sequence, kind, stage, stream, message, `json.RawMessage` facts, and creation time. Define `OperationResult` with terminal status, result version/controller version, service state, controller handoff, warnings, typed error code/message, exit code, and completion time. Define `PublicVerification` with status (`not_run`, `succeeded`, or `failed`), operation ID, checked time, typed error code/message; add it to `Overview`.

Until the durable verification projector exists, `Controller.Overview` must explicitly return `PublicVerificationNotRun`; a zero-value status is not a valid public contract.

- [ ] **Step 4: Run protocol tests and formatting**

Run: `cd backend && gofmt -w internal/opsprotocol/types.go internal/opsprotocol/transitions.go internal/opsprotocol/transitions_test.go && go test ./internal/opsprotocol -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the contract checkpoint**

```bash
git add backend/internal/opsprotocol/types.go backend/internal/opsprotocol/transitions.go backend/internal/opsprotocol/transitions_test.go backend/internal/opscontroller/controller.go backend/internal/opscontroller/controller_test.go
git commit -m "feat(ops): define durable upgrade protocol"
```

### Task 2: Build the atomic operation journal

**Files:**
- Create: `backend/internal/opsstate/layout.go`
- Create: `backend/internal/opsstate/atomic.go`
- Create: `backend/internal/opsstate/journal.go`
- Create: `backend/internal/opsstate/commands.go`
- Create: `backend/internal/opsstate/replace_unix.go`
- Create: `backend/internal/opsstate/replace_windows.go`
- Create: `backend/internal/opsstate/journal_test.go`

**Interfaces:**
- Consumes: Task 1 DTOs.
- Produces: `NewJournal(root string) (*Journal, error)`.
- Produces these exact methods:

```go
func (j *Journal) CreateRequest(opsprotocol.OperationRequestFile) error
func (j *Journal) ReadRequest(operationID string) (opsprotocol.OperationRequestFile, error)
func (j *Journal) WriteLaunchCommand(secret []byte, command opsprotocol.RunnerLaunchCommand) error
func (j *Journal) ReadLaunchCommand(secret []byte, operationID string) (opsprotocol.RunnerLaunchCommand, error)
func (j *Journal) WriteCancelCommand(secret []byte, command opsprotocol.CancelOperationCommand) error
func (j *Journal) ReadCancelCommand(secret []byte, operationID string) (*opsprotocol.CancelOperationCommand, error)
func (j *Journal) WriteLease(operationID string, lease opsprotocol.RunnerLease) error
func (j *Journal) WriteHeartbeat(operationID string, heartbeat opsprotocol.RunnerHeartbeat) error
func (j *Journal) WriteCheckpoint(operationID string, checkpoint opsprotocol.OperationCheckpoint) error
func (j *Journal) WriteResult(operationID string, result opsprotocol.OperationResult) error
func (j *Journal) AppendEvent(event opsprotocol.OperationEvent) error
func (j *Journal) ReadEvents(operationID string, generation uint64, after uint64) ([]opsprotocol.OperationEvent, error)
```

- Produces both launch and cancel commands as a `SignedCommandFile`: marshal the concrete typed command into canonical JSON payload bytes, sign those exact bytes with HMAC-SHA256, then marshal the envelope. Reads may unmarshal only the envelope first; they must validate schema version, compare the HMAC with `hmac.Equal`, and only then unmarshal `Payload` into the trusted typed command. Reject unknown fields, operation-ID mismatch, expired commands, malformed signatures, and replayed nonce/generation.
- Ownership: controller calls request/command methods; Runner calls lease/heartbeat/checkpoint/result/event methods.

- [x] **Step 1: Write failing filesystem safety and replay tests**

```go
func TestJournalPersistsBeforeReplayAndRejectsDuplicateSequence(t *testing.T) {
	j := newTestJournal(t)
	req := opsprotocol.OperationRequestFile{OperationID: "op-001", RequestHash: strings.Repeat("a", 64)}
	if err := j.CreateRequest(req); err != nil { t.Fatal(err) }
	event := opsprotocol.OperationEvent{OperationID: "op-001", Generation: 1, Sequence: 1, Kind: "fact"}
	if err := j.AppendEvent(event); err != nil { t.Fatal(err) }
	if err := j.AppendEvent(event); !errors.Is(err, ErrImmutableFactExists) { t.Fatalf("got %v", err) }
	events, err := j.ReadEvents("op-001", 1, 0)
	if err != nil { t.Fatal(err) }
	if len(events) != 1 || events[0].Sequence != 1 { t.Fatalf("events=%+v", events) }
}

func TestJournalRejectsTraversalAndTruncatedCheckpoint(t *testing.T) {
	j := newTestJournal(t)
	if _, err := j.Layout().OperationDir("../escape"); err == nil { t.Fatal("expected path rejection") }
	writeRawCheckpoint(t, j, "op-002", []byte(`{"stage":`))
	if _, err := j.ReadCheckpoint("op-002"); !errors.Is(err, ErrCorruptFact) { t.Fatalf("got %v", err) }
}
```

- [x] **Step 2: Run the focused RED test**

Run: `cd backend && go test ./internal/opsstate -count=1`

Expected: FAIL because package `opsstate` does not exist.

- [x] **Step 3: Implement validated layout and durable writes**

```go
func writeBytesAtomic(path string, mode fs.FileMode, encoded []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil { return err }
	temporary, err := os.CreateTemp(directory, ".hmaigc-*.tmp")
	if err != nil { return err }
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil { temporary.Close(); return err }
	if _, err := temporary.Write(encoded); err != nil { temporary.Close(); return err }
	if err := temporary.Sync(); err != nil { temporary.Close(); return err }
	if err := temporary.Close(); err != nil { return err }
	if err := os.Rename(temporaryPath, path); err != nil { return err }
	dir, err := os.Open(directory)
	if err != nil { return err }
	defer dir.Close()
	return dir.Sync()
}
```

Each typed journal method first calls `json.Marshal` on its concrete DTO, then passes the bytes to `writeBytesAtomic`. Use `Lstat`, `filepath.Rel`, regular-file checks, 4 MiB read limits, `0600` files, `0700` directories, O_EXCL immutable request/event creation, and canonical event filenames `%020d-%020d.json`.

- [x] **Step 4: Run journal tests and race**

Run: `cd backend && gofmt -w internal/opsstate && go test ./internal/opsstate -count=1 && go test -race ./internal/opsstate -count=1`

Expected: PASS with duplicate, traversal, truncation, and concurrent append cases covered.

- [ ] **Step 5: Commit the journal checkpoint**

```bash
git add backend/internal/opsstate backend/internal/opsprotocol/types.go
git commit -m "feat(ops): persist immutable operation facts"
```

### Task 3: Implement the Runner stage and recovery engine

**Files:**
- Create: `backend/internal/opsrunner/runtime.go`
- Create: `backend/internal/opsrunner/engine.go`
- Create: `backend/internal/opsrunner/recovery.go`
- Create: `backend/internal/opsrunner/engine_test.go`
- Create: `backend/internal/opsrunner/recovery_test.go`

**Interfaces:**
- Consumes: `opsstate.Journal`, Task 1 stages/statuses.
- Produces: `Runtime.Execute(context.Context, StageInput) (StageOutput, error)`.
- Produces: `Engine.Run(context.Context, RunInput) error`.
- Produces: `DecideRecovery(OperationCheckpoint, ObservedState) (RecoveryDecision, error)`.

- [x] **Step 1: Write failing stage-order, safe-cancel, and recovery-decision tests**

```go
func TestUpgradePersistsCheckpointBeforeNextStage(t *testing.T) {
	runtime := &recordingRuntime{}
	journal := newRunnerJournal(t, opsprotocol.ActionUpgrade)
	engine := NewEngine(journal, runtime, fixedClock())
	if err := engine.Run(context.Background(), RunInput{OperationID: "op-upgrade", Generation: 1, FencingToken: "token"}); err != nil { t.Fatal(err) }
	want := []opsprotocol.OperationStage{
		opsprotocol.StageOnlinePreflight, opsprotocol.StageQuiescing, opsprotocol.StageQuiescedAudit,
		opsprotocol.StageBackingUp, opsprotocol.StageStartingTarget, opsprotocol.StageVerifyingTarget,
		opsprotocol.StageCommittingRelease, opsprotocol.StageControllerHandoff,
	}
	if !reflect.DeepEqual(want, runtime.stages) { t.Fatalf("want=%v got=%v", want, runtime.stages) }
	assertEveryStageHasCompletedCheckpoint(t, journal, want)
}

func TestCancellationAfterQuiesceRestoresCurrentBeforeCancelled(t *testing.T) {
	runtime := &recordingRuntime{cancelAt: opsprotocol.StageQuiescing}
	result := runEngine(t, runtime, opsprotocol.ActionUpgrade)
	if result.Status != opsprotocol.OperationCancelled || result.ServiceState != opsprotocol.ServiceCurrentRestored { t.Fatalf("%+v", result) }
	if !slices.Contains(runtime.stages, opsprotocol.StageRestoringCurrent) { t.Fatal("current release was not restored") }
}

func TestRecoveryAfterCommittedReleaseKeepsTarget(t *testing.T) {
	decision, err := DecideRecovery(committedCheckpoint(), ObservedState{TargetHealthy: true, ActualVersion: "v1.0.58"})
	if err != nil { t.Fatal(err) }
	if decision.Action != opsprotocol.RecoveryContinueControllerHandoff { t.Fatalf("%+v", decision) }
}
```

- [x] **Step 2: Run the focused RED tests**

Run: `cd backend && go test ./internal/opsrunner -run 'TestUpgrade|TestCancellation|TestRecovery' -count=1`

Expected: FAIL because the Runner engine does not exist.

- [x] **Step 3: Implement pure action plans and fact-first execution**

```go
type Runtime interface {
	Execute(context.Context, StageInput) (StageOutput, error)
}

type StageInput struct {
	Request opsprotocol.OperationRequestFile
	Checkpoint opsprotocol.OperationCheckpoint
	Stage opsprotocol.OperationStage
}

type StageOutput struct {
	ServiceState opsprotocol.ServiceState
	CurrentVersion string
	ResultVersion string
	BackupPath string
	BackendImage string
	WebImage string
	BackupHelperImage string
	ControllerImage string
	ControllerHandoff opsprotocol.ControllerHandoff
	Warnings []opsprotocol.OperationWarning
}

type ObservedState struct {
	ActualVersion string
	CurrentHealthy bool
	TargetHealthy bool
	DataRewriteStarted bool
	VerifiedBackupPath string
	ReleaseCommitted bool
	RunnerStillOwnsLock bool
}

type RecoveryDecision struct {
	Action opsprotocol.RecoveryAction
	Stage opsprotocol.OperationStage
	ExpectedServiceState opsprotocol.ServiceState
}

func stagesFor(action opsprotocol.Action) ([]opsprotocol.OperationStage, error) {
	switch action {
	case opsprotocol.ActionUpgrade:
		return []opsprotocol.OperationStage{opsprotocol.StageOnlinePreflight, opsprotocol.StageQuiescing, opsprotocol.StageQuiescedAudit, opsprotocol.StageBackingUp, opsprotocol.StageStartingTarget, opsprotocol.StageVerifyingTarget, opsprotocol.StageCommittingRelease, opsprotocol.StageControllerHandoff}, nil
	case opsprotocol.ActionVerify:
		return []opsprotocol.OperationStage{opsprotocol.StagePublicVerifying}, nil
	case opsprotocol.ActionBackup:
		return []opsprotocol.OperationStage{opsprotocol.StageOnlinePreflight, opsprotocol.StageQuiescing, opsprotocol.StageBackingUp, opsprotocol.StageStartingTarget, opsprotocol.StageVerifyingTarget}, nil
	case opsprotocol.ActionRollback:
		return []opsprotocol.OperationStage{opsprotocol.StageOnlinePreflight, opsprotocol.StageQuiescing, opsprotocol.StageBackingUp, opsprotocol.StageStartingTarget, opsprotocol.StageVerifyingTarget, opsprotocol.StageCommittingRelease, opsprotocol.StageControllerHandoff}, nil
	case opsprotocol.ActionInstall:
		return []opsprotocol.OperationStage{opsprotocol.StageOnlinePreflight, opsprotocol.StageStartingTarget, opsprotocol.StageVerifyingTarget, opsprotocol.StageCommittingRelease, opsprotocol.StageControllerHandoff}, nil
	default:
		return nil, fmt.Errorf("unsupported action %q", action)
	}
}
```

For each stage: append `intent`, execute one runtime call, append `fact`, atomically advance checkpoint, then inspect the signed cancel command. On failure, call `DecideRecovery`; execute `restoring_current` when no data was rewritten and `restoring_backup` after a verified recovery point exists. Write `result.json` only after service state is locally observed.

- [x] **Step 4: Run Runner tests and race**

Run: `cd backend && gofmt -w internal/opsrunner && go test ./internal/opsrunner -count=1 && go test -race ./internal/opsrunner -count=1`

Expected: PASS across every checkpoint boundary, safe cancellation, target commit, restore failure, and repeated recovery.

- [ ] **Step 5: Commit the Runner engine checkpoint**

```bash
git add backend/internal/opsrunner
git commit -m "feat(ops): add checkpointed runner engine"
```

### Task 4: Add digest-pinned production stage execution

**Files:**
- Create: `deploy/lib/images.sh`
- Create: `deploy/hmaigc-stage.sh`
- Create: `deploy/tests/hmaigc-stage-smoke.sh`
- Create: `backend/internal/opsrunner/shell_runtime.go`
- Create: `backend/internal/opsrunner/shell_runtime_test.go`
- Create: `backend/cmd/ops-runner/main.go`
- Modify: `deploy/lib/common.sh:81-345`
- Modify: `deploy/lib/backup.sh:40-119`
- Modify: `docker-compose.production.yml:31-84`
- Modify: `backend/Dockerfile.ops:1-56`

**Interfaces:**
- Consumes: Task 3 `Runtime`.
- Produces: `ShellRuntime.Execute` and `/usr/local/bin/hmaigc-ops-runner`.
- Produces stage commands: `prepare`, `online-preflight`, `public-verify`, `quiesce`, `quiesced-audit`, `backup`, `start-target`, `verify-target`, `commit-release`, `restore-current`, `handoff-controller`.

- [ ] **Step 1: Write failing digest and ordering smoke cases**

```bash
run_stage prepare v1.0.58
assert_file_contains "$FACTS_FILE" '"backendImage":"ghcr.io/example/hmaigc-backend@sha256:'
assert_file_contains "$FACTS_FILE" '"webImage":"ghcr.io/example/hmaigc-web@sha256:'
assert_not_contains "$FACTS_FILE" 'hmaigc-backend:v1.0.58"'

run_stage quiesce v1.0.58
assert_log_before 'compose stop web backend' 'stage completed: quiesce'

FAKE_PUBLIC_CDN_FAILURE=1 run_stage public-verify v1.0.58 && fail "public verify unexpectedly passed"
assert_release_state CURRENT_VERSION v1.0.57

FAKE_PUBLIC_CDN_HANG=1 PUBLIC_VERIFY_TOTAL_TIMEOUT_SECONDS=2 run_stage public-verify v1.0.58 && fail "hung public verify unexpectedly passed"
assert_elapsed_less_than 4

run_runner_in_background op-lock-1
run_second_runner_expect_conflict op-lock-2
assert_no_production_mutation op-lock-2
```

- [ ] **Step 2: Run focused RED tests**

Run: `bash deploy/tests/hmaigc-stage-smoke.sh && cd backend && go test ./internal/opsrunner -run TestShellRuntime -count=1`

Expected: FAIL because stage entry points and digest facts do not exist.

- [ ] **Step 3: Implement exact image resolution and narrow stage commands**

```bash
resolve_image_digest() {
    local tagged="$1" repo_digest
    docker pull "$tagged" >/dev/null
    repo_digest="$(docker image inspect "$tagged" --format '{{join .RepoDigests "\n"}}' | head -n 1)"
    [[ "$repo_digest" == *@sha256:* ]] || fail "镜像未返回不可变摘要：$tagged"
    printf '%s\n' "$repo_digest"
}
```

Change business Compose image declarations to required `HMAIGC_BACKEND_IMAGE` and `HMAIGC_WEB_IMAGE`. The Runner saves the previous canonical config before writing target digest references; `restoring_current` or `restoring_backup` restores the previous digest references before starting the current version. `start-target` refuses missing digest variables. Keep `BACKUP_HELPER_IMAGE` pinned by digest. Stage commands may change services/backups/release state but must never write operation request, event, checkpoint, heartbeat, or result files.

The Runner acquires `/var/lib/hmaigc-ops/deploy.lock` with a non-blocking exclusive OS lock immediately after validating the signed launch command and retains the same open lock handle until the terminal result is durably written. Lock contention fails before any production mutation with `state_conflict`; no second Runner waits invisibly or steals the lock.

`public-verify` enumerates every manifest asset but runs at most eight requests concurrently. Each request uses a 5-second connect timeout, a 15-second total timeout, and at most two attempts; the entire verification has a 120-second deadline. Any timeout or missing asset is an explicit failed `PublicVerification`; it never changes business release state. Tests override the total deadline to prove a hanging endpoint cannot hold the operation indefinitely.

- [ ] **Step 4: Build and exercise the Runner image locally**

Run:

```bash
bash -n deploy/hmaigc-stage.sh deploy/lib/images.sh deploy/lib/common.sh deploy/lib/backup.sh
bash deploy/tests/hmaigc-stage-smoke.sh
cd backend && go test ./internal/opsrunner -run TestShellRuntime -count=1
cd .. && docker build -f backend/Dockerfile.ops -t hmaigc-ops-controller:runner-test .
docker run --rm --entrypoint hmaigc-ops-runner hmaigc-ops-controller:runner-test --help
```

Expected: all checks PASS; `--help` exits 0 without state mutation.

- [ ] **Step 5: Commit the production stage checkpoint**

```bash
git add backend/Dockerfile.ops backend/cmd/ops-runner backend/internal/opsrunner deploy/hmaigc-stage.sh deploy/lib/images.sh deploy/lib/common.sh deploy/lib/backup.sh deploy/tests/hmaigc-stage-smoke.sh docker-compose.production.yml
git commit -m "feat(deploy): execute upgrades in digest-pinned stages"
```

### Task 5: Replace controller execution with dispatch, projection, and reconciliation

**Files:**
- Create: `backend/internal/opscontroller/runner_manager.go`
- Create: `backend/internal/opscontroller/projector.go`
- Create: `backend/internal/opscontroller/reconcile.go`
- Create: `backend/internal/opscontroller/reconcile_test.go`
- Modify: `backend/internal/opscontroller/controller.go:19-315`
- Modify: `backend/internal/opscontroller/store.go:14-292`
- Modify: `backend/internal/opscontroller/controller_test.go:1-166`
- Modify: `backend/cmd/ops-controller/main.go:28-133`
- Delete: `backend/internal/opscontroller/executor.go`

**Interfaces:**
- Consumes: journal and Runner command from Tasks 2–4.
- Produces: `RunnerManager.Resolve`, `Start`, `Inspect`, `ListByOperation`.
- Produces: `Projector.Project(operationID string) error` and `Controller.Reconcile(context.Context) error`.

- [ ] **Step 1: Replace executor tests with failing restart/replay tests**

```go
func TestControllerRestartReattachesMatchingRunner(t *testing.T) {
	fixture := newControllerFixture(t)
	op := fixture.startQueuedUpgrade(t)
	fixture.manager.instances[op.ID] = RunnerInstance{Running: true, Generation: 1, Digest: fixture.targetDigest}
	writeRunningFacts(t, fixture.journal, op.ID, 1)
	restarted := fixture.newController(t)
	if err := restarted.Reconcile(context.Background()); err != nil { t.Fatal(err) }
	if fixture.manager.startCount != 0 { t.Fatal("must not start a second Runner") }
	assertProjectedStatus(t, fixture.store, op.ID, opsprotocol.OperationRunning)
}

func TestControllerProjectsResultExactlyOnce(t *testing.T) {
	fixture := newControllerFixture(t)
	op := fixture.startQueuedUpgrade(t)
	writeTerminalResultAndEvents(t, fixture.journal, op.ID)
	if err := fixture.projector.Project(op.ID); err != nil { t.Fatal(err) }
	if err := fixture.projector.Project(op.ID); err != nil { t.Fatal(err) }
	assertLogCount(t, fixture.store, op.ID, 3)
}

func TestControllerEntersRecoveryRequiredWhenRunnerOwnershipUnknown(t *testing.T) {
	fixture := newControllerFixture(t)
	op := fixture.startQueuedUpgrade(t)
	writeExpiredHeartbeatWithLiveLock(t, fixture, op.ID)
	if err := fixture.controller.Reconcile(context.Background()); err != nil { t.Fatal(err) }
	assertProjectedStatus(t, fixture.store, op.ID, opsprotocol.OperationRecoveryRequired)
}
```

- [ ] **Step 2: Run the controller RED tests**

Run: `cd backend && go test ./internal/opscontroller -run 'TestControllerRestart|TestControllerProjects|TestControllerEnters' -count=1`

Expected: FAIL because startup still marks running operations failed and the controller owns `ScriptExecutor`.

- [ ] **Step 3: Implement request-first dispatch and idempotent projection**

```go
type RunnerManager interface {
	Resolve(context.Context, opsprotocol.OperationRequestFile) (ResolvedRunner, error)
	Start(context.Context, RunnerLaunch) error
	Inspect(context.Context, string) (RunnerInstance, error)
	ListByOperation(context.Context, string) ([]RunnerInstance, error)
}

type ResolvedRunner struct {
	Version string
	Digest string
}

type RunnerLaunch struct {
	OperationID string
	Generation uint64
	ImageDigest string
	StateVolume string
}

type RunnerInstance struct {
	ContainerID string
	OperationID string
	Generation uint64
	Digest string
	Running bool
	ExitCode *int
}

func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = c.Reconcile(ctx)
		_ = c.dispatchQueued(ctx)
		select { case <-ctx.Done(): return; case <-ticker.C: case <-c.wake: }
	}
}
```

Derive operation IDs with this exact helper:

```go
func operationIDForIdempotency(idempotencyKey string) string {
	sum := sha256.Sum256([]byte(idempotencyKey))
	return hex.EncodeToString(sum[:16])
}
```

Under `startMu`, check an existing SQLite projection first, then create or compare the immutable request at that deterministic path, and finally import it into SQLite. Startup reconciliation imports any request that survived before projection. Install/upgrade resolve the requested target Runner; rollback/backup/verify resolve the current `control.env` Runner digest. Delete `FailInterruptedOperations` and `ScriptExecutor`. Expand the SQLite projection with `stage`, generation, digest, heartbeat, service state, checkpoint sequence, warning JSON, handoff, cancel/recovery fields, and unique `(operation_id,generation,event_sequence)`. Every update must include expected prior status/stage in its `WHERE` clause.

Start Runner containers with deterministic name `hmaigc-ops-runner-<operation-id>-g<generation>` and only these non-secret ownership labels: role `runner`, operation ID, generation, and image digest. Use the digest returned by `Resolve`, `--restart=no`, a read-only root filesystem, `--tmpfs /tmp`, `--security-opt no-new-privileges`, the Docker socket, and the existing named state volume. Pass only operation ID and generation as process arguments. The Runner reads and verifies `commands/launch.json` and the shared secret from the state volume; the raw fencing token must never be present in container environment, labels, arguments, inspect output, logs, events, or SQLite.

- [ ] **Step 4: Run focused controller tests and race**

Run: `cd backend && gofmt -w internal/opscontroller cmd/ops-controller && go test ./internal/opscontroller -count=1 && go test -race ./internal/opscontroller -count=1`

Expected: PASS; no test references `ScriptExecutor` or `FailInterruptedOperations`.

- [ ] **Step 5: Commit the controller hard cut**

```bash
git add backend/internal/opscontroller backend/cmd/ops-controller/main.go
git commit -m "refactor(ops): detach upgrade execution from controller"
```

### Task 6: Add authenticated cancel and recovery APIs

**Files:**
- Modify: `backend/internal/opsprotocol/client.go:20-158`
- Modify: `backend/internal/opscontroller/http.go:32-203`
- Modify: `backend/internal/opscontroller/errors.go:1-21`
- Modify: `backend/internal/service/admin_operations.go:12-139`
- Modify: `backend/internal/service/admin_operations_test.go:1-48`
- Modify: `backend/internal/handler/admin_operations.go:12-101`
- Modify: `backend/cmd/opsctl/main.go:20-126`
- Create: `backend/internal/opscontroller/control_test.go`

**Interfaces:**
- Produces client methods `CancelOperation(context.Context, string, CancelOperationRequest)` and `RecoverOperation(context.Context, string, RecoverOperationRequest)`.
- Produces controller routes `POST /v1/operations/{id}/cancel` and `POST /v1/operations/{id}/recover`.
- Produces backend routes `POST /admin/operations/:id/cancel` and `POST /admin/operations/:id/recover`.

- [ ] **Step 1: Write failing authorization, signature, and state tests**

```go
func TestCancelOperationWritesSignedCommandAndMovesToCancelling(t *testing.T) {
	fixture := newControllerFixture(t)
	op := fixture.runningOperation(t)
	result, err := fixture.controller.CancelOperation(op.ID, opsprotocol.CancelOperationRequest{
		ActorUserID: "admin", ActorDisplayName: "管理员",
		IdempotencyKey: "cancel-command-0001", Confirmation: "STOP " + op.ID,
	})
	if err != nil { t.Fatal(err) }
	if result.Status != opsprotocol.OperationCancelling { t.Fatalf("%+v", result) }
	command := readAndVerifyCancelCommand(t, fixture.journal, op.ID, fixture.secret)
	if command.OperationID != op.ID { t.Fatalf("%+v", command) }
}

func TestRecoverRequiresRecoveryRequiredStatus(t *testing.T) {
	fixture := newControllerFixture(t)
	op := fixture.runningOperation(t)
	_, err := fixture.controller.RecoverOperation(op.ID, validRecover(op.ID))
	if !errors.Is(err, ErrRecoveryNotAllowed) { t.Fatalf("got %v", err) }
}
```

- [ ] **Step 2: Run the API RED tests**

Run: `cd backend && go test ./internal/opscontroller ./internal/service -run 'TestCancel|TestRecover|TestMapOperations' -count=1`

Expected: FAIL because cancel/recover methods and routes do not exist.

- [ ] **Step 3: Implement command HMAC and API propagation**

```go
type Client interface {
	Overview(context.Context) (*Overview, error)
	Operations(context.Context, int) (*OperationPage, error)
	Operation(context.Context, string) (*Operation, error)
	OperationLogs(context.Context, string, uint64, int) (*OperationLogPage, error)
	Backups(context.Context, int) ([]Backup, error)
	StartOperation(context.Context, StartOperationRequest) (*Operation, error)
	CancelOperation(context.Context, string, CancelOperationRequest) (*Operation, error)
	RecoverOperation(context.Context, string, RecoverOperationRequest) (*Operation, error)
}
```

Use exact confirmations `STOP <operation-id>` and `RECOVER <operation-id>`. Return 409 for illegal state or idempotency conflict, 404 for unknown operation, 403 for non-admin backend callers, and 503 when the controller is unavailable. Recovery starts a new generation only after the manager proves no prior Runner can still mutate production.

- [ ] **Step 4: Run API and service tests**

Run: `cd backend && gofmt -w internal/opsprotocol internal/opscontroller internal/service internal/handler cmd/opsctl && go test ./internal/opsprotocol ./internal/opscontroller ./internal/service ./internal/handler -count=1`

Expected: PASS, including duplicate cancel, mismatched confirmation, non-admin, transport failure, and illegal recovery cases.

- [ ] **Step 5: Commit the control API checkpoint**

```bash
git add backend/internal/opsprotocol backend/internal/opscontroller backend/internal/service/admin_operations.go backend/internal/service/admin_operations_test.go backend/internal/handler/admin_operations.go backend/cmd/opsctl/main.go
git commit -m "feat(ops): add safe cancellation and recovery APIs"
```

### Task 7: Implement controller handoff and one-time bootstrap

**Files:**
- Create: `backend/internal/opsbootstrap/bootstrap.go`
- Create: `backend/internal/opsbootstrap/bootstrap_test.go`
- Create: `backend/cmd/ops-bootstrap/main.go`
- Create: `deploy/hmaigc-bootstrap.sh`
- Create: `deploy/tests/hmaigc-bootstrap-smoke.sh`
- Create: `deploy/tests/fixtures/production.env`
- Create: `deploy/tests/fixtures/ops.env`
- Modify: `backend/internal/opsrunner/shell_runtime.go`
- Modify: `backend/internal/opsrunner/shell_runtime_test.go`
- Modify: `backend/cmd/ops-controller/main.go`
- Modify: `backend/Dockerfile.ops`
- Modify: `deploy/docker-compose.ops.yml:1-37`
- Modify: `deploy/ops-entrypoint.sh:1-16`
- Modify: `deploy/hmaigc-ops.sh:1-26`
- Modify: `.env.production.example:1-43`

**Interfaces:**
- Produces `hmaigc-ops-controller validate` with no scheduler/store mutation.
- Produces `hmaigc-ops-bootstrap import --source-db ... --state-root ...`.
- Produces canonical `config/production.env` and `config/control.env` with `HMAIGC_OPS_IMAGE=<repo>@sha256:<digest>`.

- [ ] **Step 1: Write failing handoff and migration tests**

```go
func TestHandoffRestoresOldControllerWithoutFailingBusinessUpgrade(t *testing.T) {
	runtime := newHandoffFixture(t)
	runtime.failCandidateHealth = true
	output, err := runtime.Execute(context.Background(), handoffStageInput())
	if err != nil { t.Fatal(err) }
	if output.ControllerHandoff != opsprotocol.ControllerHandoffRestoredPrevious { t.Fatalf("%+v", output) }
	if output.ServiceState != opsprotocol.ServiceTargetOnline { t.Fatalf("%+v", output) }
	assertControlEnvDigest(t, runtime.controlEnv, runtime.oldDigest)
}

func TestBootstrapRefusesActiveHistoricalOperation(t *testing.T) {
	fixture := newBootstrapFixture(t)
	fixture.insertLegacyOperation(t, opsprotocol.OperationRunning)
	err := Bootstrap(fixture.input())
	if !errors.Is(err, ErrActiveLegacyOperation) { t.Fatalf("got %v", err) }
	assertFileAbsent(t, filepath.Join(fixture.stateRoot, "config", "control.env"))
}

func TestBootstrapImportsTerminalHistoryWithoutChangingIDs(t *testing.T) {
	fixture := newBootstrapFixture(t)
	legacy := fixture.insertLegacyOperation(t, opsprotocol.OperationFailed)
	if err := Bootstrap(fixture.input()); err != nil { t.Fatal(err) }
	result := readImportedResult(t, fixture.stateRoot, legacy.ID)
	if result.OperationID != legacy.ID || !result.CompletedAt.Equal(*legacy.CompletedAt) { t.Fatalf("%+v", result) }
}
```

- [ ] **Step 2: Run the bootstrap/handoff RED tests**

Run: `cd backend && go test ./internal/opsbootstrap ./internal/opsrunner -run 'TestBootstrap|TestHandoff' -count=1`

Expected: FAIL because bootstrap import and controller validation/handoff do not exist.

- [ ] **Step 3: Implement side-effect-free validation, atomic control-env swap, and importer**

```go
func runValidate(config Config) error {
	if err := ValidateConfigFiles(config); err != nil { return err }
	if err := ValidateStateLayout(config.StateRoot); err != nil { return err }
	if err := ValidateStoreSchemaReadOnly(config.StorePath); err != nil { return err }
	return ValidateProtocolVersion(config.ProtocolVersion)
}
```

The validate command must not call `OpenStore` with `AutoMigrate`, `Controller.New`, `Run`, reconciliation, or any write method. Bootstrap first proves there are no queued/running/unknown legacy operations, creates a verified state-volume backup, imports terminal operations, writes canonical config atomically, then starts the stable controller. It never deletes legacy host directories.

`config/production.env` is the only business Compose input and must contain the existing production variables plus exact digest-pinned `HMAIGC_BACKEND_IMAGE`, `HMAIGC_WEB_IMAGE`, `HMAIGC_VERSION`, and `BACKUP_HELPER_IMAGE`. `config/control.env` must contain digest-pinned `HMAIGC_OPS_IMAGE`, `HMAIGC_OPS_VERSION`, `HMAIGC_OPS_PROTOCOL_VERSION`, image registry/release API settings, compose project name, state volume, backend GID, and `CANVAS_ENVIRONMENT=production`. `docker-compose.production.yml` consumes the two required business image references directly. `deploy/docker-compose.ops.yml` consumes required `${HMAIGC_OPS_IMAGE:?}` and mounts only the Docker socket plus named `ops-state`; it must not bind any host version directory or `.env.production`. `ops-entrypoint.sh` only validates the canonical config files are readable and then `exec`s the controller; it must not copy host configuration on restart. `hmaigc-ops.sh` becomes a disaster-recovery CLI that reads canonical `control.env`, not a self-updating controller tied to its working directory.

`hmaigc-bootstrap.sh` requires explicit source env, source controller database, state-volume name, target controller digest, and protocol version. It copies and normalizes the source only after read-only validation, rejects a second source with different facts, and is idempotent for the exact same inputs. The Docker fixtures use syntactically valid fake 64-hex digests for all three required application/controller image variables, so Compose validation exercises the production contract instead of tag fallbacks.

- [ ] **Step 4: Run isolated Docker handoff/bootstrap smoke**

Run:

```bash
bash -n deploy/hmaigc-bootstrap.sh deploy/hmaigc-ops.sh deploy/ops-entrypoint.sh
bash deploy/tests/hmaigc-bootstrap-smoke.sh
cd backend && go test ./internal/opsbootstrap ./internal/opsrunner ./internal/opscontroller -count=1
cd .. && docker compose --env-file deploy/tests/fixtures/ops.env -f deploy/docker-compose.ops.yml config -q
```

Expected: PASS for successful bootstrap, active-operation refusal, candidate-controller failure with old-controller restoration, and repeat bootstrap idempotency.

- [ ] **Step 5: Commit the bootstrap checkpoint**

```bash
git add backend/internal/opsbootstrap backend/cmd/ops-bootstrap backend/cmd/ops-controller backend/internal/opsrunner backend/Dockerfile.ops deploy/hmaigc-bootstrap.sh deploy/tests/hmaigc-bootstrap-smoke.sh deploy/tests/fixtures/production.env deploy/tests/fixtures/ops.env deploy/docker-compose.ops.yml deploy/ops-entrypoint.sh deploy/hmaigc-ops.sh .env.production.example
git commit -m "feat(deploy): bootstrap stable upgrade controller"
```

### Task 8: Expose truthful operation control in the admin UI

**Files:**
- Modify: `web/src/services/api/operations.ts:1-119`
- Create: `web/src/pages/admin/operations/operation-active-panel.tsx`
- Modify: `web/src/pages/admin/operations/operations-page.tsx:1-452`
- Modify: `web/src/pages/admin/operations/operations-presenters.tsx:1-157`
- Create: `web/test/admin-operations-recovery.test.ts`

**Interfaces:**
- Consumes: exact backend DTOs and cancel/recover routes from Task 6.
- Produces: `cancelOperation(id, input)` and `recoverOperation(id, input)` API helpers.
- Produces: active operation panel with heartbeat, stage, service state, safe-stop status, warnings, and recovery action.
- Produces: pure `presentPublicVerification(PublicVerification)` returning label/tone/detail without deriving status from a business operation.

- [ ] **Step 1: Re-read `Design.md`, then write failing UI contract tests**

```ts
test("active panel exposes only controls allowed by durable status", async () => {
    const cancelled: string[] = [];
    const recovered: string[] = [];
    const mounted = await mountPanel(runningOperation(), {
        onCancel: async (operation) => { cancelled.push(operation.id); },
        onRecover: async (operation) => { recovered.push(operation.id); },
    });
    await clickButton("停止任务");
    expect(cancelled).toEqual(["op-001"]);
    await mounted.rerender(cancellingOperation());
    expect(document.body.textContent).toContain("已收到停止请求，正在到达安全点");
    expect(findButton("停止任务")).toBeNull();
    await mounted.rerender(recoveryRequiredOperation());
    await clickButton("恢复任务");
    expect(recovered).toEqual(["op-001"]);
    expect(document.body.textContent).not.toContain("执行成功，请耐心等待");
});

test("public verification is presented independently", () => {
    expect(presentPublicVerification({ status: "not_run", operationId: "", checkedAt: null, errorCode: "", error: "" }).label).toBe("未执行");
    expect(presentPublicVerification({ status: "succeeded", operationId: "op-v", checkedAt: "2026-08-25T00:00:00Z", errorCode: "", error: "" }).tone).toBe("success");
    expect(presentPublicVerification({ status: "failed", operationId: "op-v", checkedAt: "2026-08-25T00:00:00Z", errorCode: "public_timeout", error: "timeout" }).tone).toBe("error");
});
```

Use the existing Happy DOM + React `act` mounting pattern from adjacent admin tests. `mountPanel`, `runningOperation`, `cancellingOperation`, `recoveryRequiredOperation`, `findButton`, and `clickButton` are typed test helpers in the same test file; they construct full `OperationsRecord` fixtures without unsafe type assertions.

- [ ] **Step 2: Run the focused UI RED test**

Run: `cd web && bun test test/admin-operations-recovery.test.ts`

Expected: FAIL because the panel and expanded DTO do not exist.

- [ ] **Step 3: Implement the typed panel and page integration**

```ts
export type OperationsStatus = "queued" | "running" | "cancelling" | "recovering" | "succeeded" | "failed" | "cancelled" | "recovery_required";

export function cancelOperation(id: string, input: { confirmation: string; idempotencyKey: string }) {
    return request<OperationsRecord>(api.post(`/admin/operations/${encodeURIComponent(id)}/cancel`, input));
}

export function recoverOperation(id: string, input: { confirmation: string; idempotencyKey: string }) {
    return request<OperationsRecord>(api.post(`/admin/operations/${encodeURIComponent(id)}/recover`, input));
}

export type OperationActivePanelProps = {
    operation: OperationsRecord;
    submitting: boolean;
    onCancel: (operation: OperationsRecord) => Promise<void>;
    onRecover: (operation: OperationsRecord) => Promise<void>;
};

export type PublicVerification = {
    status: "not_run" | "succeeded" | "failed";
    operationId: string;
    checkedAt: string | null;
    errorCode: string;
    error: string;
};

```

Add required field `publicVerification: PublicVerification` to the existing `OperationsOverview` alongside its current controller, release, operation, backup, rollback, and update fields; do not replace or make those existing fields optional.

Use `OperationActivePanel` to remove orchestration/state rendering from the 452-line page. Show a stop icon only for cancellable active statuses; after acceptance, replace it with the exact safe-point copy. For `recovery_required`, disable upgrade/rollback/backup/verify and expose only evidence plus explicit recovery confirmation. Show controller handoff warnings as warnings, not failed business upgrades. Show public verification independently as `未执行`, `成功`, or `失败`.

- [ ] **Step 4: Run Web tests, typecheck, and production build**

Run: `cd web && bun test test/admin-operations-recovery.test.ts test/admin-operations-unified-layout.test.ts && bun run build`

Expected: PASS with no TypeScript errors, no `any`, and bundle budget passing.

- [ ] **Step 5: Commit the admin UI checkpoint**

```bash
git add web/src/services/api/operations.ts web/src/pages/admin/operations/operation-active-panel.tsx web/src/pages/admin/operations/operations-page.tsx web/src/pages/admin/operations/operations-presenters.tsx web/test/admin-operations-recovery.test.ts
git commit -m "feat(admin): control durable upgrade operations"
```

### Task 9: Add release gates, fault injection, docs, and final review

**Files:**
- Modify: `deploy/tests/hmaigc-smoke.sh:1-282`
- Create: `deploy/tests/ops-runner-fault-injection.sh`
- Modify: `.github/workflows/publish-images.yml:80-336`
- Modify: `deploy/README.md:1-140`
- Modify: `PRODUCTION.md:1-178`
- Modify: `CHANGELOG.md:1-40`
- Modify: `docs/superpowers/specs/2026-08-25-durable-one-click-upgrade-design.md`
- Modify: `docs/superpowers/plans/2026-08-25-durable-one-click-upgrade.md`

**Interfaces:**
- Consumes: complete implementation.
- Produces: reproducible fault-injection gate and operator runbook.

- [ ] **Step 1: Add fault-injection cases with explicit expected states**

```bash
run_fault controller_restart online_preflight
assert_operation_status running
assert_single_runner

run_fault runner_kill backing_up
assert_operation_status recovering
resume_recovery
assert_service_state current_restored

run_fault runner_kill committing_release
resume_recovery
assert_release_version v1.0.58
assert_service_state target_online

run_fault candidate_controller_health_failure controller_handoff
assert_operation_status succeeded
assert_operation_warning controller_handoff_failed
assert_controller_digest "$OLD_CONTROLLER_DIGEST"

run_fault public_cdn_timeout public_verifying
assert_operation_status failed
assert_release_version v1.0.58
```

- [ ] **Step 2: Run the fault gate and fix only evidence-backed failures**

Run: `bash deploy/tests/ops-runner-fault-injection.sh`

Expected: PASS for normal upgrade, duplicate request, controller restart at every stage, Runner termination around every checkpoint, backup corruption, target health failure, repeated recovery interruption, cancel at each safe point, controller handoff restoration, and CDN isolation.

- [ ] **Step 3: Add CI/release assertions and update operational docs**

Add these release assertions after the existing ops-image build, using the already built local CI tag, and make each command a required workflow step:

```yaml
- name: Verify durable ops binaries and read-only validator
  run: |
    docker run --rm --entrypoint hmaigc-ops-runner hmaigc-ops-controller:ci --help
    docker run --rm --entrypoint hmaigc-ops-bootstrap hmaigc-ops-controller:ci --help
- name: Run durable upgrade smoke gates
  run: |
    bash deploy/tests/hmaigc-stage-smoke.sh
    bash deploy/tests/hmaigc-bootstrap-smoke.sh
    bash deploy/tests/ops-runner-fault-injection.sh
```

The bootstrap smoke must create a complete canonical fixture and invoke `hmaigc-ops-controller validate` against it, then checksum the fixture before and after to prove validation is read-only. Document the one-time bootstrap as a separately approved high-risk operation, normal backend one-click flow, safe cancellation, `recovery_required`, controller handoff warning semantics, disaster CLI, and the rule that public verify never rolls back business.

The fixture used by workflow and local Compose checks must define `HMAIGC_BACKEND_IMAGE=example.invalid/hmaigc-backend@sha256:<64 hex>`, `HMAIGC_WEB_IMAGE=example.invalid/hmaigc-web@sha256:<64 hex>`, and `HMAIGC_OPS_IMAGE=example.invalid/hmaigc-ops-controller@sha256:<64 hex>`; tests assert tag-only references and empty digest variables are rejected.

- [ ] **Step 4: Run the complete final gate once**

Run:

```bash
(cd backend && go test ./... -count=1)
(cd backend && go build ./...)
(cd backend && go test -race ./internal/opsprotocol ./internal/opsstate ./internal/opsrunner ./internal/opscontroller ./internal/service -count=1)
bash -n deploy/*.sh deploy/lib/*.sh deploy/tests/*.sh
bash deploy/tests/hmaigc-smoke.sh
bash deploy/tests/hmaigc-stage-smoke.sh
bash deploy/tests/hmaigc-bootstrap-smoke.sh
bash deploy/tests/ops-runner-fault-injection.sh
(cd web && bun test)
(cd web && bun run build)
docker compose --env-file deploy/tests/fixtures/production.env -f docker-compose.production.yml config -q
docker compose --env-file deploy/tests/fixtures/ops.env -f deploy/docker-compose.ops.yml config -q
git diff --check
```

Expected: all commands exit 0. If ShellCheck exists, also run `shellcheck deploy/*.sh deploy/lib/*.sh deploy/tests/*.sh` and require exit 0.

- [ ] **Step 5: Perform one explicit review, one concentrated fix pass, and one targeted re-review**

Review against the approved spec, this plan, actual diff, protocol names, SQLite projection migration, operation-file ownership, HMAC/permissions, digest pinning, fencing, idempotency, cancellation, recovery, public verification isolation, documentation, and test evidence. Classify findings as current-diff defect, existing debt, or new scope. Fix current-diff defects in one pass; rerun only affected focused gates; then perform one targeted re-review. If the targeted re-review still finds a new cross-module Critical/Important issue, stop and return to architecture rather than adding a third patch layer.

- [ ] **Step 6: Create the focused completion commit**

```bash
git add .github/workflows/publish-images.yml CHANGELOG.md PRODUCTION.md deploy/README.md deploy/tests/hmaigc-smoke.sh deploy/tests/ops-runner-fault-injection.sh
git add -f docs/superpowers/specs/2026-08-25-durable-one-click-upgrade-design.md docs/superpowers/plans/2026-08-25-durable-one-click-upgrade.md
git diff --cached --check
git diff --cached --stat
git commit -m "feat(ops): make one-click upgrades recoverable"
```

Confirm the unrelated `web/src/pages/prototype/` directory is not staged. Do not push, tag, publish, deploy, or run the production bootstrap.

## Implementation record (2026-08-26)

- The protocol, journal, detached Runner, recovery, rollback, bootstrap, controller reconciliation, admin controls, digest pinning, and documentation steps were implemented as one focused delivery. The planned intermediate checkpoint commits were intentionally consolidated into the final single-responsibility commit.
- Explicit review completed against the approved design, actual diff, frontend/backend contract, SQLite projection, HMAC and file permissions, generation fencing, idempotency, cancellation, recovery, controller handoff, public verification isolation, and operational documentation. One concentrated fix pass addressed ambiguous Docker start outcomes; the targeted re-review found no remaining cross-module Critical or Important issue.
- Verification passed: `go test ./... -count=1`, `go build ./...`, `go vet ./...`, affected `go test -race`, 658 Web tests, Web typecheck/production build/bundle budgets, shell syntax and all deployment smoke/fault-injection suites, both Compose configurations, a clean Linux ops image build, Runner/bootstrap binary checks, and `git diff --check`.
- No production bootstrap, cloud upgrade, push, tag, or release publication was performed. The unrelated `web/src/pages/prototype/` work remains outside this delivery.
