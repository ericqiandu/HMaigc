package opsstate

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

const (
	commandSchemaVersion = 1
	minimumCommandSecret = 32
	maximumLaunchAge     = 30 * time.Minute
	maximumClockSkew     = time.Minute
)

func (j *Journal) WriteLaunchCommand(secret []byte, command opsprotocol.RunnerLaunchCommand) error {
	if err := validateCommandSecret(secret); err != nil {
		return err
	}
	if err := validateLaunchCommand(command, time.Now().UTC()); err != nil {
		return err
	}
	path, err := j.layout.LaunchCommandPath(command.OperationID)
	if err != nil {
		return err
	}
	if existing, readErr := j.readLaunchCommand(secret, command.OperationID, false); readErr == nil {
		if sameLaunchCommand(existing, command) {
			return nil
		}
		if command.Generation <= existing.Generation {
			return ErrImmutableFactExists
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return readErr
	}
	encoded, err := encodeSignedCommand(secret, command)
	if err != nil {
		return err
	}
	return writeBytesAtomic(path, 0o600, encoded)
}

func (j *Journal) ReadLaunchCommand(secret []byte, operationID string) (opsprotocol.RunnerLaunchCommand, error) {
	return j.readLaunchCommand(secret, operationID, true)
}

func (j *Journal) ReadLaunchCommandForReconciliation(secret []byte, operationID string) (opsprotocol.RunnerLaunchCommand, error) {
	return j.readLaunchCommand(secret, operationID, false)
}

func (j *Journal) readLaunchCommand(secret []byte, operationID string, enforceFreshness bool) (opsprotocol.RunnerLaunchCommand, error) {
	path, err := j.layout.LaunchCommandPath(operationID)
	if err != nil {
		return opsprotocol.RunnerLaunchCommand{}, err
	}
	command, err := readSignedCommand[opsprotocol.RunnerLaunchCommand](secret, path)
	if err != nil {
		return opsprotocol.RunnerLaunchCommand{}, err
	}
	if command.OperationID != operationID {
		return opsprotocol.RunnerLaunchCommand{}, ErrCommandMismatch
	}
	if err := validateLaunchCommandFields(command); err != nil {
		return opsprotocol.RunnerLaunchCommand{}, err
	}
	if enforceFreshness {
		if err := validateLaunchCommandFreshness(command, time.Now().UTC()); err != nil {
			return opsprotocol.RunnerLaunchCommand{}, err
		}
	}
	return command, nil
}

func (j *Journal) WriteCancelCommand(secret []byte, command opsprotocol.CancelOperationCommand) error {
	if err := validateCommandSecret(secret); err != nil {
		return err
	}
	if err := validateCancelCommand(command, time.Now().UTC()); err != nil {
		return err
	}
	path, err := j.layout.CancelCommandPath(command.OperationID)
	if err != nil {
		return err
	}
	encoded, err := encodeSignedCommand(secret, command)
	if err != nil {
		return err
	}
	return createBytesImmutable(path, 0o600, encoded, true)
}

func (j *Journal) ReadCancelCommand(secret []byte, operationID string) (*opsprotocol.CancelOperationCommand, error) {
	path, err := j.layout.CancelCommandPath(operationID)
	if err != nil {
		return nil, err
	}
	command, err := readSignedCommand[opsprotocol.CancelOperationCommand](secret, path)
	if err != nil {
		return nil, err
	}
	if command.OperationID != operationID {
		return nil, ErrCommandMismatch
	}
	if err := validateCancelCommand(command, time.Now().UTC()); err != nil {
		return nil, err
	}
	return &command, nil
}

func (j *Journal) WriteRecoverCommand(secret []byte, command opsprotocol.RecoverOperationCommand) error {
	if err := validateCommandSecret(secret); err != nil {
		return err
	}
	if err := validateRecoverCommand(command, time.Now().UTC()); err != nil {
		return err
	}
	path, err := j.layout.RecoverCommandPath(command.OperationID)
	if err != nil {
		return err
	}
	encoded, err := encodeSignedCommand(secret, command)
	if err != nil {
		return err
	}
	return createBytesImmutable(path, 0o600, encoded, true)
}

func (j *Journal) ReadRecoverCommand(secret []byte, operationID string) (*opsprotocol.RecoverOperationCommand, error) {
	path, err := j.layout.RecoverCommandPath(operationID)
	if err != nil {
		return nil, err
	}
	command, err := readSignedCommand[opsprotocol.RecoverOperationCommand](secret, path)
	if err != nil {
		return nil, err
	}
	if command.OperationID != operationID {
		return nil, ErrCommandMismatch
	}
	if err := validateRecoverCommand(command, time.Now().UTC()); err != nil {
		return nil, err
	}
	return &command, nil
}

type commandPayload interface {
	opsprotocol.RunnerLaunchCommand | opsprotocol.CancelOperationCommand | opsprotocol.RecoverOperationCommand
}

func encodeSignedCommand[T commandPayload](secret []byte, command T) ([]byte, error) {
	if err := validateCommandSecret(secret); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("marshal command payload: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	envelope := opsprotocol.SignedCommandFile{
		SchemaVersion: commandSchemaVersion,
		Payload:       payload,
		Signature:     hex.EncodeToString(mac.Sum(nil)),
	}
	return marshalFact(envelope)
}

func readSignedCommand[T commandPayload](secret []byte, path string) (T, error) {
	var command T
	if err := validateCommandSecret(secret); err != nil {
		return command, err
	}
	envelope, err := readStrictJSON[opsprotocol.SignedCommandFile](path)
	if err != nil {
		return command, err
	}
	if envelope.SchemaVersion != commandSchemaVersion {
		return command, fmt.Errorf("%w: unsupported schema version", ErrInvalidCommandSignature)
	}
	provided, err := hex.DecodeString(envelope.Signature)
	if err != nil || len(provided) != sha256.Size {
		return command, ErrInvalidCommandSignature
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(envelope.Payload)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return command, ErrInvalidCommandSignature
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		return command, fmt.Errorf("%w: decode signed command: %v", ErrCorruptFact, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return command, fmt.Errorf("%w: decode signed command: %v", ErrCorruptFact, err)
	}
	return command, nil
}

func validateLaunchCommand(command opsprotocol.RunnerLaunchCommand, now time.Time) error {
	if err := validateLaunchCommandFields(command); err != nil {
		return err
	}
	return validateLaunchCommandFreshness(command, now)
}

func validateLaunchCommandFields(command opsprotocol.RunnerLaunchCommand) error {
	if err := validateOperationID(command.OperationID); err != nil {
		return err
	}
	if command.Generation == 0 || command.FencingToken == "" {
		return fmt.Errorf("launch generation and fencing token are required")
	}
	if !isImmutableDigest(command.RunnerDigest) {
		return fmt.Errorf("runner digest must use an immutable SHA-256 reference")
	}
	if command.RecoveryAction != "" && !isExecutableRecoveryAction(command.RecoveryAction) {
		return fmt.Errorf("runner recovery action is not executable: %s", command.RecoveryAction)
	}
	if command.IssuedAt.IsZero() {
		return fmt.Errorf("launch issue time is required")
	}
	return nil
}

func validateLaunchCommandFreshness(command opsprotocol.RunnerLaunchCommand, now time.Time) error {
	if command.IssuedAt.After(now.Add(maximumClockSkew)) || now.Sub(command.IssuedAt) > maximumLaunchAge {
		return ErrCommandExpired
	}
	return nil
}

func sameLaunchCommand(left opsprotocol.RunnerLaunchCommand, right opsprotocol.RunnerLaunchCommand) bool {
	leftEncoded, leftErr := json.Marshal(left)
	rightEncoded, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftEncoded, rightEncoded)
}

func validateCancelCommand(command opsprotocol.CancelOperationCommand, now time.Time) error {
	if err := validateOperationID(command.OperationID); err != nil {
		return err
	}
	if command.RequestHash == "" || command.Nonce == "" || command.ActorUserID == "" {
		return fmt.Errorf("cancel request hash, nonce, and actor are required")
	}
	if command.RequestedAt.IsZero() || command.RequestedAt.After(now.Add(maximumClockSkew)) {
		return ErrCommandExpired
	}
	return nil
}

func validateRecoverCommand(command opsprotocol.RecoverOperationCommand, now time.Time) error {
	if err := validateOperationID(command.OperationID); err != nil {
		return err
	}
	if command.RequestHash == "" || command.Nonce == "" || command.ActorUserID == "" {
		return fmt.Errorf("recover request hash, nonce, and actor are required")
	}
	if !isExecutableRecoveryAction(command.RecoveryAction) {
		return fmt.Errorf("recover action is not executable: %s", command.RecoveryAction)
	}
	if command.RequestedAt.IsZero() || command.RequestedAt.After(now.Add(maximumClockSkew)) {
		return ErrCommandExpired
	}
	return nil
}

func isExecutableRecoveryAction(action opsprotocol.RecoveryAction) bool {
	switch action {
	case opsprotocol.RecoveryRestoreCurrent,
		opsprotocol.RecoveryRestoreBackup,
		opsprotocol.RecoveryCommitTarget,
		opsprotocol.RecoveryContinueControllerHandoff:
		return true
	default:
		return false
	}
}

func validateCommandSecret(secret []byte) error {
	if len(secret) < minimumCommandSecret {
		return fmt.Errorf("command signing secret must contain at least %d bytes", minimumCommandSecret)
	}
	return nil
}

func isImmutableDigest(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	if separator <= 0 {
		return false
	}
	digest := reference[separator+len("@sha256:"):]
	if len(digest) != sha256.Size*2 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
