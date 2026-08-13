package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestWatermarkPolicyRoutesEnforceAuthStrictJSONAndVersionConflict(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)

	if response := fixture.request(http.MethodGet, "/api/me/watermark-preference", "", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous preference status = %d, body=%s", response.Code, response.Body.String())
	} else {
		assertPrivateNoStoreHeaders(t, response.Header())
	}
	initial := fixture.request(http.MethodGet, "/api/me/watermark-preference", "", fixture.userCookie, "")
	if initial.Code != http.StatusOK || !responseDataContains(initial.Body.Bytes(), `"status":"policy_unavailable"`) {
		t.Fatalf("initial preference = %d, body=%s", initial.Code, initial.Body.String())
	}
	assertPrivateNoStoreHeaders(t, initial.Header())
	if response := fixture.request(http.MethodPost, "/api/admin/legal/ai-watermark-policy/publications", `{"managementRuleRichText":"<p>规则</p>","watermarkPolicyUrl":"https://example.com/v1"}`, fixture.userCookie, ""); response.Code != http.StatusForbidden {
		t.Fatalf("non-admin publish = %d, body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPost, "/api/admin/legal/ai-watermark-policy/publications", `{"managementRuleRichText":"<p>规则</p>","watermarkPolicyUrl":"https://example.com/v1","extra":true}`, fixture.adminCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown publication field = %d, body=%s", response.Code, response.Body.String())
	}
	v1Response := fixture.request(http.MethodPost, "/api/admin/legal/ai-watermark-policy/publications", `{"managementRuleRichText":"<p>规则一</p>","watermarkPolicyUrl":"https://example.com/v1"}`, fixture.adminCookie, "")
	if v1Response.Code != http.StatusOK {
		t.Fatalf("publish v1 = %d, body=%s", v1Response.Code, v1Response.Body.String())
	}
	v1ID := publicationIDFromResponse(t, v1Response.Body.Bytes())
	if response := fixture.request(http.MethodPut, "/api/me/watermark-preference", `{"removeWatermark":true,"publicationId":"`+v1ID+`","extra":true}`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown preference field = %d, body=%s", response.Code, response.Body.String())
	}
	active := fixture.request(http.MethodPut, "/api/me/watermark-preference", `{"removeWatermark":true,"publicationId":"`+v1ID+`"}`, fixture.userCookie, "")
	if active.Code != http.StatusOK || !responseDataContains(active.Body.Bytes(), `"status":"active"`) {
		t.Fatalf("active preference = %d, body=%s", active.Code, active.Body.String())
	}
	v2Response := fixture.request(http.MethodPost, "/api/admin/legal/ai-watermark-policy/publications", `{"managementRuleRichText":"<p>规则二</p>","watermarkPolicyUrl":"https://example.com/v2"}`, fixture.adminCookie, "")
	if v2Response.Code != http.StatusOK {
		t.Fatalf("publish v2 = %d, body=%s", v2Response.Code, v2Response.Body.String())
	}
	stale := fixture.request(http.MethodPut, "/api/me/watermark-preference", `{"removeWatermark":true,"publicationId":"`+v1ID+`"}`, fixture.userCookie, "")
	if stale.Code != http.StatusConflict || !responseDataContains(stale.Body.Bytes(), "水印规范已更新") {
		t.Fatalf("stale preference = %d, body=%s", stale.Code, stale.Body.String())
	}
	if response := fixture.request(http.MethodPut, "/api/me/watermark-preference", `{"removeWatermark":false} trailing`, fixture.userCookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status = %d, body=%s", response.Code, response.Body.String())
	}
	var events int64
	if err := fixture.db.Model(&model.UserWatermarkPreferenceEvent{}).Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("preference event count = %d, err=%v", events, err)
	}
}

func assertPrivateNoStoreHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	for key, want := range map[string]string{
		"Cache-Control":   "private, no-store",
		"Pragma":          "no-cache",
		"Referrer-Policy": "no-referrer",
	} {
		if got := headers.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func responseDataContains(body []byte, needle string) bool {
	return len(body) >= len(needle) && strings.Contains(string(body), needle)
}

func publicationIDFromResponse(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" {
		t.Fatalf("publication id missing: %s", body)
	}
	return envelope.Data.ID
}
