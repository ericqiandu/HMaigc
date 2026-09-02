package agentruntime_test

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestCapabilityIdempotencyKeyIsStableAcrossRunsAndScopedToCommercialIntent(t *testing.T) {
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "user-1", ActorUserID: "user-1",
		DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessEditor},
	}
	call := agentruntime.ToolCallDecision{
		ToolCallID: "call-1", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-1","modelKey":"gpt-image-2","parameters":{"prompt":"red cube"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"stable-request"}`),
	}
	want, err := agentruntime.CapabilityIdempotencyKey(scope, call)
	if err != nil {
		t.Fatal(err)
	}
	otherRun := scope
	otherRun.RunID = "run-2"
	got, err := agentruntime.CapabilityIdempotencyKey(otherRun, call)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cross-run key = %q, want %q", got, want)
	}
	otherCanvas := scope
	otherCanvas.CanvasID = "canvas-2"
	got, err = agentruntime.CapabilityIdempotencyKey(otherCanvas, call)
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Fatal("capability key crossed the canvas boundary")
	}
	changedCall := call
	changedCall.Arguments = json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-1","modelKey":"gpt-image-2","parameters":{"prompt":"red cube"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"different-request"}`)
	got, err = agentruntime.CapabilityIdempotencyKey(scope, changedCall)
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Fatal("capability key ignored the client request identity")
	}
	changedArguments := call
	changedArguments.Arguments = json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-1","modelKey":"gpt-image-2","parameters":{"prompt":"blue sphere"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"stable-request"}`)
	got, err = agentruntime.CapabilityIdempotencyKey(scope, changedArguments)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("commercial identity changed with mutable arguments: got %q, want %q", got, want)
	}
}

func TestReplayResolvedToolAdvancesWithoutASecondApproval(t *testing.T) {
	current := agentruntime.RuntimeState{
		StateVersion: 1, MaxSteps: 8, Status: agentruntime.RunQueued, UserMessage: "generate image",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided},
	}
	call := agentruntime.ToolCallDecision{
		ToolCallID: "replay-call", ToolName: agentruntime.ToolMediaGenerate, ActionVersion: 1,
		Arguments: json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-1","modelKey":"gpt-image-2","parameters":{"prompt":"red cube"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"stable-request"}`),
		ExpectedDelivery: agentruntime.ExpectedDelivery{
			Kind: agentruntime.DeliveryGeneratedAsset, RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
			CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactTaskBackedResource, Artifact: agentruntime.ArtifactImage}},
		},
	}
	pending, err := agentruntime.Advance(current, agentruntime.RuntimeInput{
		Decision: agentruntime.ModelDecision{Kind: agentruntime.DecisionToolCall, ToolCall: &call},
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := agentruntime.ReplayResolvedTool(current, pending, agentruntime.ToolReplay{
		Call: call, SourceRunID: "source-run", SourceToolCallID: "source-call", SourceActionVersion: 1,
		Result: agentruntime.ToolResolution{
			ToolCallID: call.ToolCallID, ActionVersion: call.ActionVersion, Succeeded: true,
			Output: json.RawMessage(`{"taskId":"task-1","resources":[{"id":"resource-1","url":"https://example.com/image.png"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.State.Status != agentruntime.RunRunning || replayed.State.PendingToolCall != nil ||
		replayed.State.LastToolResult == nil || !replayed.State.LastToolResult.Succeeded ||
		replayed.ToolReplay == nil {
		t.Fatalf("replayed state = %#v", replayed)
	}
	wantEvents := []agentruntime.EventKind{agentruntime.EventToolCall, agentruntime.EventToolResult, agentruntime.EventRunStatusChanged}
	if len(replayed.EventKinds) != len(wantEvents) {
		t.Fatalf("replayed events = %#v", replayed.EventKinds)
	}
	for index, want := range wantEvents {
		if replayed.EventKinds[index] != want {
			t.Fatalf("replayed event %d = %q, want %q", index, replayed.EventKinds[index], want)
		}
	}
}

func TestVisionCapabilityIdempotencyKeyCoversCompleteOrderedRunFacts(t *testing.T) {
	t.Parallel()

	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantTeam, TenantID: "team-1", ActorUserID: "user-1",
		DomainProjectID: "project-1", CanvasID: "canvas-1", ThreadID: "thread-1", RunID: "run-1",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessEditor, SubscriptionActive: true},
	}
	base := agentruntime.ToolCallDecision{
		ToolCallID: "vision-call-1", ToolName: agentruntime.ToolVisionAnalyze, ActionVersion: 1,
		Arguments: json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述人物关系","detail":"low","clientRequestId":"vision-request-1"}`),
	}
	want, err := agentruntime.CapabilityIdempotencyKey(scope, base)
	if err != nil || want == "" {
		t.Fatalf("base vision key = %q, err=%v", want, err)
	}
	sameFacts := base
	sameFacts.ToolCallID = "another-tool-call"
	got, err := agentruntime.CapabilityIdempotencyKey(scope, sameFacts)
	if err != nil || got != want {
		t.Fatalf("tool-call-local identity changed complete vision key: got=%q want=%q err=%v", got, want, err)
	}

	tests := []struct {
		name   string
		mutate func(*agentruntime.Scope, *agentruntime.ToolCallDecision)
	}{
		{name: "run", mutate: func(value *agentruntime.Scope, _ *agentruntime.ToolCallDecision) { value.RunID = "run-2" }},
		{name: "tenant", mutate: func(value *agentruntime.Scope, _ *agentruntime.ToolCallDecision) { value.TenantID = "team-2" }},
		{name: "project", mutate: func(value *agentruntime.Scope, _ *agentruntime.ToolCallDecision) { value.DomainProjectID = "project-2" }},
		{name: "canvas", mutate: func(value *agentruntime.Scope, _ *agentruntime.ToolCallDecision) { value.CanvasID = "canvas-2" }},
		{name: "resource order", mutate: func(_ *agentruntime.Scope, value *agentruntime.ToolCallDecision) {
			value.Arguments = json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-2","resource-1"],"prompt":"描述人物关系","detail":"low","clientRequestId":"vision-request-1"}`)
		}},
		{name: "model", mutate: func(_ *agentruntime.Scope, value *agentruntime.ToolCallDecision) {
			value.Arguments = json.RawMessage(`{"modelRecordId":"vision-record-2","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述人物关系","detail":"low","clientRequestId":"vision-request-1"}`)
		}},
		{name: "prompt", mutate: func(_ *agentruntime.Scope, value *agentruntime.ToolCallDecision) {
			value.Arguments = json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述场景光线","detail":"low","clientRequestId":"vision-request-1"}`)
		}},
		{name: "detail", mutate: func(_ *agentruntime.Scope, value *agentruntime.ToolCallDecision) {
			value.Arguments = json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述人物关系","detail":"original","clientRequestId":"vision-request-1"}`)
		}},
		{name: "client request", mutate: func(_ *agentruntime.Scope, value *agentruntime.ToolCallDecision) {
			value.Arguments = json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"描述人物关系","detail":"low","clientRequestId":"vision-request-2"}`)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			changedScope := scope
			changedCall := base
			testCase.mutate(&changedScope, &changedCall)
			key, keyErr := agentruntime.CapabilityIdempotencyKey(changedScope, changedCall)
			if keyErr != nil {
				t.Fatal(keyErr)
			}
			if key == want {
				t.Fatalf("%s was omitted from vision capability identity", testCase.name)
			}
		})
	}
}
