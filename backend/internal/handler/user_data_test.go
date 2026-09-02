package handler

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCanvasProjectDeletionHTTPContract(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	previousRuntime := runtimeService
	ConfigureRuntime(fixture.svc)
	t.Cleanup(func() { runtimeService = previousRuntime })
	const canvasID = "handler-canvas-deletion"
	const body = `{"project":{"id":"handler-canvas-deletion","title":"跨客户端删除","createdAt":"2026-08-25T00:00:00Z","updatedAt":"2026-08-25T00:00:00Z","nodes":[],"connections":[]}}`

	created := fixture.request(http.MethodPut, "/api/canvas-projects/"+canvasID, body, fixture.userCookie, "")
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	deleted := fixture.request(http.MethodDelete, "/api/canvas-projects/"+canvasID, "", fixture.userCookie, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	repeated := fixture.request(http.MethodDelete, "/api/canvas-projects/"+canvasID, "", fixture.userCookie, "")
	if repeated.Code != http.StatusOK {
		t.Fatalf("repeated delete status = %d, body = %s", repeated.Code, repeated.Body.String())
	}

	listed := fixture.request(http.MethodGet, "/api/canvas-projects", "", fixture.userCookie, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}
	var envelope struct {
		Data struct {
			Deletions []struct {
				ID string `json:"id"`
			} `json:"deletions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Deletions) != 1 || envelope.Data.Deletions[0].ID != canvasID {
		t.Fatalf("deletion list = %#v, body = %s", envelope.Data.Deletions, listed.Body.String())
	}

	recreated := fixture.request(http.MethodPut, "/api/canvas-projects/"+canvasID, body, fixture.userCookie, "")
	if recreated.Code != http.StatusGone {
		t.Fatalf("recreate status = %d, want %d, body = %s", recreated.Code, http.StatusGone, recreated.Body.String())
	}
	unrelatedDelete := fixture.request(http.MethodDelete, "/api/canvas-projects/"+canvasID, "", fixture.adminCookie, "")
	if unrelatedDelete.Code != http.StatusNotFound {
		t.Fatalf("unrelated delete status = %d, want %d, body = %s", unrelatedDelete.Code, http.StatusNotFound, unrelatedDelete.Body.String())
	}
}
