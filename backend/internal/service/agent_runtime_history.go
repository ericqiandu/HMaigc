package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type AgentThreadHistoryThreadView struct {
	ID        string                    `json:"id"`
	CanvasID  string                    `json:"canvasId"`
	Status    agentruntime.ThreadStatus `json:"status"`
	CreatedAt time.Time                 `json:"createdAt"`
	UpdatedAt time.Time                 `json:"updatedAt"`
}

type AgentThreadHistoryItem struct {
	Thread     AgentThreadHistoryThreadView `json:"thread"`
	ActivityAt time.Time                    `json:"activityAt"`
	Turns      []AgentThreadHistoryTurnView `json:"turns"`
}

type AgentThreadHistoryRunView struct {
	ID                string                 `json:"id"`
	ThreadID          string                 `json:"threadId"`
	Status            agentruntime.RunStatus `json:"status"`
	LastEventSequence int64                  `json:"lastEventSequence"`
	StateVersion      int                    `json:"stateVersion"`
	StepNumber        int                    `json:"stepNumber"`
	MaxSteps          int                    `json:"maxSteps"`
	ModelKey          string                 `json:"modelKey"`
	ToolSchemaVersion int                    `json:"toolSchemaVersion"`
	RuntimeVersion    int                    `json:"runtimeVersion"`
	PolicyVersion     int                    `json:"policyVersion"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
	CompletedAt       *time.Time             `json:"completedAt,omitempty"`
}

type AgentThreadHistoryTimelineItemView struct {
	ID                  string                        `json:"id"`
	RunID               string                        `json:"runId"`
	Kind                model.AgentTimelineItemKind   `json:"kind"`
	Status              model.AgentTimelineItemStatus `json:"status"`
	Ordinal             int64                         `json:"ordinal"`
	SourceEventSequence int64                         `json:"sourceEventSequence"`
	Content             json.RawMessage               `json:"content"`
	StartedAt           time.Time                     `json:"startedAt"`
	CompletedAt         *time.Time                    `json:"completedAt,omitempty"`
	CreatedAt           time.Time                     `json:"createdAt"`
	UpdatedAt           time.Time                     `json:"updatedAt"`
}

type AgentThreadHistoryTurnView struct {
	Run   AgentThreadHistoryRunView            `json:"run"`
	Items []AgentThreadHistoryTimelineItemView `json:"items"`
}

type AgentThreadHistoryView struct {
	Items                    []AgentThreadHistoryItem `json:"items"`
	CanvasProjectionRevision *int64                   `json:"canvasProjectionRevision,omitempty"`
}

type agentHistoricalArtifactTimelinePayload struct {
	ArtifactID   string                              `json:"artifactId"`
	Kind         model.AgentProductionArtifactKind   `json:"kind"`
	PlanKey      string                              `json:"planKey"`
	PlanVersion  int                                 `json:"planVersion"`
	ReferenceKey string                              `json:"referenceKey,omitempty"`
	ShotKey      string                              `json:"shotKey,omitempty"`
	ResourceID   string                              `json:"resourceId,omitempty"`
	Status       model.AgentProductionArtifactStatus `json:"status"`
}

type agentHistoricalArtifactView struct {
	ArtifactID   string                              `json:"artifactId"`
	Kind         model.AgentProductionArtifactKind   `json:"kind"`
	PlanKey      string                              `json:"planKey"`
	PlanVersion  int                                 `json:"planVersion"`
	ReferenceKey string                              `json:"referenceKey,omitempty"`
	ShotKey      string                              `json:"shotKey,omitempty"`
	ResourceID   string                              `json:"resourceId"`
	Status       model.AgentProductionArtifactStatus `json:"status"`
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
	terminalProjections := make([]agentTerminalCanvasProjection, 0)
	for _, record := range records {
		if record.Thread.CanvasID != scope.CanvasID || record.Thread.TenantKind != scope.TenantKind ||
			record.Thread.TenantID != scope.TenantID || record.Thread.DomainProjectID != scope.DomainProjectID ||
			record.Thread.CreatedByUserID != scope.ActorUserID {
			return nil, errors.New("Agent 历史对话作用域与已授权画布不一致")
		}
		item := AgentThreadHistoryItem{
			Thread: AgentThreadHistoryThreadView{
				ID: record.Thread.ID, CanvasID: record.Thread.CanvasID, Status: record.Thread.Status,
				CreatedAt: agentHistoryUTCTime(record.Thread.CreatedAt), UpdatedAt: agentHistoryUTCTime(record.Thread.UpdatedAt),
			},
			ActivityAt: agentHistoryUTCTime(record.ActivityAt),
			Turns:      make([]AgentThreadHistoryTurnView, 0, len(record.Turns)),
		}
		for _, recordTurn := range record.Turns {
			state, err := decodeAgentRuntimeState(recordTurn.StateJSON)
			if err != nil {
				return nil, err
			}
			if _, err := agentRuntimeViewFromFacts(recordTurn.Run, state); err != nil {
				return nil, err
			}
			if agentRuntimeRunTerminal(state.Status) {
				projectionScope := scope
				projectionScope.ThreadID = record.Thread.ID
				projectionScope.RunID = recordTurn.Run.ID
				terminalProjections = append(terminalProjections, agentTerminalCanvasProjection{scope: projectionScope, state: state})
			}
			turn, err := agentThreadHistoryTurnView(recordTurn.Run, recordTurn.Items)
			if err != nil {
				return nil, err
			}
			item.Turns = append(item.Turns, turn)
		}
		view.Items = append(view.Items, item)
	}
	projection, err := s.projectTerminalAgentRunsCanvas(terminalProjections)
	if err != nil {
		return nil, fmt.Errorf("对账 Agent 历史终态画布节点: %w", err)
	}
	if len(terminalProjections) > 0 {
		view.CanvasProjectionRevision = &projection.revision
	}
	return view, nil
}

func agentThreadHistoryTurnView(run model.AgentRun, items []model.AgentTimelineItem) (AgentThreadHistoryTurnView, error) {
	view := AgentThreadHistoryTurnView{
		Run: AgentThreadHistoryRunView{
			ID: run.ID, ThreadID: run.ThreadID, Status: run.Status, LastEventSequence: run.LastEventSequence,
			StateVersion: run.StateVersion, StepNumber: run.StepNumber, MaxSteps: run.MaxSteps,
			ModelKey: run.ModelKey, ToolSchemaVersion: run.ToolSchemaVersion, RuntimeVersion: run.RuntimeVersion,
			PolicyVersion: run.PolicyVersion, CreatedAt: agentHistoryUTCTime(run.CreatedAt), UpdatedAt: agentHistoryUTCTime(run.UpdatedAt),
			CompletedAt: agentHistoryUTCTimePointer(run.CompletedAt),
		},
		Items: make([]AgentThreadHistoryTimelineItemView, 0, len(items)),
	}
	for _, item := range items {
		content, err := agentTimelineHistoryContent(item)
		if err != nil {
			return AgentThreadHistoryTurnView{}, err
		}
		view.Items = append(view.Items, AgentThreadHistoryTimelineItemView{
			ID: item.ID, RunID: item.RunID, Kind: item.Kind, Status: item.Status, Ordinal: item.Ordinal,
			SourceEventSequence: item.SourceEventSequence, Content: content, StartedAt: agentHistoryUTCTime(item.StartedAt),
			CompletedAt: agentHistoryUTCTimePointer(item.CompletedAt), CreatedAt: agentHistoryUTCTime(item.CreatedAt),
			UpdatedAt: agentHistoryUTCTime(item.UpdatedAt),
		})
	}
	return view, nil
}

func agentHistoryUTCTime(value time.Time) time.Time {
	return value.UTC()
}

func agentHistoryUTCTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func agentTimelineHistoryContent(item model.AgentTimelineItem) (json.RawMessage, error) {
	if !json.Valid([]byte(item.ContentJSON)) {
		return nil, errors.New("agent timeline item content is invalid")
	}
	if item.Kind != model.AgentTimelineItemArtifact {
		return append(json.RawMessage(nil), item.ContentJSON...), nil
	}
	var envelope struct {
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal([]byte(item.ContentJSON), &envelope); err != nil {
		return nil, errors.New("agent artifact timeline facts are invalid")
	}
	switch envelope.ContentType {
	case agentruntime.AssetPublicationContentType:
		if _, err := agentruntime.DecodeAssetPublicationContent([]byte(item.ContentJSON)); err != nil {
			return nil, errors.New("agent asset publication timeline facts are invalid")
		}
		return append(json.RawMessage(nil), item.ContentJSON...), nil
	case agentruntime.AssetPublicationFailedType:
		if _, err := agentruntime.DecodeAssetPublicationFailureContent([]byte(item.ContentJSON)); err != nil {
			return nil, errors.New("agent asset publication failure timeline facts are invalid")
		}
		return append(json.RawMessage(nil), item.ContentJSON...), nil
	case "":
		// Historical production artifacts remain readable for audit, but are never executable.
	default:
		return nil, errors.New("retired agent artifact timeline content is not readable by the current history contract")
	}
	internal, err := decodeHistoricalAgentArtifactTimelinePayload(item.ContentJSON)
	if err != nil || internal.ArtifactID == "" || internal.PlanKey == "" || internal.PlanVersion < 1 ||
		internal.ResourceID == "" || !internal.Status.Valid() {
		return nil, errors.New("agent artifact timeline facts are invalid")
	}
	return json.Marshal(agentHistoricalArtifactView{
		ArtifactID: internal.ArtifactID, Kind: internal.Kind, PlanKey: internal.PlanKey,
		PlanVersion: internal.PlanVersion, ReferenceKey: internal.ReferenceKey, ShotKey: internal.ShotKey,
		ResourceID: internal.ResourceID, Status: internal.Status,
	})
}

func decodeHistoricalAgentArtifactTimelinePayload(raw string) (agentHistoricalArtifactTimelinePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload agentHistoricalArtifactTimelinePayload
	if err := decoder.Decode(&payload); err != nil {
		return agentHistoricalArtifactTimelinePayload{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentHistoricalArtifactTimelinePayload{}, errors.New("agent event payload has trailing data")
	}
	return payload, nil
}

func decodeAgentRuntimeState(stateJSON string) (agentruntime.RuntimeState, error) {
	return agentruntime.DecodeCheckpointState(stateJSON)
}
