package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestProjectAgentEventProducesVersionedRunAndItemEvents(t *testing.T) {
	now := time.Now().UTC()
	statePayload, err := json.Marshal(agentruntime.RuntimeState{
		StateVersion: 1, MaxSteps: 4, Status: agentruntime.RunQueued,
		UserMessage: "创建短剧", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	})
	if err != nil {
		t.Fatal(err)
	}
	runEvent, err := ProjectAgentEvent("thread-1", model.AgentRunEvent{
		RunID: "run-1", Sequence: 1, Kind: agentruntime.EventRunCreated,
		PayloadJSON: string(statePayload), CreatedAt: now,
	}, nil, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if runEvent.ProtocolVersion != CurrentAgentUIProtocolVersion || runEvent.ThreadID != "thread-1" ||
		runEvent.RunID != "run-1" || runEvent.Sequence != 1 || runEvent.Kind != AgentUIEventRunStarted || runEvent.ItemID != "" {
		t.Fatalf("projected run event = %#v", runEvent)
	}

	item := model.AgentTimelineItem{
		ID: "item-tool-1", TenantKind: agentruntime.TenantPersonal, TenantID: "user-1",
		ThreadID: "thread-1", RunID: "run-1", Kind: model.AgentTimelineItemToolCall,
		Status: model.AgentTimelineItemInProgress, Ordinal: 2, SourceEventSequence: 3,
		ContentJSON: `{"toolCallId":"tool-1","actionVersion":1}`, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	itemEvent, err := ProjectAgentEvent("thread-1", model.AgentRunEvent{
		RunID: "run-1", Sequence: 3, Kind: agentruntime.EventToolCall,
		PayloadJSON: string(statePayload), CreatedAt: now,
	}, &item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if itemEvent.Kind != AgentUIEventItemStarted || itemEvent.ItemID != item.ID ||
		itemEvent.ThreadID != item.ThreadID || string(itemEvent.Payload) != item.ContentJSON {
		t.Fatalf("projected item event = %#v", itemEvent)
	}
	terminalState := agentruntime.RuntimeState{
		StateVersion: 2, StepNumber: 1, MaxSteps: 4, Status: agentruntime.RunSucceeded,
		UserMessage: "创建短剧", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	terminalPayload, err := json.Marshal(terminalState)
	if err != nil {
		t.Fatal(err)
	}
	terminalItem := model.AgentTimelineItem{
		ID: "item-status-1", ThreadID: "thread-1", RunID: "run-1", Kind: model.AgentTimelineItemStatusKind,
		Status: model.AgentTimelineItemCompleted, Ordinal: 3, SourceEventSequence: 5,
		ContentJSON: `{"status":"succeeded"}`, StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	terminalEvent, err := ProjectAgentEvent("thread-1", model.AgentRunEvent{
		RunID: "run-1", Sequence: 5, Kind: agentruntime.EventRunCompleted,
		PayloadJSON: string(terminalPayload), CreatedAt: now,
	}, &terminalItem, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if terminalEvent.Kind != AgentUIEventRunCompleted || terminalEvent.ItemID != terminalItem.ID ||
		!strings.Contains(string(terminalEvent.Payload), `"item":{"kind":"status","status":"completed","content":{"status":"succeeded"}}`) {
		t.Fatalf("projected terminal event = %#v", terminalEvent)
	}
}

func TestProjectAgentEventRejectsUnknownProtocolInvalidFactsAndUnboundItems(t *testing.T) {
	now := time.Now().UTC()
	event := model.AgentRunEvent{
		RunID: "run-1", Sequence: 2, Kind: agentruntime.EventToolCall,
		PayloadJSON: `{}`, CreatedAt: now,
	}
	item := model.AgentTimelineItem{
		ID: "item-1", ThreadID: "thread-1", RunID: event.RunID,
		Kind: model.AgentTimelineItemToolCall, Status: model.AgentTimelineItemInProgress,
		Ordinal: 1, SourceEventSequence: event.Sequence, ContentJSON: `{}`,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	tests := []struct {
		name     string
		threadID string
		event    model.AgentRunEvent
		item     *model.AgentTimelineItem
		protocol int
	}{
		{name: "unknown protocol", threadID: "thread-1", event: event, item: &item, protocol: CurrentAgentUIProtocolVersion + 1},
		{name: "missing item", threadID: "thread-1", event: event, protocol: CurrentAgentUIProtocolVersion},
		{name: "wrong thread", threadID: "thread-other", event: event, item: &item, protocol: CurrentAgentUIProtocolVersion},
		{name: "wrong sequence", threadID: "thread-1", event: event, item: func() *model.AgentTimelineItem { copied := item; copied.SourceEventSequence++; return &copied }(), protocol: CurrentAgentUIProtocolVersion},
		{name: "invalid content", threadID: "thread-1", event: event, item: func() *model.AgentTimelineItem { copied := item; copied.ContentJSON = `{`; return &copied }(), protocol: CurrentAgentUIProtocolVersion},
		{name: "unverified model delta", threadID: "thread-1", event: model.AgentRunEvent{RunID: "run-1", Sequence: 4, Kind: agentruntime.EventModelDelta, PayloadJSON: `{"delta":"internal-json"}`, CreatedAt: now}, protocol: CurrentAgentUIProtocolVersion},
		{name: "terminal event missing item", threadID: "thread-1", event: model.AgentRunEvent{RunID: "run-1", Sequence: 4, Kind: agentruntime.EventRunFailed, PayloadJSON: `{"stateVersion":2,"stepNumber":1,"maxSteps":4,"status":"failed","failureCode":"model_failed","userMessage":"任务","configuration":{"executionMode":"guided"},"clarificationHistory":[]}`, CreatedAt: now}, protocol: CurrentAgentUIProtocolVersion},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ProjectAgentEvent(testCase.threadID, testCase.event, testCase.item, testCase.protocol); err == nil {
				t.Fatal("invalid projection facts were accepted")
			}
		})
	}
}

func TestProjectAgentEventSanitizesArtifactPayload(t *testing.T) {
	now := time.Now().UTC()
	event := model.AgentRunEvent{
		RunID: "run-artifact", Sequence: 8, Kind: agentruntime.EventArtifactAvailable,
		PayloadJSON: `{}`, CreatedAt: now,
	}
	item := model.AgentTimelineItem{
		ID: "item-artifact", TenantKind: agentruntime.TenantPersonal, TenantID: "user-1",
		ThreadID: "thread-artifact", RunID: event.RunID, Kind: model.AgentTimelineItemArtifact,
		Status: model.AgentTimelineItemCompleted, Ordinal: 4, SourceEventSequence: event.Sequence,
		ContentJSON: `{"artifactId":"artifact-1","kind":"video_clip","planKey":"plan-1","planVersion":1,"shotKey":"shot-1","taskId":"task-private","billingOrderId":"bill-private","resourceId":"resource-1","status":"succeeded"}`,
		StartedAt:   now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	projected, err := ProjectAgentEvent(item.ThreadID, event, &item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Kind != AgentUIEventItemCompleted || projected.ItemID != item.ID ||
		!strings.Contains(string(projected.Payload), `"resourceId":"resource-1"`) ||
		strings.Contains(string(projected.Payload), "task-private") || strings.Contains(string(projected.Payload), "bill-private") ||
		strings.Contains(strings.ToLower(string(projected.Payload)), "url") {
		t.Fatalf("projected artifact payload = %s", projected.Payload)
	}

	item.ContentJSON = `{"artifactId":"artifact-1","kind":"video_clip","planKey":"plan-1","planVersion":1,"resourceId":"resource-1","status":"succeeded","signedUrl":"https://oss.example/signed"}`
	if _, err := ProjectAgentEvent(item.ThreadID, event, &item, CurrentAgentUIProtocolVersion); err == nil {
		t.Fatal("artifact timeline containing a signed URL was accepted")
	}
}

func TestAgentTimelineHistoryContentSanitizesArtifactPayload(t *testing.T) {
	item := model.AgentTimelineItem{
		Kind:        model.AgentTimelineItemArtifact,
		ContentJSON: `{"artifactId":"artifact-1","kind":"video_clip","planKey":"plan-1","planVersion":1,"shotKey":"shot-1","taskId":"task-private","billingOrderId":"bill-private","resourceId":"resource-1","status":"succeeded"}`,
	}
	content, err := agentTimelineHistoryContent(item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"resourceId":"resource-1"`) || strings.Contains(string(content), "task-private") ||
		strings.Contains(string(content), "bill-private") || strings.Contains(strings.ToLower(string(content)), "url") {
		t.Fatalf("history artifact payload = %s", content)
	}
	item.ContentJSON = `{"artifactId":"artifact-1","kind":"video_clip","planKey":"plan-1","planVersion":1,"resourceId":"resource-1","status":"succeeded","signedUrl":"https://oss.example/signed"}`
	if _, err := agentTimelineHistoryContent(item); err == nil {
		t.Fatal("history artifact containing a signed URL was accepted")
	}
}

func TestValidateAgentStreamCursorRejectsNegativeAndFutureSequences(t *testing.T) {
	for _, cursor := range []int64{-1, 9} {
		if err := validateAgentStreamCursor(cursor, 8); !errors.Is(err, ErrAgentStreamCursorInvalid) {
			t.Fatalf("cursor %d error = %v", cursor, err)
		}
	}
	for _, cursor := range []int64{0, 8} {
		if err := validateAgentStreamCursor(cursor, 8); err != nil {
			t.Fatalf("valid cursor %d rejected: %v", cursor, err)
		}
	}
}
