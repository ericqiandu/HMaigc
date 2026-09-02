package opsprotocol

import (
	"errors"
	"fmt"
)

var ErrInvalidStatusTransition = errors.New("运维操作状态迁移无效")

var allowedStatusTransitions = map[OperationStatus]map[OperationStatus]struct{}{
	OperationQueued: {
		OperationRunning:    {},
		OperationCancelling: {},
		OperationCancelled:  {},
		OperationFailed:     {},
	},
	OperationRunning: {
		OperationCancelling:       {},
		OperationRecovering:       {},
		OperationSucceeded:        {},
		OperationFailed:           {},
		OperationRecoveryRequired: {},
	},
	OperationCancelling: {
		OperationRecovering:       {},
		OperationCancelled:        {},
		OperationFailed:           {},
		OperationRecoveryRequired: {},
	},
	OperationRecovering: {
		OperationSucceeded:        {},
		OperationFailed:           {},
		OperationCancelled:        {},
		OperationRecoveryRequired: {},
	},
	OperationRecoveryRequired: {
		OperationRecovering: {},
	},
}

func ValidateStatusTransition(from OperationStatus, to OperationStatus) error {
	next, exists := allowedStatusTransitions[from]
	if !exists {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, from, to)
	}
	if _, exists := next[to]; !exists {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, from, to)
	}
	return nil
}

func IsTerminalStatus(status OperationStatus) bool {
	switch status {
	case OperationSucceeded, OperationFailed, OperationCancelled:
		return true
	default:
		return false
	}
}
