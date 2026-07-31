package opsprotocol

import "time"

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
	OperationQueued    OperationStatus = "queued"
	OperationRunning   OperationStatus = "running"
	OperationSucceeded OperationStatus = "succeeded"
	OperationFailed    OperationStatus = "failed"
)

type StartOperationRequest struct {
	Action           Action `json:"action"`
	TargetVersion    string `json:"targetVersion,omitempty"`
	ActorUserID      string `json:"actorUserId"`
	ActorDisplayName string `json:"actorDisplayName"`
	IdempotencyKey   string `json:"idempotencyKey"`
	Confirmation     string `json:"confirmation"`
}

type Operation struct {
	ID                    string          `json:"id"`
	Action                Action          `json:"action"`
	TargetVersion         string          `json:"targetVersion,omitempty"`
	CurrentVersionAtStart string          `json:"currentVersionAtStart,omitempty"`
	ResultVersion         string          `json:"resultVersion,omitempty"`
	Status                OperationStatus `json:"status"`
	Phase                 string          `json:"phase"`
	Error                 string          `json:"error,omitempty"`
	ExitCode              *int            `json:"exitCode,omitempty"`
	ActorUserID           string          `json:"actorUserId"`
	ActorDisplayName      string          `json:"actorDisplayName"`
	IdempotencyKey        string          `json:"idempotencyKey"`
	CreatedAt             time.Time       `json:"createdAt"`
	StartedAt             *time.Time      `json:"startedAt,omitempty"`
	CompletedAt           *time.Time      `json:"completedAt,omitempty"`
	UpdatedAt             time.Time       `json:"updatedAt"`
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
	Controller      ControllerInfo `json:"controller"`
	Release         ReleaseCheck   `json:"release"`
	ActiveOperation *Operation     `json:"activeOperation,omitempty"`
	LatestOperation *Operation     `json:"latestOperation,omitempty"`
	LatestBackup    *Backup        `json:"latestBackup,omitempty"`
	RollbackReady   bool           `json:"rollbackReady"`
	RollbackStatus  string         `json:"rollbackStatus"`
	PreviousVersion string         `json:"previousVersion,omitempty"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}
