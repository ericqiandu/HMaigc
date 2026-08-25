//go:build windows

package opsrunner

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

var ErrDeploymentLockHeld = errors.New("another deployment runner owns the deployment lock")

type DeploymentLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func (l *DeploymentLock) RecordOwner(operationID string, generation uint64) error {
	if l == nil || l.file == nil {
		return errors.New("deployment lock is closed")
	}
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate deployment lock owner: %w", err)
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek deployment lock owner: %w", err)
	}
	if _, err := fmt.Fprintf(l.file, "%s\n%s\n", operationID, strconv.FormatUint(generation, 10)); err != nil {
		return fmt.Errorf("write deployment lock owner: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync deployment lock owner: %w", err)
	}
	return nil
}

func AcquireDeploymentLock(path string) (*DeploymentLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open deployment lock: %w", err)
	}
	lock := &DeploymentLock{file: file}
	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&lock.overlapped,
	)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, ErrDeploymentLockHeld
		}
		return nil, fmt.Errorf("acquire deployment lock: %w", err)
	}
	return lock, nil
}

func (l *DeploymentLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}
