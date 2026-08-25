package opscontroller

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/opsprotocol"
)

const maximumDockerOutput = 1 << 20

type RunnerManager interface {
	Resolve(context.Context, opsprotocol.OperationRequestFile) (ResolvedRunner, error)
	Start(context.Context, RunnerLaunch) error
	Inspect(context.Context, string) (RunnerInstance, error)
	ListByOperation(context.Context, string) ([]RunnerInstance, error)
}

type ResolvedRunner struct {
	Version string
	Digest  string
}

type RunnerLaunch struct {
	OperationID string
	Generation  uint64
	ImageDigest string
	StateVolume string
}

type RunnerInstance struct {
	ContainerID string
	OperationID string
	Generation  uint64
	Digest      string
	Running     bool
	ExitCode    *int
}

type DockerRunnerManager struct {
	Binary                string
	Registry              string
	ControllerContainerID string
	runCommand            func(context.Context, ...string) (string, error)
}

func (m DockerRunnerManager) Resolve(ctx context.Context, request opsprotocol.OperationRequestFile) (ResolvedRunner, error) {
	if strings.TrimSpace(m.Registry) == "" || strings.TrimSpace(m.ControllerContainerID) == "" {
		return ResolvedRunner{}, errors.New("Runner 镜像仓库或当前控制器容器未配置")
	}
	version := request.ControllerVersionAtStart
	imageReference := ""
	switch request.RunnerSource {
	case opsprotocol.RunnerSourceTarget:
		version = request.ExpectedVersion
		imageReference = strings.TrimSuffix(m.Registry, "/") + "/hmaigc-ops-controller:" + version
		if _, err := m.docker(ctx, "pull", imageReference); err != nil {
			return ResolvedRunner{}, fmt.Errorf("拉取目标 Runner 镜像失败: %w", err)
		}
	case opsprotocol.RunnerSourceCurrent:
		output, err := m.docker(ctx, "inspect", "--format", "{{.Config.Image}}", m.ControllerContainerID)
		if err != nil {
			return ResolvedRunner{}, fmt.Errorf("读取当前控制器镜像失败: %w", err)
		}
		imageReference = strings.TrimSpace(output)
	default:
		return ResolvedRunner{}, fmt.Errorf("不支持的 Runner 来源: %s", request.RunnerSource)
	}
	if isImmutableImageDigest(imageReference) {
		return ResolvedRunner{Version: version, Digest: imageReference}, nil
	}
	digest, err := m.resolveRepoDigest(ctx, imageReference)
	if err != nil {
		return ResolvedRunner{}, err
	}
	return ResolvedRunner{Version: version, Digest: digest}, nil
}

func (m DockerRunnerManager) Start(ctx context.Context, launch RunnerLaunch) error {
	if launch.OperationID == "" || launch.Generation == 0 || !isImmutableImageDigest(launch.ImageDigest) || launch.StateVolume == "" {
		return errors.New("Runner 启动参数不完整或镜像不是不可变摘要")
	}
	name := runnerContainerName(launch.OperationID, launch.Generation)
	existingIDs, err := m.docker(ctx, "ps", "--all", "--quiet", "--filter", "name=^/"+name+"$")
	if err != nil {
		return err
	}
	if strings.TrimSpace(existingIDs) != "" {
		existing, err := m.inspectContainer(ctx, name)
		if err != nil {
			return err
		}
		if existing.OperationID == launch.OperationID && existing.Generation == launch.Generation &&
			existing.Digest == launch.ImageDigest && existing.Running {
			return nil
		}
		return fmt.Errorf("Runner 容器名已被不匹配实例占用: %s", name)
	}
	_, err = m.docker(ctx,
		"run", "--detach", "--name", name,
		"--label", "hmaigc.ops.role=runner",
		"--label", "hmaigc.ops.operation-id="+launch.OperationID,
		"--label", "hmaigc.ops.generation="+strconv.FormatUint(launch.Generation, 10),
		"--label", "hmaigc.ops.image-digest="+launch.ImageDigest,
		"--restart=no", "--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--security-opt", "no-new-privileges",
		"--volume", "/var/run/docker.sock:/var/run/docker.sock",
		"--volume", launch.StateVolume+":/var/lib/hmaigc-ops",
		"--entrypoint", "hmaigc-ops-runner",
		launch.ImageDigest,
		"--operation-id", launch.OperationID,
		"--generation", strconv.FormatUint(launch.Generation, 10),
	)
	if err != nil {
		instances, inspectErr := m.ListByOperation(ctx, launch.OperationID)
		if inspectErr != nil {
			return fmt.Errorf("%w: docker run: %v; inspect: %v", ErrRunnerStartOutcomeUnknown, err, inspectErr)
		}
		for _, instance := range instances {
			if instance.OperationID == launch.OperationID && instance.Generation == launch.Generation &&
				instance.Digest == launch.ImageDigest {
				if instance.Running {
					return nil
				}
				return fmt.Errorf("%w: docker run: %v; matching Runner exited before confirmation", ErrRunnerStartOutcomeUnknown, err)
			}
		}
		if len(instances) > 0 {
			return fmt.Errorf("%w: docker run: %v; found %d non-matching Runner instances", ErrRunnerStartOutcomeUnknown, err, len(instances))
		}
		return fmt.Errorf("启动独立 Runner 失败: %w", err)
	}
	return nil
}

func (m DockerRunnerManager) Inspect(ctx context.Context, operationID string) (RunnerInstance, error) {
	instances, err := m.ListByOperation(ctx, operationID)
	if err != nil {
		return RunnerInstance{}, err
	}
	if len(instances) == 0 {
		return RunnerInstance{}, nil
	}
	if len(instances) != 1 {
		return RunnerInstance{}, fmt.Errorf("操作 %s 存在 %d 个 Runner，所有权不唯一", operationID, len(instances))
	}
	return instances[0], nil
}

func (m DockerRunnerManager) ListByOperation(ctx context.Context, operationID string) ([]RunnerInstance, error) {
	output, err := m.docker(ctx, "ps", "--all", "--quiet", "--filter", "label=hmaigc.ops.role=runner", "--filter", "label=hmaigc.ops.operation-id="+operationID)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(output)
	instances := make([]RunnerInstance, 0, len(ids))
	for _, id := range ids {
		instance, err := m.inspectContainer(ctx, id)
		if err != nil {
			return nil, err
		}
		if instance.ContainerID != "" {
			instances = append(instances, instance)
		}
	}
	return instances, nil
}

type dockerInspectRecord struct {
	ID     string `json:"Id"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running  bool `json:"Running"`
		ExitCode int  `json:"ExitCode"`
	} `json:"State"`
}

func (m DockerRunnerManager) inspectContainer(ctx context.Context, id string) (RunnerInstance, error) {
	output, err := m.docker(ctx, "inspect", id)
	if err != nil {
		return RunnerInstance{}, err
	}
	var records []dockerInspectRecord
	if err := json.Unmarshal([]byte(output), &records); err != nil || len(records) != 1 {
		return RunnerInstance{}, errors.New("Docker Runner inspect 返回无效")
	}
	record := records[0]
	generation, err := strconv.ParseUint(record.Config.Labels["hmaigc.ops.generation"], 10, 64)
	if err != nil {
		return RunnerInstance{}, errors.New("Runner generation 标签无效")
	}
	instance := RunnerInstance{
		ContainerID: record.ID,
		OperationID: record.Config.Labels["hmaigc.ops.operation-id"],
		Generation:  generation,
		Digest:      record.Config.Labels["hmaigc.ops.image-digest"],
		Running:     record.State.Running,
	}
	if !record.State.Running {
		exitCode := record.State.ExitCode
		instance.ExitCode = &exitCode
	}
	return instance, nil
}

func (m DockerRunnerManager) resolveRepoDigest(ctx context.Context, reference string) (string, error) {
	output, err := m.docker(ctx, "image", "inspect", "--format", "{{json .RepoDigests}}", reference)
	if err != nil {
		return "", fmt.Errorf("解析 Runner 镜像摘要失败: %w", err)
	}
	var digests []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &digests); err != nil {
		return "", fmt.Errorf("Runner 镜像摘要格式无效: %w", err)
	}
	repository := reference
	if separator := strings.LastIndex(repository, ":"); separator > strings.LastIndex(repository, "/") {
		repository = repository[:separator]
	}
	for _, digest := range digests {
		if strings.HasPrefix(digest, repository+"@sha256:") && isImmutableImageDigest(digest) {
			return digest, nil
		}
	}
	return "", fmt.Errorf("镜像 %s 没有匹配仓库的不可变摘要", reference)
}

func (m DockerRunnerManager) docker(ctx context.Context, arguments ...string) (string, error) {
	if m.runCommand != nil {
		return m.runCommand(ctx, arguments...)
	}
	binary := strings.TrimSpace(m.Binary)
	if binary == "" {
		binary = "docker"
	}
	command := exec.CommandContext(ctx, binary, arguments...)
	var output limitedBuffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	remaining := maximumDockerOutput - b.buffer.Len()
	if remaining <= 0 {
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = b.buffer.Write(value[:remaining])
		return len(value), nil
	}
	return b.buffer.Write(value)
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

func runnerContainerName(operationID string, generation uint64) string {
	return "hmaigc-ops-runner-" + operationID + "-g" + strconv.FormatUint(generation, 10)
}

func isImmutableImageDigest(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	if separator <= 0 {
		return false
	}
	digest := reference[separator+len("@sha256:"):]
	if len(digest) != 64 || digest != strings.ToLower(digest) {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
