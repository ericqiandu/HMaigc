package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpDoesNotCreateOperationalState(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "must-not-exist")
	t.Setenv("HMAIGC_OPS_STATE_DIR", stateRoot)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("help output=%q", stdout.String())
	}
	if _, err := os.Stat(stateRoot); !os.IsNotExist(err) {
		t.Fatalf("help mutated operational state: %v", err)
	}
}

func TestRunnerRequiresExplicitIdentity(t *testing.T) {
	var output bytes.Buffer
	if err := run(nil, &output, &output); err == nil {
		t.Fatal("runner accepted a launch without operation identity")
	}
}
