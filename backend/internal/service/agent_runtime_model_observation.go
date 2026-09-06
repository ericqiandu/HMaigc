package service

import (
	"context"
	"errors"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
)

type agentRuntimeStateObservationReport struct {
	TotalFailures       int  `json:"totalFailures"`
	ConsecutiveFailures int  `json:"consecutiveFailures"`
	ThresholdExceeded   bool `json:"thresholdExceeded"`
}

type agentRuntimeStateObservationFailurePolicy struct {
	maxConsecutiveFailures int
	totalFailures          int
	consecutiveFailures    int
	thresholdExceeded      bool
}

func newAgentRuntimeStateObservationFailurePolicy(maxConsecutiveFailures int) *agentRuntimeStateObservationFailurePolicy {
	return &agentRuntimeStateObservationFailurePolicy{maxConsecutiveFailures: maxConsecutiveFailures}
}

func (policy *agentRuntimeStateObservationFailurePolicy) Record(observationErr error) bool {
	if policy == nil || policy.maxConsecutiveFailures < 1 {
		return true
	}
	if observationErr == nil {
		policy.consecutiveFailures = 0
		return false
	}
	policy.totalFailures++
	policy.consecutiveFailures++
	policy.thresholdExceeded = policy.consecutiveFailures >= policy.maxConsecutiveFailures
	return policy.thresholdExceeded
}

func (policy *agentRuntimeStateObservationFailurePolicy) Report() agentRuntimeStateObservationReport {
	if policy == nil {
		return agentRuntimeStateObservationReport{ThresholdExceeded: true}
	}
	return agentRuntimeStateObservationReport{
		TotalFailures:       policy.totalFailures,
		ConsecutiveFailures: policy.consecutiveFailures,
		ThresholdExceeded:   policy.thresholdExceeded,
	}
}

func (report agentRuntimeStateObservationReport) Validate() error {
	if report.TotalFailures < 0 || report.ConsecutiveFailures < 0 || report.ConsecutiveFailures > report.TotalFailures ||
		(report.ThresholdExceeded && report.ConsecutiveFailures == 0) {
		return errors.New("Agent 运行状态观察报告无效")
	}
	return nil
}

func loadAgentRuntimeCheckpointWithRetry(
	ctx context.Context,
	attempts int,
	delay time.Duration,
	load func() (agentruntime.RuntimeState, error),
) (agentruntime.RuntimeState, agentRuntimeStateObservationReport, error) {
	if ctx == nil || attempts < 1 || delay < 0 || load == nil {
		return agentruntime.RuntimeState{}, agentRuntimeStateObservationReport{}, errors.New("Agent 运行状态重试配置无效")
	}
	policy := newAgentRuntimeStateObservationFailurePolicy(attempts)
	var state agentruntime.RuntimeState
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		state, err = load()
		if err == nil {
			policy.Record(nil)
			return state, policy.Report(), nil
		}
		if policy.Record(err) || attempt == attempts-1 {
			return agentruntime.RuntimeState{}, policy.Report(), err
		}
		if delay == 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return agentruntime.RuntimeState{}, policy.Report(), context.Cause(ctx)
		case <-timer.C:
		}
	}
	return agentruntime.RuntimeState{}, policy.Report(), err
}
