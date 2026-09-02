package opsrunner

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDeploymentLockRejectsConcurrentRunnerWithoutWaiting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deploy.lock")
	first, err := AcquireDeploymentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := AcquireDeploymentLock(path)
	if !errors.Is(err, ErrDeploymentLockHeld) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("expected immediate lock conflict, got %v", err)
	}
}
