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

func TestAgentRuntimeViewMarshalsLegacyTerminalStateAsExplicitHistoricalConfiguration(t *testing.T) {
	now := time.Now().UTC()
	view := AgentRuntimeView{
		Run: model.AgentRun{
			ID: "legacy-run", ThreadID: "legacy-thread", ActorUserID: "legacy-user",
			ClientRequestID: "legacy-request", Status: agentruntime.RunFailed, LastEventSequence: 4,
			StateVersion: 3, StepNumber: 2, MaxSteps: 8, ModelRecordID: "legacy-model-record",
			ModelKey: "legacy-model", ToolSchemaVersion: 1, RuntimeVersion: 1, PolicyVersion: 1,
			CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
		},
		State: agentruntime.RuntimeState{
			StateVersion: 3, StepNumber: 2, MaxSteps: 8, Status: agentruntime.RunFailed,
			FailureCode: "legacy_failure", UserMessage: "旧运行请求",
			Configuration: agentruntime.RunConfiguration{
				GenerationModels: agentruntime.GenerationModelSelections{
					Image: &agentruntime.GenerationModelSelection{ChannelID: "legacy-channel", Model: "gpt-image-2"},
				},
				Skills: []agentruntime.SkillSelection{{Dir: "legacy-skill", Name: "旧 Skill", Version: 1}},
			},
		},
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		State struct {
			ClarificationHistory []json.RawMessage `json:"clarificationHistory"`
			Configuration        struct {
				GenerationModels struct {
					Image *agentruntime.GenerationModelSelection `json:"image"`
				} `json:"generationModels"`
				Skills        []json.RawMessage `json:"skills"`
				Attachments   []json.RawMessage `json:"attachments"`
				ExecutionMode string            `json:"executionMode"`
			} `json:"configuration"`
		} `json:"state"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.State.ClarificationHistory == nil || payload.State.Configuration.GenerationModels.Image == nil ||
		payload.State.Configuration.GenerationModels.Image.Model != "gpt-image-2" ||
		payload.State.Configuration.Skills == nil || payload.State.Configuration.Attachments == nil ||
		payload.State.Configuration.ExecutionMode != "historical" {
		t.Fatalf("legacy read-only state = %s", encoded)
	}
	if view.State.Configuration.ExecutionMode != "" || view.State.ClarificationHistory != nil {
		t.Fatal("wire projection mutated immutable checkpoint state")
	}

	view.State.Configuration.ExecutionMode = agentruntime.ExecutionGuided
	encoded, err = json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var guidedPayload struct {
		State struct {
			Configuration struct {
				ExecutionMode string `json:"executionMode"`
			} `json:"configuration"`
		} `json:"state"`
	}
	if err := json.Unmarshal(encoded, &guidedPayload); err != nil {
		t.Fatal(err)
	}
	if guidedPayload.State.Configuration.ExecutionMode != string(agentruntime.ExecutionGuided) {
		t.Fatalf("existing legacy execution mode changed: %s", encoded)
	}
}

func TestProjectAgentItemEventCarriesTimelineKind(t *testing.T) {
	now := time.Now().UTC()
	item := model.AgentTimelineItem{
		ID: "item-user-1", ThreadID: "thread-1", RunID: "run-1",
		Kind: model.AgentTimelineItemUserMessage, Status: model.AgentTimelineItemCompleted,
		Ordinal: 1, SourceEventSequence: 2, ContentJSON: `{"message":"用户输入"}`,
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	projected, err := ProjectAgentEvent(item.ThreadID, model.AgentRunEvent{
		RunID: item.RunID, Sequence: item.SourceEventSequence, Kind: agentruntime.EventUserMessageAdded,
		PayloadJSON: `{}`, CreatedAt: now,
	}, &item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"itemKind":"user_message"`) {
		t.Fatalf("projected item event must carry its timeline kind: %s", encoded)
	}

	deltaItem := model.AgentTimelineItem{
		ID: "item-agent-1", ThreadID: "thread-1", RunID: "run-1",
		Kind: model.AgentTimelineItemAgentMessage, Status: model.AgentTimelineItemInProgress,
		Ordinal: 2, SourceEventSequence: 3, ContentJSON: `{"message":"你"}`,
		StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	delta, err := ProjectAgentEvent(deltaItem.ThreadID, model.AgentRunEvent{
		RunID: deltaItem.RunID, Sequence: deltaItem.SourceEventSequence, Kind: agentruntime.EventModelDelta,
		PayloadJSON: `{"itemId":"item-agent-1","delta":"你","userVisible":true,"started":true}`, CreatedAt: now,
	}, &deltaItem, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if delta.ItemKind != model.AgentTimelineItemAgentMessage {
		t.Fatalf("projected model delta item kind = %q", delta.ItemKind)
	}
}

func TestProjectAgentEventMapsPersistedStageReviewResolution(t *testing.T) {
	now := time.Now().UTC()
	content := agentruntime.StageReviewResolutionContent{
		ContentType: agentruntime.StageReviewContentType, StageID: "stage-script", StageVersion: 3,
		RevisionID: "revision-script-1", Decision: agentruntime.StageReviewApprove, ClientRequestID: "review-1",
		ResultStageVersion: 4, ResultStatus: agentruntime.StageApproved,
		ResultReviewRevisionID: "revision-script-1", ResultUpdatedAt: now,
	}
	payload, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	item := model.AgentTimelineItem{
		ID: "item-stage-review", ThreadID: "thread-1", RunID: "run-1",
		Kind: model.AgentTimelineItemApproval, Status: model.AgentTimelineItemCompleted,
		Ordinal: 4, SourceEventSequence: 8, ContentJSON: string(payload),
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	projected, err := ProjectAgentEvent(item.ThreadID, model.AgentRunEvent{
		RunID: item.RunID, Sequence: item.SourceEventSequence, Kind: agentruntime.EventApprovalDecided,
		PayloadJSON: string(payload), CreatedAt: now,
	}, &item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Kind != AgentUIEventApprovalResolved || projected.ItemID != item.ID ||
		projected.ItemKind != model.AgentTimelineItemApproval || string(projected.Payload) != string(payload) {
		t.Fatalf("projected stage review resolution = %#v", projected)
	}
}

func TestProjectAgentEventPreservesOnlySafeCanvasCommitRefreshFacts(t *testing.T) {
	now := time.Now().UTC()
	item := model.AgentTimelineItem{
		ID: "item-canvas-commit", ThreadID: "thread-1", RunID: "run-1",
		Kind: model.AgentTimelineItemToolCall, Status: model.AgentTimelineItemCompleted,
		Ordinal: 2, SourceEventSequence: 4,
		ContentJSON: `{"toolCallId":"call-1","toolName":"canvas.commit","actionVersion":1,"succeeded":true,"output":{"canvasId":"canvas-1","committedRevision":8}}`,
		StartedAt:   now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	projected, err := ProjectAgentEvent(item.ThreadID, model.AgentRunEvent{
		RunID: item.RunID, Sequence: item.SourceEventSequence, Kind: agentruntime.EventToolResult,
		PayloadJSON: `{}`, CreatedAt: now,
	}, &item, CurrentAgentUIProtocolVersion)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Kind != AgentUIEventItemCompleted || !strings.Contains(string(projected.Payload), `"toolName":"canvas.commit"`) ||
		!strings.Contains(string(projected.Payload), `"output":{"canvasId":"canvas-1","committedRevision":8}`) || strings.Contains(strings.ToLower(string(projected.Payload)), "url") {
		t.Fatalf("projected canvas commit payload = %s", projected.Payload)
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

func TestProjectAgentEventReplaysAssetPublicationLifecycle(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		itemID     string
		status     model.AgentTimelineItemStatus
		content    json.Marshaler
		wantKind   AgentUIEventKind
		wantMarker string
	}{
		{
			name: "published", itemID: "publication-1", status: model.AgentTimelineItemCompleted,
			content: assetPublicationJSON{Content: agentruntime.AssetPublicationContent{
				ContentType: agentruntime.AssetPublicationContentType, PublicationID: "publication-1",
				ArtifactRevisionID: "revision-1", ResourceID: "resource-1", AssetID: "asset-1",
				AssetVersionID: "asset-version-1", ProjectAssetLinkID: "project-link-1",
				RepresentationID: "representation-1", PublicationPurpose: "character-library",
				TargetCategory: "character", TargetBindingKey: "hero",
			}},
			wantKind: AgentUIEventItemCompleted, wantMarker: `"assetId":"asset-1"`,
		},
		{
			name: "failed", itemID: "publication-1-failure", status: model.AgentTimelineItemFailed,
			content: assetPublicationFailureJSON{Content: agentruntime.AssetPublicationFailureContent{
				ContentType: agentruntime.AssetPublicationFailedType, PublicationID: "publication-1",
				ArtifactRevisionID: "revision-1", ErrorCode: "asset_publication_persistence_failed",
			}},
			wantKind: AgentUIEventItemFailed, wantMarker: `"errorCode":"asset_publication_persistence_failed"`,
		},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			content, err := testCase.content.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			sequence := int64(index + 1)
			item := model.AgentTimelineItem{
				ID: testCase.itemID, ThreadID: "thread-publication", RunID: "run-publication",
				Kind: model.AgentTimelineItemArtifact, Status: testCase.status,
				Ordinal: sequence, SourceEventSequence: sequence, ContentJSON: string(content),
				StartedAt: now, CreatedAt: now, UpdatedAt: now,
			}
			projected, err := ProjectAgentEvent(item.ThreadID, model.AgentRunEvent{
				RunID: item.RunID, Sequence: sequence, Kind: agentruntime.EventArtifactAvailable,
				PayloadJSON: string(content), CreatedAt: now,
			}, &item, CurrentAgentUIProtocolVersion)
			if err != nil {
				t.Fatal(err)
			}
			if projected.Kind != testCase.wantKind || projected.ItemID != item.ID ||
				!strings.Contains(string(projected.Payload), testCase.wantMarker) {
				t.Fatalf("projected publication event = %#v", projected)
			}
		})
	}
}

type assetPublicationJSON struct {
	Content agentruntime.AssetPublicationContent
}

func (value assetPublicationJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Content)
}

type assetPublicationFailureJSON struct {
	Content agentruntime.AssetPublicationFailureContent
}

func (value assetPublicationFailureJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.Content)
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
