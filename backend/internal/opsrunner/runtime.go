package opsrunner

import (
	"context"
	"fmt"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

const (
	EventKindIntent  = "intent"
	EventKindFact    = "fact"
	EventKindFailure = "failure"
)

type Runtime interface {
	Execute(context.Context, StageInput) (StageOutput, error)
}

type Clock func() time.Time

type RunInput struct {
	OperationID    string
	Generation     uint64
	FencingToken   string
	RunnerDigest   string
	RecoveryAction opsprotocol.RecoveryAction
	CommandSecret  []byte
}

type StageInput struct {
	Request      opsprotocol.OperationRequestFile
	Checkpoint   opsprotocol.OperationCheckpoint
	Stage        opsprotocol.OperationStage
	FencingToken string
}

type StageOutput struct {
	ServiceState          opsprotocol.ServiceState       `json:"serviceState"`
	CurrentVersion        string                         `json:"currentVersion,omitempty"`
	ResultVersion         string                         `json:"resultVersion,omitempty"`
	BackupPath            string                         `json:"backupPath,omitempty"`
	BackupChecksumStatus  string                         `json:"backupChecksumStatus,omitempty"`
	BackendImage          string                         `json:"backendImage,omitempty"`
	WebImage              string                         `json:"webImage,omitempty"`
	BackupHelperImage     string                         `json:"backupHelperImage,omitempty"`
	ControllerImage       string                         `json:"controllerImage,omitempty"`
	ControllerVersion     string                         `json:"controllerVersion,omitempty"`
	ControllerHandoff     opsprotocol.ControllerHandoff  `json:"controllerHandoff,omitempty"`
	WritesQuiesced        bool                           `json:"writesQuiesced"`
	VerifiedRecoveryPoint bool                           `json:"verifiedRecoveryPoint"`
	DataMigrationStarted  bool                           `json:"dataMigrationStarted"`
	TargetBackendHealthy  bool                           `json:"targetBackendHealthy"`
	TargetWebHealthy      bool                           `json:"targetWebHealthy"`
	ReleaseCommitted      bool                           `json:"releaseCommitted"`
	Warnings              []opsprotocol.OperationWarning `json:"warnings,omitempty"`
}

func stagesFor(action opsprotocol.Action) ([]opsprotocol.OperationStage, error) {
	switch action {
	case opsprotocol.ActionUpgrade:
		return []opsprotocol.OperationStage{
			opsprotocol.StageOnlinePreflight,
			opsprotocol.StageQuiescing,
			opsprotocol.StageQuiescedAudit,
			opsprotocol.StageBackingUp,
			opsprotocol.StageStartingTarget,
			opsprotocol.StageVerifyingTarget,
			opsprotocol.StageCommittingRelease,
			opsprotocol.StageControllerHandoff,
		}, nil
	case opsprotocol.ActionVerify:
		return []opsprotocol.OperationStage{opsprotocol.StagePublicVerifying}, nil
	case opsprotocol.ActionBackup:
		return []opsprotocol.OperationStage{
			opsprotocol.StageOnlinePreflight,
			opsprotocol.StageQuiescing,
			opsprotocol.StageBackingUp,
			opsprotocol.StageStartingTarget,
			opsprotocol.StageVerifyingTarget,
		}, nil
	case opsprotocol.ActionRollback:
		return []opsprotocol.OperationStage{
			opsprotocol.StageOnlinePreflight,
			opsprotocol.StageQuiescing,
			opsprotocol.StageBackingUp,
			opsprotocol.StageRestoringRollbackBackup,
			opsprotocol.StageStartingTarget,
			opsprotocol.StageVerifyingTarget,
			opsprotocol.StageCommittingRelease,
			opsprotocol.StageControllerHandoff,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported operation action %q", action)
	}
}
