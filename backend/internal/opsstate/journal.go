package opsstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/opsprotocol"
)

// Journal persists the authoritative facts for operations that must survive
// controller replacement and process restarts.
type Journal struct {
	layout *Layout
}

func NewJournal(root string) (*Journal, error) {
	layout, err := newLayout(root)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(layout.root); err != nil {
		return nil, err
	}
	return &Journal{layout: layout}, nil
}

func (j *Journal) Layout() *Layout {
	return j.layout
}

func (j *Journal) CreateRequest(request opsprotocol.OperationRequestFile) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	path, err := j.layout.RequestPath(request.OperationID)
	if err != nil {
		return err
	}
	encoded, err := marshalFact(request)
	if err != nil {
		return err
	}
	return createBytesImmutable(path, 0o600, encoded, true)
}

func (j *Journal) ReadRequest(operationID string) (opsprotocol.OperationRequestFile, error) {
	path, err := j.layout.RequestPath(operationID)
	if err != nil {
		return opsprotocol.OperationRequestFile{}, err
	}
	request, err := readStrictJSON[opsprotocol.OperationRequestFile](path)
	if err != nil {
		return opsprotocol.OperationRequestFile{}, err
	}
	if request.OperationID != operationID {
		return opsprotocol.OperationRequestFile{}, ErrCommandMismatch
	}
	if err := validateRequest(request); err != nil {
		return opsprotocol.OperationRequestFile{}, fmt.Errorf("%w: %v", ErrCorruptFact, err)
	}
	return request, nil
}

func (j *Journal) ListRequests() ([]opsprotocol.OperationRequestFile, error) {
	operationsRoot := filepath.Join(j.layout.Root(), operationsDirectory)
	entries, err := os.ReadDir(operationsRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return []opsprotocol.OperationRequestFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	requests := make([]opsprotocol.OperationRequestFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		request, err := j.ReadRequest(entry.Name())
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	sort.Slice(requests, func(left int, right int) bool {
		return requests[left].CreatedAt.Before(requests[right].CreatedAt)
	})
	return requests, nil
}

func (j *Journal) WriteLease(operationID string, lease opsprotocol.RunnerLease) error {
	if lease.OperationID != operationID {
		return ErrCommandMismatch
	}
	existing, err := j.ReadLease(operationID)
	if err == nil {
		if existing.Generation > lease.Generation {
			return ErrImmutableFactExists
		}
		if existing.Generation == lease.Generation &&
			(existing.TokenHash != lease.TokenHash || existing.RunnerDigest != lease.RunnerDigest) {
			return ErrImmutableFactExists
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeMutableFact(j, operationID, j.layout.LeasePath, lease)
}

func (j *Journal) ReadLease(operationID string) (opsprotocol.RunnerLease, error) {
	lease, err := readMutableFact[opsprotocol.RunnerLease](j, operationID, j.layout.LeasePath)
	if err != nil {
		return opsprotocol.RunnerLease{}, err
	}
	if lease.OperationID != operationID {
		return opsprotocol.RunnerLease{}, ErrCommandMismatch
	}
	return lease, nil
}

func (j *Journal) WriteHeartbeat(operationID string, heartbeat opsprotocol.RunnerHeartbeat) error {
	if heartbeat.OperationID != operationID {
		return ErrCommandMismatch
	}
	return writeMutableFact(j, operationID, j.layout.HeartbeatPath, heartbeat)
}

func (j *Journal) ReadHeartbeat(operationID string) (opsprotocol.RunnerHeartbeat, error) {
	heartbeat, err := readMutableFact[opsprotocol.RunnerHeartbeat](j, operationID, j.layout.HeartbeatPath)
	if err != nil {
		return opsprotocol.RunnerHeartbeat{}, err
	}
	if heartbeat.OperationID != operationID {
		return opsprotocol.RunnerHeartbeat{}, ErrCommandMismatch
	}
	return heartbeat, nil
}

func (j *Journal) WriteCheckpoint(operationID string, checkpoint opsprotocol.OperationCheckpoint) error {
	if checkpoint.OperationID != operationID {
		return ErrCommandMismatch
	}
	path, err := j.layout.CheckpointPath(operationID)
	if err != nil {
		return err
	}
	encoded, err := marshalFact(checkpoint)
	if err != nil {
		return err
	}
	existing, readErr := j.ReadCheckpoint(operationID)
	if readErr == nil {
		if existing.Generation > checkpoint.Generation {
			return ErrImmutableFactExists
		}
		if existing.Generation == checkpoint.Generation {
			if existing.FencingTokenHash != checkpoint.FencingTokenHash || existing.Sequence > checkpoint.Sequence {
				return ErrImmutableFactExists
			}
			if existing.Sequence == checkpoint.Sequence {
				existingEncoded, marshalErr := marshalFact(existing)
				if marshalErr != nil {
					return marshalErr
				}
				if bytes.Equal(existingEncoded, encoded) {
					return nil
				}
				return ErrImmutableFactExists
			}
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	return writeBytesAtomic(path, 0o600, encoded)
}

func (j *Journal) ReadCheckpoint(operationID string) (opsprotocol.OperationCheckpoint, error) {
	checkpoint, err := readMutableFact[opsprotocol.OperationCheckpoint](j, operationID, j.layout.CheckpointPath)
	if err != nil {
		return opsprotocol.OperationCheckpoint{}, err
	}
	if checkpoint.OperationID != operationID {
		return opsprotocol.OperationCheckpoint{}, ErrCommandMismatch
	}
	return checkpoint, nil
}

func (j *Journal) WriteResult(operationID string, result opsprotocol.OperationResult) error {
	if result.OperationID != operationID {
		return ErrCommandMismatch
	}
	if !isRunnerResultStatus(result.Status) {
		return fmt.Errorf("operation result status must be terminal: %s", result.Status)
	}
	path, err := j.layout.ResultPath(operationID)
	if err != nil {
		return err
	}
	encoded, err := marshalFact(result)
	if err != nil {
		return err
	}
	existing, readErr := j.ReadResult(operationID)
	if errors.Is(readErr, fs.ErrNotExist) {
		return createBytesImmutable(path, 0o600, encoded, true)
	}
	if readErr != nil {
		return readErr
	}
	if existing.Generation > result.Generation {
		return ErrImmutableFactExists
	}
	if existing.Generation == result.Generation {
		existingEncoded, marshalErr := marshalFact(existing)
		if marshalErr != nil {
			return marshalErr
		}
		if bytes.Equal(existingEncoded, encoded) {
			return nil
		}
		return ErrImmutableFactExists
	}
	if existing.Status != opsprotocol.OperationRecoveryRequired {
		return ErrImmutableFactExists
	}
	return writeBytesAtomic(path, 0o600, encoded)
}

func (j *Journal) ReadResult(operationID string) (opsprotocol.OperationResult, error) {
	result, err := readMutableFact[opsprotocol.OperationResult](j, operationID, j.layout.ResultPath)
	if err != nil {
		return opsprotocol.OperationResult{}, err
	}
	if result.OperationID != operationID {
		return opsprotocol.OperationResult{}, ErrCommandMismatch
	}
	return result, nil
}

func (j *Journal) AppendEvent(event opsprotocol.OperationEvent) error {
	path, err := j.layout.EventPath(event.OperationID, event.Generation, event.Sequence)
	if err != nil {
		return err
	}
	encoded, err := marshalFact(event)
	if err != nil {
		return err
	}
	return createBytesImmutable(path, 0o600, encoded, false)
}

func (j *Journal) ReadEvents(operationID string, generation uint64, after uint64) ([]opsprotocol.OperationEvent, error) {
	directory, err := j.layout.EventsDir(operationID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return []opsprotocol.OperationEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	events := make([]opsprotocol.OperationEvent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".hmaigc-") && strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		event, err := readStrictJSON[opsprotocol.OperationEvent](filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if event.OperationID != operationID {
			return nil, fmt.Errorf("%w: event operation id mismatch", ErrCorruptFact)
		}
		expectedPath, err := j.layout.EventPath(operationID, event.Generation, event.Sequence)
		if err != nil || filepath.Base(expectedPath) != entry.Name() {
			return nil, fmt.Errorf("%w: non-canonical event filename %q", ErrCorruptFact, entry.Name())
		}
		if event.Generation == generation && event.Sequence > after {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(left int, right int) bool {
		return events[left].Sequence < events[right].Sequence
	})
	return events, nil
}

type operationPath func(string) (string, error)

type mutableFact interface {
	opsprotocol.RunnerLease |
		opsprotocol.RunnerHeartbeat |
		opsprotocol.OperationCheckpoint |
		opsprotocol.OperationResult
}

func writeMutableFact[T mutableFact](j *Journal, operationID string, resolve operationPath, value T) error {
	path, err := resolve(operationID)
	if err != nil {
		return err
	}
	encoded, err := marshalFact(value)
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, 0o600, encoded)
}

func readMutableFact[T mutableFact](j *Journal, operationID string, resolve operationPath) (T, error) {
	var zero T
	path, err := resolve(operationID)
	if err != nil {
		return zero, err
	}
	return readStrictJSON[T](path)
}

func validateRequest(request opsprotocol.OperationRequestFile) error {
	if err := validateOperationID(request.OperationID); err != nil {
		return err
	}
	if len(request.RequestHash) != sha256.Size*2 {
		return fmt.Errorf("request hash must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(request.RequestHash); err != nil || request.RequestHash != strings.ToLower(request.RequestHash) {
		return fmt.Errorf("request hash must be lowercase hexadecimal")
	}
	if err := opsprotocol.ValidateReleaseVersion(request.ExpectedVersion); err != nil {
		return fmt.Errorf("expected version is invalid: %w", err)
	}
	if request.RunnerSource != opsprotocol.RunnerSourceCurrent && request.RunnerSource != opsprotocol.RunnerSourceTarget {
		return fmt.Errorf("invalid runner source %q", request.RunnerSource)
	}
	if err := validateRequestVersionContract(request); err != nil {
		return err
	}
	if request.CreatedAt.IsZero() {
		return fmt.Errorf("request creation time is required")
	}
	return nil
}

func validateRequestVersionContract(request opsprotocol.OperationRequestFile) error {
	switch request.Request.Action {
	case opsprotocol.ActionInstall, opsprotocol.ActionUpgrade:
		if request.Request.TargetVersion != request.ExpectedVersion || request.RunnerSource != opsprotocol.RunnerSourceTarget || request.RollbackBackup != "" {
			return fmt.Errorf("version-changing request target, expected version, and runner source disagree")
		}
	case opsprotocol.ActionRollback:
		if request.Request.TargetVersion != "" || request.PreviousVersion != request.ExpectedVersion ||
			request.RunnerSource != opsprotocol.RunnerSourceCurrent ||
			(request.RollbackBackup == "" && !request.ImportedTerminal) {
			return fmt.Errorf("rollback request does not match its immutable previous version")
		}
	case opsprotocol.ActionBackup, opsprotocol.ActionVerify:
		if request.Request.TargetVersion != "" || request.CurrentVersion != request.ExpectedVersion ||
			request.RunnerSource != opsprotocol.RunnerSourceCurrent || request.RollbackBackup != "" {
			return fmt.Errorf("non-version-changing request does not match its immutable current version")
		}
	default:
		return fmt.Errorf("unsupported operation action %q", request.Request.Action)
	}
	return nil
}

func isRunnerResultStatus(status opsprotocol.OperationStatus) bool {
	return opsprotocol.IsTerminalStatus(status) || status == opsprotocol.OperationRecoveryRequired
}
