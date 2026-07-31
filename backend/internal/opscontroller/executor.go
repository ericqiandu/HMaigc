package opscontroller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"infinite-canvas/backend/internal/opsprotocol"
)

const maxCapturedLogLineBytes = 64 << 10

type ExecutionResult struct {
	ExitCode int
}

type Executor interface {
	Execute(context.Context, opsprotocol.Action, string, func(string, string)) (ExecutionResult, error)
}

type ScriptExecutor struct {
	ScriptPath string
	EnvFile    string
}

func (e ScriptExecutor) Execute(ctx context.Context, action opsprotocol.Action, targetVersion string, appendLog func(string, string)) (ExecutionResult, error) {
	args := []string{string(action)}
	if targetVersion != "" {
		args = append(args, targetVersion)
	}
	command := exec.CommandContext(ctx, e.ScriptPath, args...)
	command.Env = append(command.Environ(), "HMAIGC_ENV_FILE="+e.EnvFile)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return ExecutionResult{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return ExecutionResult{}, err
	}
	if err := command.Start(); err != nil {
		return ExecutionResult{}, err
	}
	var waitGroup sync.WaitGroup
	var scanMu sync.Mutex
	var scanErrors []error
	scan := func(stream string, reader io.Reader) {
		defer waitGroup.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), maxCapturedLogLineBytes)
		for scanner.Scan() {
			appendLog(stream, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			scanMu.Lock()
			scanErrors = append(scanErrors, fmt.Errorf("%s 日志读取失败: %w", stream, err))
			scanMu.Unlock()
		}
	}
	waitGroup.Add(2)
	go scan("stdout", stdout)
	go scan("stderr", stderr)
	waitGroup.Wait()
	waitErr := command.Wait()
	if len(scanErrors) > 0 {
		return ExecutionResult{ExitCode: command.ProcessState.ExitCode()}, errors.Join(scanErrors...)
	}
	exitCode := command.ProcessState.ExitCode()
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) {
			return ExecutionResult{ExitCode: exitCode}, fmt.Errorf("部署脚本退出码 %d", exitCode)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return ExecutionResult{ExitCode: exitCode}, errors.New("运维任务被控制器停止")
		}
		return ExecutionResult{ExitCode: exitCode}, waitErr
	}
	return ExecutionResult{ExitCode: exitCode}, nil
}

func validateExecutorConfig(scriptPath string, envFile string) error {
	if strings.TrimSpace(scriptPath) == "" {
		return errors.New("部署脚本路径未配置")
	}
	if strings.TrimSpace(envFile) == "" {
		return errors.New("生产环境文件路径未配置")
	}
	return nil
}
