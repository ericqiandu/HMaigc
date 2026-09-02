package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestAgentDefaultModelRoutesRequireAdminAndPublishValidatedReference(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "agent-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Agent", InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now}
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

func TestAgentDefaultVisionModelRoutesRequireAdmin(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	now := time.Now().UTC()
	channel := model.ModelChannel{ID: "vision-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "Vision", BaseURL: "https://api.deepseek.com", APIKey: "vision-secret", InterfaceType: model.ChannelInterfaceChatCompletion, CreatedAt: now, UpdatedAt: now}
	item := model.ChannelModel{ID: "vision-model", ChannelID: channel.ID, ModelKey: "deepseek-v4-flash-vision-exp", AccessPolicy: model.ModelAccessAuthenticated, Capability: "vision", BillingMode: "token_usage", PriceStrategy: "token", PriceConfigured: true, Enabled: true, PriceVersion: 3, CreatedAt: now, UpdatedAt: now}
	pricing := model.ModelPricing{ID: "vision-pricing", ChannelID: channel.ID, Model: item.ModelKey, Capability: "vision", Currency: "CNY", InputPerMillionMicros: 1_000_000, OutputPerMillionMicros: 2_000_000, MaxOutputTokens: 8_192, CreatedAt: now, UpdatedAt: now}
	if err := fixture.db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/admin/settings/agent-vision-model"
	if response := fixture.request(http.MethodGet, path, "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPut, path, `{"channelModelId":"vision-model"}`, fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, body = %s", response.Code, response.Body.String())
	}
	updated := fixture.request(http.MethodPut, path, `{"channelModelId":"vision-model"}`, fixture.adminCookie, "")
	if updated.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	loaded := fixture.request(http.MethodGet, path, "", fixture.adminCookie, "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"channelModelId":"vision-model"`) {
		t.Fatalf("get status = %d, body = %s", loaded.Code, loaded.Body.String())
	}
}
