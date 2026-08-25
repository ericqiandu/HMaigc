package opsstate

import (
	"fmt"
	"path/filepath"
)

const (
	operationsDirectory = "operations"
	commandsDirectory   = "commands"
	eventsDirectory     = "events"
)

// Layout owns every durable path used by the operations journal.
type Layout struct {
	root string
}

func newLayout(root string) (*Layout, error) {
	if root == "" {
		return nil, fmt.Errorf("operations state root is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve operations state root: %w", err)
	}
	return &Layout{root: filepath.Clean(absolute)}, nil
}

func (l *Layout) Root() string {
	return l.root
}

func (l *Layout) OperationDir(operationID string) (string, error) {
	if err := validateOperationID(operationID); err != nil {
		return "", err
	}
	return filepath.Join(l.root, operationsDirectory, operationID), nil
}

func (l *Layout) RequestPath(operationID string) (string, error) {
	return l.operationFile(operationID, "request.json")
}

func (l *Layout) LaunchCommandPath(operationID string) (string, error) {
	return l.operationFile(operationID, commandsDirectory, "launch.json")
}

func (l *Layout) CancelCommandPath(operationID string) (string, error) {
	return l.operationFile(operationID, commandsDirectory, "cancel.json")
}

func (l *Layout) RecoverCommandPath(operationID string) (string, error) {
	return l.operationFile(operationID, commandsDirectory, "recover.json")
}

func (l *Layout) LeasePath(operationID string) (string, error) {
	return l.operationFile(operationID, "lease.json")
}

func (l *Layout) HeartbeatPath(operationID string) (string, error) {
	return l.operationFile(operationID, "heartbeat.json")
}

func (l *Layout) CheckpointPath(operationID string) (string, error) {
	return l.operationFile(operationID, "checkpoint.json")
}

func (l *Layout) ResultPath(operationID string) (string, error) {
	return l.operationFile(operationID, "result.json")
}

func (l *Layout) EventsDir(operationID string) (string, error) {
	return l.operationFile(operationID, eventsDirectory)
}

func (l *Layout) EventPath(operationID string, generation uint64, sequence uint64) (string, error) {
	if generation == 0 || sequence == 0 {
		return "", fmt.Errorf("event generation and sequence must be positive")
	}
	return l.operationFile(
		operationID,
		eventsDirectory,
		fmt.Sprintf("%020d-%020d.json", generation, sequence),
	)
}

func (l *Layout) operationFile(operationID string, elements ...string) (string, error) {
	directory, err := l.OperationDir(operationID)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{directory}, elements...)...), nil
}

func validateOperationID(operationID string) error {
	if len(operationID) == 0 || len(operationID) > 128 {
		return fmt.Errorf("%w: length must be between 1 and 128", ErrInvalidOperationID)
	}
	for _, character := range operationID {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' {
			continue
		}
		return fmt.Errorf("%w: invalid character %q", ErrInvalidOperationID, character)
	}
	return nil
}
