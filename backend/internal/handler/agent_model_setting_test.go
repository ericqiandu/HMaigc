package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestAgentDefaultModelRoutesRequireAdminAndPublishValidatedReference(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{ID: "agent-text", ChannelID: channel.ID, ModelKey: "custom-agent", DisplayName: "Custom Agent", AccessPolicy: model.ModelAccessAuthenticated, Capability: "text", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 100, PriceConfigured: true, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := fixture.db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	if response := fixture.request(http.MethodGet, "/api/admin/settings/agent-model", "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPut, "/api/admin/settings/agent-model", `{"channelModelId":"agent-text"}`, fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, body = %s", response.Code, response.Body.String())
	}
	updated := fixture.request(http.MethodPut, "/api/admin/settings/agent-model", `{"channelModelId":"agent-text"}`, fixture.adminCookie, "")
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}

	session := fixture.request(http.MethodGet, "/api/auth/session", "", fixture.adminCookie, "")
	if session.Code != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", session.Code, session.Body.String())
	}
	var envelope struct {
		Data struct {
			AgentDefaultModel *struct {
				ChannelModelID string `json:"channelModelId"`
				ChannelID      string `json:"channelId"`
				ModelKey       string `json:"modelKey"`
			} `json:"agentDefaultModel"`
		} `json:"data"`
	}
	if err := json.Unmarshal(session.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.AgentDefaultModel == nil || envelope.Data.AgentDefaultModel.ChannelModelID != item.ID || envelope.Data.AgentDefaultModel.ChannelID != channel.ID || envelope.Data.AgentDefaultModel.ModelKey != item.ModelKey {
		t.Fatalf("session Agent model = %#v, body = %s", envelope.Data.AgentDefaultModel, session.Body.String())
	}
}
