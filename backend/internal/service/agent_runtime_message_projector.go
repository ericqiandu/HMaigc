package service

import (
	"errors"
	"time"
)

type appendAgentVisibleMessageProjection func(delta string, message string, started bool) error

type agentVisibleMessageProjector struct {
	flushInterval time.Duration
	append        appendAgentVisibleMessageProjection
	message       string
	pending       string
	lastFlushAt   time.Time
	started       bool
}

func combineAgentRuntimeRequestErrors(current error, next error) error {
	switch {
	case current == nil:
		return next
	case next == nil:
		return current
	default:
		return errors.Join(current, next)
	}
}

func newAgentVisibleMessageProjector(flushInterval time.Duration, appendProjection appendAgentVisibleMessageProjection) *agentVisibleMessageProjector {
	return &agentVisibleMessageProjector{flushInterval: flushInterval, append: appendProjection}
}

func (projector *agentVisibleMessageProjector) Push(delta string, now time.Time) error {
	if projector == nil || projector.flushInterval <= 0 || projector.append == nil || now.IsZero() {
		return errors.New("Agent 可见消息投影器配置无效")
	}
	if delta == "" {
		return nil
	}
	projector.pending += delta
	if !projector.started || now.Sub(projector.lastFlushAt) >= projector.flushInterval {
		return projector.Flush(now)
	}
	return nil
}

func (projector *agentVisibleMessageProjector) Flush(now time.Time) error {
	if projector == nil || projector.flushInterval <= 0 || projector.append == nil || now.IsZero() {
		return errors.New("Agent 可见消息投影器配置无效")
	}
	if projector.pending == "" {
		return nil
	}
	nextMessage := projector.message + projector.pending
	if err := projector.append(projector.pending, nextMessage, !projector.started); err != nil {
		return err
	}
	projector.message = nextMessage
	projector.pending = ""
	projector.lastFlushAt = now
	projector.started = true
	return nil
}

func (projector *agentVisibleMessageProjector) PersistedMessage() string {
	if projector == nil {
		return ""
	}
	return projector.message
}
