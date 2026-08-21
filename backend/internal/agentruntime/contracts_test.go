package agentruntime_test

import (
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func validPersonalScope() agentruntime.Scope {
	return agentruntime.Scope{
		TenantKind:  agentruntime.TenantPersonal,
		TenantID:    "user-1",
		ActorUserID: "user-1",
		CanvasID:    "canvas-1",
		ThreadID:    "thread-1",
		RunID:       "run-1",
		Access: agentruntime.AccessGrant{
			Level:              agentruntime.AccessManager,
			SubscriptionActive: true,
		},
	}
}

func TestScopeValidationAndMutationAuthority(t *testing.T) {
	scope := validPersonalScope()
	if err := scope.Validate(); err != nil {
		t.Fatalf("valid personal scope rejected: %v", err)
	}
	if !scope.CanMutateCanvas() {
		t.Fatal("personal canvas manager must be able to mutate")
	}

	teamEditor := scope
	teamEditor.TenantKind = agentruntime.TenantTeam
	teamEditor.TenantID = "team-1"
	teamEditor.Access.Level = agentruntime.AccessEditor
	if err := teamEditor.Validate(); err != nil {
		t.Fatalf("valid team scope rejected: %v", err)
	}
	if !teamEditor.CanMutateCanvas() {
		t.Fatal("active team editor must be able to mutate")
	}

	teamEditor.Access.SubscriptionActive = false
	if teamEditor.CanMutateCanvas() {
		t.Fatal("team editor without an active subscription must be read-only")
	}

	teamEditor.Access.Level = agentruntime.AccessViewer
	teamEditor.Access.SubscriptionActive = true
	if teamEditor.CanMutateCanvas() {
		t.Fatal("viewer must be read-only")
	}

	invalidTenant := scope
	invalidTenant.TenantKind = agentruntime.TenantKind("organization")
	if invalidTenant.CanMutateCanvas() {
		t.Fatal("unknown tenant kind must fail closed")
	}
}

func TestScopeValidationRejectsInvalidIdentityAndClosedSets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agentruntime.Scope)
	}{
		{name: "missing actor", mutate: func(scope *agentruntime.Scope) { scope.ActorUserID = "" }},
		{name: "missing canvas", mutate: func(scope *agentruntime.Scope) { scope.CanvasID = "" }},
		{name: "missing thread", mutate: func(scope *agentruntime.Scope) { scope.ThreadID = "" }},
		{name: "missing run", mutate: func(scope *agentruntime.Scope) { scope.RunID = "" }},
		{name: "personal tenant differs from actor", mutate: func(scope *agentruntime.Scope) { scope.TenantID = "user-2" }},
		{name: "unknown tenant kind", mutate: func(scope *agentruntime.Scope) { scope.TenantKind = agentruntime.TenantKind("organization") }},
		{name: "unknown access level", mutate: func(scope *agentruntime.Scope) { scope.Access.Level = agentruntime.AccessLevel("owner") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scope := validPersonalScope()
			test.mutate(&scope)
			if err := scope.Validate(); err == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
}

func TestRuntimeStatusSetsRejectUnknownValues(t *testing.T) {
	for _, status := range []agentruntime.RunStatus{
		agentruntime.RunQueued,
		agentruntime.RunRunning,
		agentruntime.RunWaitingInput,
		agentruntime.RunWaitingApproval,
		agentruntime.RunWaitingTool,
		agentruntime.RunSucceeded,
		agentruntime.RunFailed,
		agentruntime.RunCancelled,
	} {
		if !status.Valid() {
			t.Fatalf("declared run status %q rejected", status)
		}
	}
	if agentruntime.RunStatus("paused").Valid() {
		t.Fatal("unknown run status accepted")
	}

	for _, status := range []agentruntime.ThreadStatus{
		agentruntime.ThreadActive,
		agentruntime.ThreadArchived,
	} {
		if !status.Valid() {
			t.Fatalf("declared thread status %q rejected", status)
		}
	}
	if agentruntime.ThreadStatus("deleted").Valid() {
		t.Fatal("unknown thread status accepted")
	}

	for _, status := range []agentruntime.ToolCallStatus{
		agentruntime.ToolCallPending,
		agentruntime.ToolCallWaitingApproval,
		agentruntime.ToolCallRunning,
		agentruntime.ToolCallSucceeded,
		agentruntime.ToolCallFailed,
	} {
		if !status.Valid() {
			t.Fatalf("declared tool call status %q rejected", status)
		}
	}
	if agentruntime.ToolCallStatus("skipped").Valid() {
		t.Fatal("unknown tool call status accepted")
	}
}

func TestEventKindSetRejectsUnknownValues(t *testing.T) {
	for _, kind := range []agentruntime.EventKind{
		agentruntime.EventRunCreated,
		agentruntime.EventRunStatusChanged,
		agentruntime.EventModelDelta,
		agentruntime.EventModelRejected,
		agentruntime.EventClarificationRequested,
		agentruntime.EventClarificationAnswerSaved,
		agentruntime.EventClarificationResponded,
		agentruntime.EventToolCall,
		agentruntime.EventApprovalRequired,
		agentruntime.EventApprovalDecided,
		agentruntime.EventToolStarted,
		agentruntime.EventToolResult,
		agentruntime.EventCheckpointSaved,
		agentruntime.EventRunCompleted,
		agentruntime.EventRunFailed,
	} {
		if !kind.Valid() {
			t.Fatalf("declared event kind %q rejected", kind)
		}
	}
	if agentruntime.EventKind("storyboard.route").Valid() {
		t.Fatal("unknown event kind accepted")
	}
}
