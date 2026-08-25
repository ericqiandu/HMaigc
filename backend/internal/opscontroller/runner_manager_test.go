package opscontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/opsprotocol"
)

func TestDockerRunnerManagerResolvesTargetTagToImmutableDigest(t *testing.T) {
	digest := immutableTestDigest("resolved-target")
	manager := DockerRunnerManager{
		Registry: "ghcr.io/example", ControllerContainerID: "controller",
		runCommand: func(_ context.Context, arguments ...string) (string, error) {
			switch arguments[0] {
			case "pull":
				return "pulled", nil
			case "image":
				return "[\"" + digest + "\"]", nil
			default:
				t.Fatalf("unexpected docker arguments: %v", arguments)
				return "", nil
			}
		},
	}
	resolved, err := manager.Resolve(context.Background(), opsprotocol.OperationRequestFile{
		Request:         opsprotocol.StartOperationRequest{TargetVersion: ""},
		ExpectedVersion: "v1.0.57",
		RunnerSource:    opsprotocol.RunnerSourceTarget,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Version != "v1.0.57" || resolved.Digest != digest {
		t.Fatalf("unexpected resolved Runner: %+v", resolved)
	}
}

func TestDockerRunnerManagerStartsHardenedRunnerWithoutFencingTokenMetadata(t *testing.T) {
	digest := immutableTestDigest("runner-start")
	var calls [][]string
	manager := DockerRunnerManager{
		runCommand: func(_ context.Context, arguments ...string) (string, error) {
			calls = append(calls, append([]string(nil), arguments...))
			return "", nil
		},
	}
	launch := RunnerLaunch{
		OperationID: "operation-1", Generation: 7,
		ImageDigest: digest, StateVolume: "hmaigc-ops-state",
	}
	if err := manager.Start(context.Background(), launch); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0][0] != "ps" || calls[1][0] != "run" {
		t.Fatalf("unexpected docker call sequence: %v", calls)
	}
	run := calls[1]
	required := []string{
		"--restart=no", "--read-only", "no-new-privileges", "--entrypoint",
		"hmaigc-ops-runner", digest, "--operation-id", "operation-1", "--generation", "7",
	}
	for _, value := range required {
		if !containsArgument(run, value) {
			t.Fatalf("Runner launch is missing %q: %v", value, run)
		}
	}
	for _, argument := range run {
		if strings.Contains(strings.ToLower(argument), "fencing") || strings.Contains(argument, "token") {
			t.Fatalf("secret ownership metadata leaked into Docker arguments: %v", run)
		}
	}
	if len(run) == 0 {
		t.Fatal("expected docker run arguments")
	}
}

func TestDockerRunnerManagerConvergesWhenDockerRunReportsErrorAfterContainerStarted(t *testing.T) {
	digest := immutableTestDigest("ambiguous-runner-start")
	call := 0
	manager := DockerRunnerManager{
		runCommand: func(_ context.Context, arguments ...string) (string, error) {
			call++
			switch call {
			case 1:
				return "", nil
			case 2:
				return "", errors.New("docker client connection reset")
			case 3:
				if arguments[0] != "ps" {
					t.Fatalf("expected post-error Runner lookup, got %v", arguments)
				}
				return "runner-id\n", nil
			case 4:
				if arguments[0] != "inspect" {
					t.Fatalf("expected post-error inspect, got %v", arguments)
				}
				return fmt.Sprintf(`[{"Id":"runner-id","Config":{"Image":%q,"Labels":{"hmaigc.ops.operation-id":"operation-ambiguous","hmaigc.ops.generation":"3","hmaigc.ops.image-digest":%q}},"State":{"Running":true,"ExitCode":0}}]`, digest, digest), nil
			default:
				t.Fatalf("unexpected docker call %d: %v", call, arguments)
				return "", nil
			}
		},
	}
	if err := manager.Start(context.Background(), RunnerLaunch{
		OperationID: "operation-ambiguous", Generation: 3,
		ImageDigest: digest, StateVolume: "hmaigc-ops-state",
	}); err != nil {
		t.Fatalf("matching running container must converge ambiguous docker run outcome: %v", err)
	}
}

func TestDockerRunnerManagerRequiresRecoveryWhenAmbiguousStartCreatedStoppedContainer(t *testing.T) {
	digest := immutableTestDigest("ambiguous-stopped-runner")
	call := 0
	manager := DockerRunnerManager{
		runCommand: func(_ context.Context, arguments ...string) (string, error) {
			call++
			switch call {
			case 1:
				return "", nil
			case 2:
				return "", errors.New("docker client connection reset")
			case 3:
				return "runner-id\n", nil
			case 4:
				return fmt.Sprintf(`[{"Id":"runner-id","Config":{"Image":%q,"Labels":{"hmaigc.ops.operation-id":"operation-stopped","hmaigc.ops.generation":"4","hmaigc.ops.image-digest":%q}},"State":{"Running":false,"ExitCode":2}}]`, digest, digest), nil
			default:
				t.Fatalf("unexpected docker call %d: %v", call, arguments)
				return "", nil
			}
		},
	}
	err := manager.Start(context.Background(), RunnerLaunch{
		OperationID: "operation-stopped", Generation: 4,
		ImageDigest: digest, StateVolume: "hmaigc-ops-state",
	})
	if !errors.Is(err, ErrRunnerStartOutcomeUnknown) {
		t.Fatalf("stopped container after ambiguous start must require recovery, got %v", err)
	}
}

func TestDockerRunnerManagerSurfacesUnknownStartOutcomeWhenPostErrorInspectionFails(t *testing.T) {
	call := 0
	manager := DockerRunnerManager{
		runCommand: func(_ context.Context, arguments ...string) (string, error) {
			call++
			switch call {
			case 1:
				return "", nil
			case 2:
				return "", errors.New("docker client connection reset")
			default:
				return "", errors.New("docker daemon unavailable during inspection")
			}
		},
	}
	err := manager.Start(context.Background(), RunnerLaunch{
		OperationID: "operation-unknown", Generation: 2,
		ImageDigest: immutableTestDigest("unknown-start"), StateVolume: "hmaigc-ops-state",
	})
	if !errors.Is(err, ErrRunnerStartOutcomeUnknown) {
		t.Fatalf("expected explicit ambiguous start outcome, got %v", err)
	}
}

func containsArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}
