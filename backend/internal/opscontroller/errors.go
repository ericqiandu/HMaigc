package opscontroller

import "errors"

var (
	ErrIdempotencyConflict       = errors.New("幂等键已用于不同的运维请求")
	ErrOperationActive           = errors.New("已有升级、回滚、备份或校验任务正在执行")
	ErrCancellationNotAllowed    = errors.New("当前运维任务状态不允许停止")
	ErrRecoveryNotAllowed        = errors.New("当前运维任务没有可安全执行的恢复路径")
	ErrRunnerStartOutcomeUnknown = errors.New("Runner 启动结果无法确认")
)

type RequestError struct {
	message string
}

func (e *RequestError) Error() string {
	return e.message
}

func invalidRequest(message string) error {
	return &RequestError{message: message}
}
