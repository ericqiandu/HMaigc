package opsprotocol

import (
	"encoding/json"
	"time"
)

type Action string

const (
	ActionInstall  Action = "install"
	ActionUpgrade  Action = "upgrade"
	ActionRollback Action = "rollback"
	ActionBackup   Action = "backup"
	ActionVerify   Action = "verify"
)

type OperationStatus string

const (
	OperationQueued           OperationStatus = "queued"
	OperationRunning          OperationStatus = "running"
	OperationCancelling       OperationStatus = "cancelling"
	OperationRecovering       OperationStatus = "recovering"
	OperationSucceeded        OperationStatus = "succeeded"
	OperationFailed           OperationStatus = "failed"
	OperationCancelled        OperationStatus = "cancelled"
	OperationRecoveryRequired OperationStatus = "recovery_required"
)

type OperationStage string

const (
	StageAccepted                OperationStage = "accepted"
	StageRunnerPreparing         OperationStage = "runner_preparing"
	StageOnlinePreflight         OperationStage = "online_preflight"
	StagePublicVerifying         OperationStage = "public_verifying"
	StageQuiescing               OperationStage = "quiescing"
	StageQuiescedAudit           OperationStage = "quiesced_audit"
	StageBackingUp               OperationStage = "backing_up"
	StageStartingTarget          OperationStage = "starting_target"
	StageVerifyingTarget         OperationStage = "verifying_target"
	StageRestoringCurrent        OperationStage = "restoring_current"
	StageRestoringBackup         OperationStage = "restoring_backup"
	StageRestoringRollbackBackup OperationStage = "restoring_rollback_backup"
	StageCommittingRelease       OperationStage = "committing_release"
	StageControllerHandoff       OperationStage = "controller_handoff"
	StageCompleted               OperationStage = "completed"
)

type OperationErrorCode string

const (
	ErrorPreflightFailed         OperationErrorCode = "preflight_failed"
	ErrorPublicVerifyFailed      OperationErrorCode = "public_verify_failed"
	ErrorRunnerStartFailed       OperationErrorCode = "runner_start_failed"
	ErrorLeaseLost               OperationErrorCode = "lease_lost"
	ErrorQuiesceFailed           OperationErrorCode = "quiesce_failed"
	ErrorBackupFailed            OperationErrorCode = "backup_failed"
	ErrorMigrationFailed         OperationErrorCode = "migration_failed"
	ErrorTargetHealthFailed      OperationErrorCode = "target_health_failed"
	ErrorRestoreFailed           OperationErrorCode = "restore_failed"
	ErrorControllerHandoffFailed OperationErrorCode = "controller_handoff_failed"
	ErrorStateConflict           OperationErrorCode = "state_conflict"
	ErrorCancelledAtSafePoint    OperationErrorCode = "cancelled_at_safe_point"
)

type ServiceState string

const (
	ServiceCurrentOnline   ServiceState = "current_online"
	ServiceMaintenance     ServiceState = "maintenance"
	ServiceTargetOnline    ServiceState = "target_online"
	ServiceCurrentRestored ServiceState = "current_restored"
	ServiceUnknown         ServiceState = "unknown"
)

type ControllerHandoff string

const (
	ControllerHandoffUnchanged        ControllerHandoff = "unchanged"
	ControllerHandoffUpdated          ControllerHandoff = "updated"
	ControllerHandoffRestoredPrevious ControllerHandoff = "restored_previous"
)

type RunnerSource string

const (
	RunnerSourceCurrent RunnerSource = "current"
	RunnerSourceTarget  RunnerSource = "target"
)

type RecoveryAction string

const (
	RecoveryNone                      RecoveryAction = "none"
	RecoveryRetryPreflight            RecoveryAction = "retry_preflight"
	RecoveryRestoreCurrent            RecoveryAction = "restore_current"
	RecoveryRestoreBackup             RecoveryAction = "restore_backup"
	RecoveryCommitTarget              RecoveryAction = "commit_target"
	RecoveryContinueControllerHandoff RecoveryAction = "continue_controller_handoff"
	RecoveryRequireOperator           RecoveryAction = "require_operator"
)

type PublicVerificationStatus string

const (
	PublicVerificationNotRun    PublicVerificationStatus = "not_run"
	PublicVerificationSucceeded PublicVerificationStatus = "succeeded"
	PublicVerificationFailed    PublicVerificationStatus = "failed"
)

type OperationWarning struct {
	Code    OperationErrorCode `json:"code"`
	Message string             `json:"message"`
	Facts   json.RawMessage    `json:"facts,omitempty"`
}

type StartOperationRequest struct {
	Action           Action `json:"action"`
	TargetVersion    string `json:"targetVersion,omitempty"`
	ActorUserID      string `json:"actorUserId"`
	ActorDisplayName string `json:"actorDisplayName"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Confirmation     string `json:"confirmation"`
}

type Operation struct {
	ID                       string             `json:"id"`
	Action                   Action             `json:"action"`
	TargetVersion            string             `json:"targetVersion,omitempty"`
	CurrentVersionAtStart    string             `json:"currentVersionAtStart,omitempty"`
	ResultVersion            string             `json:"resultVersion,omitempty"`
	Status                   OperationStatus    `json:"status"`
	Stage                    OperationStage     `json:"stage"`
	Phase                    string             `json:"phase"`
	RunnerVersion            string             `json:"runnerVersion,omitempty"`
	RunnerDigest             string             `json:"runnerDigest,omitempty"`
	RunnerGeneration         uint64             `json:"runnerGeneration,omitempty"`
	HeartbeatAt              *time.Time         `json:"heartbeatAt,omitempty"`
	ServiceState             ServiceState       `json:"serviceState"`
	CheckpointSequence       uint64             `json:"checkpointSequence,omitempty"`
	CancelRequestedAt        *time.Time         `json:"cancelRequestedAt,omitempty"`
	RecoveryAction           RecoveryAction     `json:"recoveryAction,omitempty"`
	ControllerVersionAtStart string             `json:"controllerVersionAtStart,omitempty"`
	ResultControllerVersion  string             `json:"resultControllerVersion,omitempty"`
	ControllerHandoff        ControllerHandoff  `json:"controllerHandoff,omitempty"`
	Warnings                 []OperationWarning `json:"warnings,omitempty"`
	ErrorCode                OperationErrorCode `json:"errorCode,omitempty"`
	Error                    string             `json:"error,omitempty"`
	ExitCode                 *int               `json:"exitCode,omitempty"`
	ActorUserID              string             `json:"actorUserId"`
	ActorDisplayName         string             `json:"actorDisplayName"`
	IdempotencyKey           string             `json:"idempotencyKey"`
	CreatedAt                time.Time          `json:"createdAt"`
	StartedAt                *time.Time         `json:"startedAt,omitempty"`
	CompletedAt              *time.Time         `json:"completedAt,omitempty"`
	UpdatedAt                time.Time          `json:"updatedAt"`
}

type OperationRequestFile struct {
	OperationID              string                `json:"operationId"`
	Request                  StartOperationRequest `json:"request"`
	RequestHash              string                `json:"requestHash"`
	CurrentVersion           string                `json:"currentVersion,omitempty"`
	PreviousVersion          string                `json:"previousVersion,omitempty"`
	ExpectedVersion          string                `json:"expectedVersion"`
	RollbackBackup           string                `json:"rollbackBackup,omitempty"`
	RunnerSource             RunnerSource          `json:"runnerSource"`
	ControllerVersionAtStart string                `json:"controllerVersionAtStart"`
	ImportedTerminal         bool                  `json:"importedTerminal,omitempty"`
	CreatedAt                time.Time             `json:"createdAt"`
}

type SignedCommandFile struct {
	SchemaVersion uint32          `json:"schemaVersion"`
	Payload       json.RawMessage `json:"payload"`
	Signature     string          `json:"signature"`
}

type RunnerLaunchCommand struct {
	OperationID    string         `json:"operationId"`
	Generation     uint64         `json:"generation"`
	FencingToken   string         `json:"fencingToken"`
	RunnerDigest   string         `json:"runnerDigest"`
	RecoveryAction RecoveryAction `json:"recoveryAction,omitempty"`
	IssuedAt       time.Time      `json:"issuedAt"`
}

type RunnerLease struct {
	OperationID  string    `json:"operationId"`
	Generation   uint64    `json:"generation"`
	TokenHash    string    `json:"tokenHash"`
	RunnerDigest string    `json:"runnerDigest"`
	AcquiredAt   time.Time `json:"acquiredAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type RunnerHeartbeat struct {
	OperationID  string         `json:"operationId"`
	Generation   uint64         `json:"generation"`
	Sequence     uint64         `json:"sequence"`
	Stage        OperationStage `json:"stage"`
	ServiceState ServiceState   `json:"serviceState"`
	ObservedAt   time.Time      `json:"observedAt"`
}

type CancelOperationRequest struct {
	ActorUserID      string `json:"actorUserId"`
	ActorDisplayName string `json:"actorDisplayName"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Confirmation     string `json:"confirmation"`
}

type CancelOperationCommand struct {
	OperationID      string    `json:"operationId"`
	RequestHash      string    `json:"requestHash"`
	ActorUserID      string    `json:"actorUserId"`
	ActorDisplayName string    `json:"actorDisplayName"`
	RequestedAt      time.Time `json:"requestedAt"`
	Nonce            string    `json:"nonce"`
}

type RecoverOperationRequest struct {
	ActorUserID      string `json:"actorUserId"`
	ActorDisplayName string `json:"actorDisplayName"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Confirmation     string `json:"confirmation"`
}

type RecoverOperationCommand struct {
	OperationID      string         `json:"operationId"`
	RequestHash      string         `json:"requestHash"`
	ActorUserID      string         `json:"actorUserId"`
	ActorDisplayName string         `json:"actorDisplayName"`
	RecoveryAction   RecoveryAction `json:"recoveryAction"`
	RequestedAt      time.Time      `json:"requestedAt"`
	Nonce            string         `json:"nonce"`
}

type OperationCheckpoint struct {
	OperationID              string             `json:"operationId"`
	Action                   Action             `json:"action"`
	TargetVersion            string             `json:"targetVersion,omitempty"`
	RunnerDigest             string             `json:"runnerDigest"`
	Generation               uint64             `json:"generation"`
	FencingTokenHash         string             `json:"fencingTokenHash"`
	Sequence                 uint64             `json:"sequence"`
	Stage                    OperationStage     `json:"stage"`
	PreviousCompletedStage   OperationStage     `json:"previousCompletedStage,omitempty"`
	StageStartedAt           *time.Time         `json:"stageStartedAt,omitempty"`
	StageCompletedAt         *time.Time         `json:"stageCompletedAt,omitempty"`
	CurrentVersion           string             `json:"currentVersion,omitempty"`
	ExpectedVersion          string             `json:"expectedVersion,omitempty"`
	ServiceState             ServiceState       `json:"serviceState"`
	WritesQuiesced           bool               `json:"writesQuiesced"`
	VerifiedRecoveryPoint    bool               `json:"verifiedRecoveryPoint"`
	BackupPath               string             `json:"backupPath,omitempty"`
	BackupChecksumStatus     string             `json:"backupChecksumStatus,omitempty"`
	DataMigrationStarted     bool               `json:"dataMigrationStarted"`
	TargetBackendHealthy     bool               `json:"targetBackendHealthy"`
	TargetWebHealthy         bool               `json:"targetWebHealthy"`
	ReleaseCommitted         bool               `json:"releaseCommitted"`
	BackendImage             string             `json:"backendImage,omitempty"`
	WebImage                 string             `json:"webImage,omitempty"`
	BackupHelperImage        string             `json:"backupHelperImage,omitempty"`
	CurrentControllerVersion string             `json:"currentControllerVersion,omitempty"`
	CurrentControllerDigest  string             `json:"currentControllerDigest,omitempty"`
	CandidateControllerImage string             `json:"candidateControllerImage,omitempty"`
	ControllerHandoff        ControllerHandoff  `json:"controllerHandoff,omitempty"`
	Warnings                 []OperationWarning `json:"warnings,omitempty"`
	CancelRequested          bool               `json:"cancelRequested"`
	NextSafeAction           RecoveryAction     `json:"nextSafeAction,omitempty"`
	FailureCode              OperationErrorCode `json:"failureCode,omitempty"`
	FailureMessage           string             `json:"failureMessage,omitempty"`
	UpdatedAt                time.Time          `json:"updatedAt"`
}

type OperationEvent struct {
	OperationID string          `json:"operationId"`
	Generation  uint64          `json:"generation"`
	Sequence    uint64          `json:"sequence"`
	Kind        string          `json:"kind"`
	Stage       OperationStage  `json:"stage"`
	Stream      string          `json:"stream"`
	Message     string          `json:"message"`
	Facts       json.RawMessage `json:"facts,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type OperationResult struct {
	OperationID       string             `json:"operationId"`
	Generation        uint64             `json:"generation"`
	Status            OperationStatus    `json:"status"`
	ResultVersion     string             `json:"resultVersion,omitempty"`
	ControllerVersion string             `json:"controllerVersion,omitempty"`
	ServiceState      ServiceState       `json:"serviceState"`
	ControllerHandoff ControllerHandoff  `json:"controllerHandoff"`
	Warnings          []OperationWarning `json:"warnings,omitempty"`
	ErrorCode         OperationErrorCode `json:"errorCode,omitempty"`
	Error             string             `json:"error,omitempty"`
	ExitCode          *int               `json:"exitCode,omitempty"`
	CompletedAt       time.Time          `json:"completedAt"`
}

type PublicVerification struct {
	Status      PublicVerificationStatus `json:"status"`
	OperationID string                   `json:"operationId"`
	CheckedAt   *time.Time               `json:"checkedAt"`
	ErrorCode   OperationErrorCode       `json:"errorCode"`
	Error       string                   `json:"error"`
}

type OperationPage struct {
	Items []Operation `json:"items"`
	Total int64       `json:"total"`
}

type OperationLog struct {
	Sequence    uint64    `json:"sequence"`
	OperationID string    `json:"operationId"`
	Stream      string    `json:"stream"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"createdAt"`
}

type OperationLogPage struct {
	Items      []OperationLog `json:"items"`
	NextCursor uint64         `json:"nextCursor"`
}

type Backup struct {
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	Version         string    `json:"version"`
	CreatedAt       time.Time `json:"createdAt"`
	SizeBytes       int64     `json:"sizeBytes"`
	ChecksumStatus  string    `json:"checksumStatus"`
	ValidationError string    `json:"validationError,omitempty"`
}

type ReleaseCheck struct {
	Status          string    `json:"status"`
	CurrentVersion  string    `json:"currentVersion,omitempty"`
	LatestVersion   string    `json:"latestVersion,omitempty"`
	UpdateAvailable bool      `json:"updateAvailable"`
	CheckedAt       time.Time `json:"checkedAt"`
	Message         string    `json:"message,omitempty"`
}

type ControllerInfo struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type Overview struct {
	Controller         ControllerInfo     `json:"controller"`
	Release            ReleaseCheck       `json:"release"`
	ActiveOperation    *Operation         `json:"activeOperation,omitempty"`
	LatestOperation    *Operation         `json:"latestOperation,omitempty"`
	LatestBackup       *Backup            `json:"latestBackup,omitempty"`
	RollbackReady      bool               `json:"rollbackReady"`
	RollbackStatus     string             `json:"rollbackStatus"`
	PreviousVersion    string             `json:"previousVersion,omitempty"`
	PublicVerification PublicVerification `json:"publicVerification"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}
