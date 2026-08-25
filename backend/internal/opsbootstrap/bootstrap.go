package opsbootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsconfig"
	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/opsstate"

	"github.com/mattn/go-sqlite3"
)

var (
	ErrActiveLegacyOperation   = errors.New("legacy controller contains an active or unknown operation")
	ErrBootstrapSourceMismatch = errors.New("ops-state was bootstrapped from different source facts")
)

type Input struct {
	SourceDatabase    string
	SourceEnvironment string
	StateRoot         string
	ControllerImage   string
	ControllerVersion string
	ProtocolVersion   string
	StateVolume       string
}

type legacyOperation struct {
	ID, Action, TargetVersion, CurrentVersion, ResultVersion string
	Status, Phase, Error                                     string
	ExitCode                                                 sql.NullInt64
	ActorUserID, ActorDisplayName, IdempotencyKey            string
	CreatedAt, UpdatedAt                                     time.Time
	StartedAt, CompletedAt                                   sql.NullTime
}

type bootstrapMarker struct {
	SourceDatabaseSHA256    string `json:"sourceDatabaseSha256"`
	SourceEnvironmentSHA256 string `json:"sourceEnvironmentSha256"`
	ControllerImage         string `json:"controllerImage"`
	ControllerVersion       string `json:"controllerVersion"`
	ProtocolVersion         string `json:"protocolVersion"`
	StateVolume             string `json:"stateVolume"`
}

func Bootstrap(input Input) error {
	if err := validateInput(input); err != nil {
		return err
	}
	snapshotPath, cleanupSnapshot, err := createConsistentSQLiteSnapshot(input.SourceDatabase)
	if err != nil {
		return fmt.Errorf("snapshot legacy controller database: %w", err)
	}
	defer cleanupSnapshot()
	databaseHash, err := fileHash(snapshotPath)
	if err != nil {
		return fmt.Errorf("hash legacy controller database: %w", err)
	}
	environmentHash, err := fileHash(input.SourceEnvironment)
	if err != nil {
		return fmt.Errorf("hash legacy production environment: %w", err)
	}
	marker := bootstrapMarker{
		SourceDatabaseSHA256: databaseHash, SourceEnvironmentSHA256: environmentHash,
		ControllerImage: input.ControllerImage, ControllerVersion: input.ControllerVersion,
		ProtocolVersion: input.ProtocolVersion, StateVolume: input.StateVolume,
	}
	markerPath := filepath.Join(input.StateRoot, "bootstrap", "completed.json")
	attemptPath := filepath.Join(input.StateRoot, "bootstrap", "in-progress.json")
	idempotent, err := checkExistingMarker(markerPath, marker)
	if err != nil {
		return err
	}
	if idempotent {
		return ValidateStateReadOnly(input.StateRoot)
	}
	resuming, err := checkExistingMarker(attemptPath, marker)
	if err != nil {
		return err
	}

	operations, err := readLegacyOperations(snapshotPath)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		status := opsprotocol.OperationStatus(operation.Status)
		if !opsprotocol.IsTerminalStatus(status) || !operation.CompletedAt.Valid {
			return fmt.Errorf("%w: %s has status %s", ErrActiveLegacyOperation, operation.ID, operation.Status)
		}
	}

	sourceEnvironment, err := opsconfig.ReadFile(input.SourceEnvironment)
	if err != nil {
		return err
	}
	productionBytes, controlBytes, err := canonicalConfigurations(sourceEnvironment, input)
	if err != nil {
		return err
	}
	databaseDestination := filepath.Join(input.StateRoot, "controller.db")
	sourceDatabaseAbsolute, err := filepath.Abs(input.SourceDatabase)
	if err != nil {
		return err
	}
	databaseDestinationAbsolute, err := filepath.Abs(databaseDestination)
	if err != nil {
		return err
	}
	copyDatabaseIntoState := filepath.Clean(sourceDatabaseAbsolute) != filepath.Clean(databaseDestinationAbsolute)
	if err := os.MkdirAll(filepath.Join(input.StateRoot, "bootstrap"), 0o700); err != nil {
		return err
	}
	encodedMarker, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	if !resuming {
		if err := ensureBootstrapDestinationsAbsent(input.StateRoot, copyDatabaseIntoState); err != nil {
			return err
		}
		if err := writeAtomic(attemptPath, append(encodedMarker, '\n'), 0o600); err != nil {
			return err
		}
	}
	backupPath := filepath.Join(input.StateRoot, "bootstrap", "legacy-controller.db")
	if err := verifiedCopy(snapshotPath, backupPath, databaseHash); err != nil {
		return err
	}
	if copyDatabaseIntoState {
		if err := verifiedCopy(snapshotPath, databaseDestination, databaseHash); err != nil {
			return err
		}
	}

	journal, err := opsstate.NewJournal(input.StateRoot)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if err := importLegacyOperation(journal, operation); err != nil {
			return err
		}
	}
	configDirectory := filepath.Join(input.StateRoot, "config")
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		return err
	}
	productionPath := filepath.Join(configDirectory, "production.env")
	controlPath := filepath.Join(configDirectory, "control.env")
	if err := writeAtomic(productionPath, productionBytes, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(controlPath, controlBytes, 0o600); err != nil {
		return err
	}
	if err := opsconfig.ValidateFiles(productionPath, controlPath); err != nil {
		return err
	}
	if err := writeAtomic(markerPath, append(encodedMarker, '\n'), 0o600); err != nil {
		return err
	}
	return ValidateStateReadOnly(input.StateRoot)
}

func createConsistentSQLiteSnapshot(sourcePath string) (string, func(), error) {
	temporary, err := os.CreateTemp("", "hmaigc-controller-snapshot-*.db")
	if err != nil {
		return "", func() {}, err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		cleanup()
		return "", func() {}, err
	}

	source, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(sourcePath)+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer source.Close()
	destination, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(temporaryPath)+"?mode=rwc&_busy_timeout=5000")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer destination.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sourceConnection, err := source.Conn(ctx)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer sourceConnection.Close()
	destinationConnection, err := destination.Conn(ctx)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer destinationConnection.Close()

	err = sourceConnection.Raw(func(sourceDriver interface{}) error {
		sourceSQLite, ok := sourceDriver.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("legacy controller database is not SQLite")
		}
		return destinationConnection.Raw(func(destinationDriver interface{}) error {
			destinationSQLite, ok := destinationDriver.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("bootstrap destination database is not SQLite")
			}
			backup, err := destinationSQLite.Backup("main", sourceSQLite, "main")
			if err != nil {
				return err
			}
			for {
				done, stepErr := backup.Step(128)
				if stepErr != nil {
					_ = backup.Close()
					return stepErr
				}
				if done {
					return backup.Finish()
				}
				select {
				case <-ctx.Done():
					_ = backup.Close()
					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	})
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := destinationConnection.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := destination.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return temporaryPath, cleanup, nil
}

func ValidateStateReadOnly(stateRoot string) error {
	if strings.TrimSpace(stateRoot) == "" {
		return errors.New("ops-state root is required")
	}
	info, err := os.Stat(stateRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("ops-state root is not a directory")
	}
	markerPath := filepath.Join(stateRoot, "bootstrap", "completed.json")
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read bootstrap completion marker: %w", err)
	}
	var marker bootstrapMarker
	decoder := json.NewDecoder(strings.NewReader(string(markerData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil || marker.SourceDatabaseSHA256 == "" || marker.ControllerImage == "" {
		return errors.New("bootstrap completion marker is invalid")
	}
	return validateStoreSchemaReadOnly(filepath.Join(stateRoot, "controller.db"))
}

func validateStoreSchemaReadOnly(path string) error {
	database, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer database.Close()
	rows, err := database.Query("PRAGMA table_info(operation_records)")
	if err != nil {
		return err
	}
	defer rows.Close()
	required := map[string]bool{"id": false, "action": false, "status": false, "idempotency_key": false}
	for rows.Next() {
		var sequence int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&sequence, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if _, exists := required[name]; exists {
			required[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for column, present := range required {
		if !present {
			return fmt.Errorf("controller store is missing required column %s", column)
		}
	}
	return nil
}

func validateInput(input Input) error {
	for name, value := range map[string]string{
		"source database": input.SourceDatabase, "source environment": input.SourceEnvironment,
		"state root": input.StateRoot, "controller image": input.ControllerImage,
		"controller version": input.ControllerVersion, "protocol version": input.ProtocolVersion,
		"state volume": input.StateVolume,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if input.ProtocolVersion != "1" {
		return errors.New("unsupported bootstrap protocol version")
	}
	return nil
}

func readLegacyOperations(path string) ([]legacyOperation, error) {
	database, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	defer database.Close()
	rows, err := database.Query(`SELECT id, action, target_version, current_version_at_start,
		result_version, status, phase, error, exit_code, actor_user_id, actor_display_name,
		idempotency_key, created_at, started_at, completed_at, updated_at
		FROM operation_records ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("read legacy operations: %w", err)
	}
	defer rows.Close()
	operations := make([]legacyOperation, 0)
	for rows.Next() {
		var operation legacyOperation
		if err := rows.Scan(
			&operation.ID, &operation.Action, &operation.TargetVersion, &operation.CurrentVersion,
			&operation.ResultVersion, &operation.Status, &operation.Phase, &operation.Error,
			&operation.ExitCode, &operation.ActorUserID, &operation.ActorDisplayName,
			&operation.IdempotencyKey, &operation.CreatedAt, &operation.StartedAt,
			&operation.CompletedAt, &operation.UpdatedAt,
		); err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func importLegacyOperation(journal *opsstate.Journal, operation legacyOperation) error {
	action := opsprotocol.Action(operation.Action)
	request := opsprotocol.StartOperationRequest{
		Action: action, TargetVersion: operation.TargetVersion,
		ActorUserID: operation.ActorUserID, ActorDisplayName: operation.ActorDisplayName,
		IdempotencyKey: operation.IdempotencyKey, Confirmation: confirmationFor(action, operation.TargetVersion),
	}
	requestHash, err := hashRequest(request)
	if err != nil {
		return err
	}
	runnerSource := opsprotocol.RunnerSourceCurrent
	if action == opsprotocol.ActionInstall || action == opsprotocol.ActionUpgrade {
		runnerSource = opsprotocol.RunnerSourceTarget
	}
	expectedVersion, previousVersion, err := importedVersionContract(operation)
	if err != nil {
		return err
	}
	requestFile := opsprotocol.OperationRequestFile{
		OperationID: operation.ID, Request: request, RequestHash: requestHash,
		CurrentVersion: operation.CurrentVersion, PreviousVersion: previousVersion,
		ExpectedVersion: expectedVersion, RunnerSource: runnerSource,
		ControllerVersionAtStart: "legacy", ImportedTerminal: true,
		CreatedAt: operation.CreatedAt.UTC(),
	}
	if err := journal.CreateRequest(requestFile); err != nil {
		return fmt.Errorf("import legacy request %s: %w", operation.ID, err)
	}
	result := opsprotocol.OperationResult{
		OperationID: operation.ID, Status: opsprotocol.OperationStatus(operation.Status),
		ResultVersion: operation.ResultVersion, ServiceState: opsprotocol.ServiceUnknown,
		ControllerHandoff: opsprotocol.ControllerHandoffUnchanged,
		Error:             operation.Error, CompletedAt: operation.CompletedAt.Time.UTC(),
	}
	if operation.ExitCode.Valid {
		exitCode := int(operation.ExitCode.Int64)
		result.ExitCode = &exitCode
	}
	if result.Status == opsprotocol.OperationSucceeded {
		result.ServiceState = opsprotocol.ServiceTargetOnline
	}
	if err := journal.WriteResult(operation.ID, result); err != nil {
		return fmt.Errorf("import legacy result %s: %w", operation.ID, err)
	}
	return nil
}

func importedVersionContract(operation legacyOperation) (string, string, error) {
	expected := ""
	previous := ""
	switch opsprotocol.Action(operation.Action) {
	case opsprotocol.ActionInstall, opsprotocol.ActionUpgrade:
		expected = operation.TargetVersion
	case opsprotocol.ActionRollback:
		expected = operation.ResultVersion
		previous = expected
	case opsprotocol.ActionBackup, opsprotocol.ActionVerify:
		expected = operation.CurrentVersion
	default:
		return "", "", fmt.Errorf("legacy operation %s has unsupported action %s", operation.ID, operation.Action)
	}
	if err := opsprotocol.ValidateReleaseVersion(expected); err != nil {
		return "", "", fmt.Errorf("legacy operation %s has no valid expected version: %w", operation.ID, err)
	}
	return expected, previous, nil
}

func confirmationFor(action opsprotocol.Action, target string) string {
	switch action {
	case opsprotocol.ActionInstall:
		return "INSTALL " + target
	case opsprotocol.ActionUpgrade:
		return "UPGRADE " + target
	case opsprotocol.ActionRollback:
		return "ROLLBACK"
	case opsprotocol.ActionBackup:
		return "BACKUP"
	case opsprotocol.ActionVerify:
		return "VERIFY"
	default:
		return ""
	}
}

func hashRequest(request opsprotocol.StartOperationRequest) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func canonicalConfigurations(source opsconfig.Environment, input Input) ([]byte, []byte, error) {
	productionValues := make(map[string]string, len(source.Values))
	for key, value := range source.Values {
		productionValues[key] = value
	}
	for _, key := range []string{
		"HMAIGC_OPS_IMAGE", "HMAIGC_OPS_VERSION", "HMAIGC_OPS_PROTOCOL_VERSION",
		"HMAIGC_OPS_COMPOSE_PROJECT_NAME", "HMAIGC_BACKEND_GID", "HMAIGC_RELEASES_API_URL",
		"HMAIGC_RELEASES_API_TOKEN", "HMAIGC_OPS_STATE_DIR", "HMAIGC_STATE_DIR", "HMAIGC_BACKUP_DIR",
	} {
		delete(productionValues, key)
	}
	productionValues["HMAIGC_OPS_STATE_VOLUME"] = input.StateVolume
	production := opsconfig.Environment{Values: productionValues}
	if err := opsconfig.ValidateProduction(production); err != nil {
		return nil, nil, err
	}
	controlValues := map[string]string{
		"HMAIGC_OPS_IMAGE": input.ControllerImage, "HMAIGC_OPS_VERSION": input.ControllerVersion,
		"HMAIGC_OPS_PROTOCOL_VERSION": input.ProtocolVersion, "HMAIGC_OPS_STATE_VOLUME": input.StateVolume,
		"HMAIGC_OPS_COMPOSE_PROJECT_NAME": valueOr(source.Values, "HMAIGC_OPS_COMPOSE_PROJECT_NAME", "hmaigc-ops"),
		"HMAIGC_BACKEND_GID":              valueOr(source.Values, "HMAIGC_BACKEND_GID", "101"),
		"HMAIGC_IMAGE_REGISTRY":           source.Values["HMAIGC_IMAGE_REGISTRY"],
		"HMAIGC_RELEASES_API_URL":         source.Values["HMAIGC_RELEASES_API_URL"],
		"HMAIGC_RELEASES_API_TOKEN":       source.Values["HMAIGC_RELEASES_API_TOKEN"],
		"CANVAS_ENVIRONMENT":              "production",
	}
	control := opsconfig.Environment{Values: controlValues}
	if err := opsconfig.ValidateControl(control); err != nil {
		return nil, nil, err
	}
	return encodeEnvironment(productionValues), encodeEnvironment(controlValues), nil
}

func valueOr(values map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(values[key]); value != "" {
		return value
	}
	return fallback
}

func encodeEnvironment(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(values[key])
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func checkExistingMarker(path string, expected bootstrapMarker) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var actual bootstrapMarker
	if err := json.Unmarshal(data, &actual); err != nil {
		return false, err
	}
	if actual != expected {
		return false, ErrBootstrapSourceMismatch
	}
	return true, nil
}

func ensureBootstrapDestinationsAbsent(root string, includeControllerDatabase bool) error {
	paths := []string{
		filepath.Join(root, "config", "production.env"), filepath.Join(root, "config", "control.env"),
		filepath.Join(root, "bootstrap", "legacy-controller.db"),
	}
	if includeControllerDatabase {
		paths = append(paths, filepath.Join(root, "controller.db"))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: destination already exists without a completed marker: %s", ErrBootstrapSourceMismatch, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func verifiedCopy(source, destination, expectedHash string) error {
	if _, err := os.Stat(destination); err == nil {
		actualHash, hashErr := fileHash(destination)
		if hashErr != nil {
			return hashErr
		}
		if actualHash != expectedHash {
			return fmt.Errorf("%w: existing bootstrap file checksum mismatch: %s", ErrBootstrapSourceMismatch, destination)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	actualHash, err := fileHash(destination)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return errors.New("legacy controller database backup checksum mismatch")
	}
	return nil
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("%w: existing bootstrap file content mismatch: %s", ErrBootstrapSourceMismatch, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".hmaigc-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
