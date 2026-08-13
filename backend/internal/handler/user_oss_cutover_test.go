package handler

import (
	"net/http"
	"testing"
)

func TestUserOSSConfigurationRoutesAreRemoved(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		response := fixture.request(method, "/api/settings/oss", `{}`, fixture.userCookie, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s /api/settings/oss status = %d, want %d", method, response.Code, http.StatusNotFound)
		}
	}
}
