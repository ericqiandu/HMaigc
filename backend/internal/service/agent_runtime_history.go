package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type AgentThreadHistoryItem struct {
	Thread     model.AgentThread `json:"thread"`
	ActivityAt time.Time         `json:"activityAt"`
	LatestRun  *AgentRuntimeView `json:"latestRun"`
}

type AgentThreadHistoryView struct {
	Items []AgentThreadHistoryItem `json:"items"`
}

func (s *Service) ListAgentThreads(actor *model.User, canvasID string, limit int) (*AgentThreadHistoryView, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" {
		return nil, errors.New("Agent 画布标识不能为空")
	}
	scope, err := s.AuthorizeAgentScope(actor.ID, canvasID, "thread-history-probe", "run-history-probe")
	if err != nil {
		return nil, err
	}
	records, err := s.repo.AgentThreadHistory(scope, limit)
	if err != nil {
		return nil, err
	}
	view := &AgentThreadHistoryView{Items: make([]AgentThreadHistoryItem, 0, len(records))}
	for _, record := range records {
		item := AgentThreadHistoryItem{Thread: record.Thread, ActivityAt: record.ActivityAt}
		if record.Run != nil {
			state, err := decodeAgentRuntimeState(record.StateJSON)
			if err != nil {
				return nil, err
			}
			item.LatestRun, err = agentRuntimeViewFromFacts(*record.Run, state)
			if err != nil {
				return nil, err
			}
		}
		view.Items = append(view.Items, item)
	}
	return view, nil
}

func decodeAgentRuntimeState(stateJSON string) (agentruntime.RuntimeState, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(stateJSON))
	decoder.DisallowUnknownFields()
	var state agentruntime.RuntimeState
	if err := decoder.Decode(&state); err != nil {
		return agentruntime.RuntimeState{}, errors.New("agent checkpoint state is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentruntime.RuntimeState{}, errors.New("agent checkpoint state is invalid")
	}
	return state, nil
}
