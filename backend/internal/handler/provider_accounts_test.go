package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderAccountHandlerSetsSecurityHeadersForAuthSuccessAndErrors(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		cookie string
		want   int
	}{
		{name: "anonymous", method: http.MethodGet, path: "/api/admin/providers/kuaizi", want: http.StatusUnauthorized},
		{name: "non admin", method: http.MethodGet, path: "/api/admin/providers/kuaizi", cookie: fixture.userCookie, want: http.StatusForbidden},
		{name: "admin success", method: http.MethodGet, path: "/api/admin/providers/kuaizi", cookie: fixture.adminCookie, want: http.StatusOK},
		{name: "bad request", method: http.MethodPut, path: "/api/admin/providers/kuaizi", body: `{"baseUrl":"https://api.example.com/path"}`, cookie: fixture.adminCookie, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := fixture.request(test.method, test.path, test.body, test.cookie, "")
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.want, response.Body.String())
			}
			assertProviderAccountSecurityHeaders(t, response)
		})
	}

	fixture.closeDB()
	response := fixture.request(http.MethodGet, "/api/admin/providers/kuaizi", "", fixture.adminCookie, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("closed database status = %d, body = %s", response.Code, response.Body.String())
	}
	assertProviderAccountSecurityHeaders(t, response)
}

func TestProviderAccountHandlerNeverReturnsOrAuditsCredentialSecret(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":0,"data":{"balance":"42"},"trace_id":"handler-trace"}`)
	}))
	defer server.Close()

	endpointResponse := fixture.request(http.MethodPut, "/api/admin/providers/kuaizi", `{"baseUrl":"`+server.URL+`"}`, fixture.adminCookie, "")
	if endpointResponse.Code != http.StatusOK {
		t.Fatalf("endpoint status = %d, body = %s", endpointResponse.Code, endpointResponse.Body.String())
	}
	const secret = "sentinel-handler-secret"
	credentialResponse := fixture.request(http.MethodPut, "/api/admin/providers/kuaizi/credentials/seedance", `{"key":"`+secret+`"}`, fixture.adminCookie, "ApiKey "+secret)
	if credentialResponse.Code != http.StatusOK {
		t.Fatalf("credential status = %d, body = %s", credentialResponse.Code, credentialResponse.Body.String())
	}
	verifyResponse := fixture.request(http.MethodPost, "/api/admin/providers/kuaizi/credentials/seedance/verify", "", fixture.adminCookie, "ApiKey "+secret)
	if verifyResponse.Code != http.StatusOK {
		t.Fatalf("verify status = %d, body = %s", verifyResponse.Code, verifyResponse.Body.String())
	}
	for _, response := range []*httptest.ResponseRecorder{endpointResponse, credentialResponse, verifyResponse} {
		assertProviderAccountSecurityHeaders(t, response)
		serialized := response.Body.String()
		for _, forbidden := range []string{secret, "enc:provider:v1:", "ApiKey " + secret} {
			if strings.Contains(serialized, forbidden) {
				t.Fatalf("provider response leaked %q: %s", forbidden, serialized)
			}
		}
	}

	var events []model.AdminAuditEvent
	if err := fixture.db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secret, "enc:provider:v1:", "ApiKey " + secret} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provider audit leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestProviderAccountHandlerAuditsForbiddenActorButNotAnonymousRequest(t *testing.T) {
	fixture := openProviderAccountHandlerFixture(t)
	anonymous := fixture.request(http.MethodGet, "/api/admin/providers/kuaizi", "", "", "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", anonymous.Code)
	}
	var afterAnonymous int64
	if err := fixture.db.Model(&model.AdminAuditEvent{}).Count(&afterAnonymous).Error; err != nil {
		t.Fatal(err)
	}
	if afterAnonymous != 0 {
		t.Fatalf("anonymous request created %d domain audits", afterAnonymous)
	}

	forbidden := fixture.request(http.MethodGet, "/api/admin/providers/kuaizi", "", fixture.userCookie, "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d", forbidden.Code)
	}
	var events []model.AdminAuditEvent
	if err := fixture.db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ActorUserID != fixture.userID || !strings.Contains(events[0].MetadataJSON, `"result":"rejected"`) {
		t.Fatalf("forbidden audits = %#v", events)
	}
}

func TestProviderAccountHandlerMapsActivationConflictTo409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	failProviderAccount(context, repository.ErrProviderActivationConflict)
	if response.Code != http.StatusConflict {
		t.Fatalf("activation conflict status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestProviderAccountHandlerMapsVerificationHealthToStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		health string
		want   int
	}{
		{health: "invalid", want: http.StatusBadRequest},
		{health: "unavailable", want: http.StatusServiceUnavailable},
		{health: "unknown", want: http.StatusBadGateway},
	} {
		response := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(response)
		failProviderAccount(context, &service.KuaiziVerificationError{HealthStatus: test.health, Code: "test"})
		if response.Code != test.want {
			t.Fatalf("health %s status = %d, want %d", test.health, response.Code, test.want)
		}
	}
}

type providerAccountHandlerFixture struct {
	router      *gin.Engine
	db          *gorm.DB
	adminCookie string
	userCookie  string
	userID      string
}

func openProviderAccountHandlerFixture(t *testing.T) *providerAccountHandlerFixture {
	t.Helper()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "handler.db")+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	admin := model.User{ID: "handler-admin", Username: "handler-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	user := model.User{ID: "handler-user", Username: "handler-user", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]model.User{admin, user}).Error; err != nil {
		t.Fatal(err)
	}
	adminCookie := createProviderHandlerSession(t, db, admin.ID, "handler-admin-session", "handler-admin-token", now)
	userCookie := createProviderHandlerSession(t, db, user.ID, "handler-user-session", "handler-user-token", now)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterProviderAccountRoutes(router.Group("/api"), service.New(repository.New(db), t.TempDir()))
	fixture := &providerAccountHandlerFixture{router: router, db: db, adminCookie: adminCookie, userCookie: userCookie, userID: user.ID}
	t.Cleanup(fixture.closeDB)
	return fixture
}

func (f *providerAccountHandlerFixture) request(method string, path string, body string, cookie string, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		request.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: cookie})
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func (f *providerAccountHandlerFixture) closeDB() {
	sqlDB, err := f.db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func createProviderHandlerSession(t *testing.T, db *gorm.DB, userID string, sessionID string, token string, now time.Time) string {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	session := model.AuthSession{ID: sessionID, UserID: userID, TokenHash: hex.EncodeToString(digest[:]), ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	return sessionID + "." + token
}

func assertProviderAccountSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for header, want := range map[string]string{
		"Cache-Control": "private, no-store", "Pragma": "no-cache", "Referrer-Policy": "no-referrer",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}
