package service

import (
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestTaskProjectScopeRejectsUnsyncedCanvas(t *testing.T) {
	svc, _, _ := newAgentRuntimeServiceFixture(t, "https://example.com")

	if err := svc.ensureTaskProjectActive("runtime-user", "local-only-canvas"); err == nil || !strings.Contains(err.Error(), "尚未同步") {
		t.Fatalf("ensureTaskProjectActive() error = %v, want explicit unsynced canvas error", err)
	}
	if _, err := svc.billingAccountScopeForTask("runtime-user", "local-only-canvas"); err == nil || !strings.Contains(err.Error(), "尚未同步") {
		t.Fatalf("billingAccountScopeForTask() error = %v, want explicit unsynced canvas error", err)
	}
	if _, err := svc.QuoteTaskBilling("runtime-user", TaskBillingQuoteRequest{
		ProjectID: "local-only-canvas", Type: "canvas_image", Operation: "generate", BatchCount: 1,
		Input: TaskBillingQuoteInput{Mode: "image", Config: TaskBillingQuoteConfig{
			ChannelID: "runtime-channel", Model: "runtime-model", Size: "1024x1024", Quality: "low",
		}},
	}); err == nil || !strings.Contains(err.Error(), "尚未同步") {
		t.Fatalf("QuoteTaskBilling() error = %v, want explicit unsynced canvas error", err)
	}
}

func TestBillingAccountScopeForTaskFollowsCanvasOwnership(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")

	personalScope, err := svc.billingAccountScopeForTask("runtime-user", "runtime-canvas")
	if err != nil {
		t.Fatal(err)
	}
	if personalScope.TeamID != "" {
		t.Fatalf("personal canvas billing scope = %#v", personalScope)
	}

	createAgentRuntimeTeamMembership(t, db, "runtime-team", 1_000_000_000)
	now := time.Now().UTC()
	if err := db.Create(&model.CanvasProject{
		ID: "runtime-team-canvas", UserID: "runtime-user", TeamID: "runtime-team", Title: "Team Canvas",
		PayloadJSON: `{"nodes":[],"connections":[]}`, DefaultTeamAccess: model.CanvasAccessManager,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	teamScope, err := svc.billingAccountScopeForTask("runtime-user", "runtime-team-canvas")
	if err != nil {
		t.Fatal(err)
	}
	if teamScope.TeamID != "runtime-team" {
		t.Fatalf("team canvas billing scope = %#v", teamScope)
	}
}
