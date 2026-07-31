package opscontroller

import "errors"

var (
	ErrIdempotencyConflict = errors.New("幂等键已用于不同的运维请求")
	ErrOperationActive     = errors.New("已有升级、回滚、备份或校验任务正在执行")
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
