package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestKuaiziCredentialFirstVerificationActivatesEndpointAndKeyAtomically(t *testing.T) {
	server := newKuaiziBalanceServer(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"123456"},"trace_id":"trace-first"}`)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()

	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "sentinel-first-key"}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance")
	if err != nil {
		t.Fatal(err)
	}
	if view.Endpoint == nil || !view.Endpoint.Active || len(view.Credentials) != 1 || view.Credentials[0].Active == nil || !view.Credentials[0].Active.HasKey || view.Credentials[0].Active.HealthStatus != "healthy" || view.Credentials[0].Active.WalletBalanceSubunits != "123456" || view.Credentials[0].Candidate != nil {
		t.Fatalf("verified provider view = %#v", view)
	}
	assertProviderActiveCounts(t, db, 1, 1)
	serialized, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sentinel-first-key", providerSecretPrefix, "ApiKey"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("admin view leaked secret %q: %s", secret, serialized)
		}
	}
}

func TestKuaiziCredentialFailedCandidateDoesNotChangeActiveVersion(t *testing.T) {
	server := newSwitchableKuaiziBalanceServer(t)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	activateInitialKuaiziCredential(t, svc, admin, server.URL, "old-healthy-key")
	oldVersionID := activeCredentialVersionID(t, db)

	server.set(http.StatusOK, `{"code":401,"message":"invalid","trace_id":"trace-invalid"}`)
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "new-invalid-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err == nil {
		t.Fatal("invalid candidate verification succeeded")
	}
	if got := activeCredentialVersionID(t, db); got != oldVersionID {
		t.Fatalf("active credential changed from %s to %s", oldVersionID, got)
	}
	var credential model.ProviderCredential
	if err := db.First(&credential, "family = ?", "seedance").Error; err != nil {
		t.Fatal(err)
	}
	if credential.HealthStatus != "healthy" {
		t.Fatalf("old active credential health = %q", credential.HealthStatus)
	}
}

func TestKuaiziCredentialViewSeparatesLifecycleRoleFromHealthStatus(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		wantHealth string
	}{
		{name: "invalid", statusCode: http.StatusUnauthorized, wantHealth: "invalid"},
		{name: "blocked", statusCode: http.StatusForbidden, wantHealth: "blocked"},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, wantHealth: "unavailable"},
	} {
		t.Run(test.name+" first candidate", func(t *testing.T) {
			server := newKuaiziBalanceServer(t, test.statusCode, `upstream failure`)
			defer server.Close()
			svc, _ := openProviderCredentialService(t)
			admin := providerAdmin()
			if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "first-candidate-key"}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err == nil {
				t.Fatal("first candidate verification succeeded")
			}
			view, err := svc.AdminKuaiziProvider(admin)
			if err != nil {
				t.Fatal(err)
			}
			credential := providerCredentialLifecycleJSON(t, view)
			if string(credential["active"]) != "null" {
				t.Fatalf("first candidate active role = %s", credential["active"])
			}
			candidate := providerCredentialRoleJSON(t, credential, "candidate")
			if candidate.HealthStatus != test.wantHealth {
				t.Fatalf("first candidate health = %q, want %q", candidate.HealthStatus, test.wantHealth)
			}
		})

		t.Run(test.name+" active", func(t *testing.T) {
			server := newKuaiziBalanceServer(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"100"},"trace_id":"trace-active-role"}`)
			defer server.Close()
			svc, db := openProviderCredentialService(t)
			admin := providerAdmin()
			activateInitialKuaiziCredential(t, svc, admin, server.URL, "active-role-key")
			if err := db.Model(&model.ProviderCredential{}).Where("family = ?", "seedance").Update("health_status", test.wantHealth).Error; err != nil {
				t.Fatal(err)
			}
			view, err := svc.AdminKuaiziProvider(admin)
			if err != nil {
				t.Fatal(err)
			}
			credential := providerCredentialLifecycleJSON(t, view)
			active := providerCredentialRoleJSON(t, credential, "active")
			if active.HealthStatus != test.wantHealth {
				t.Fatalf("active health = %q, want %q", active.HealthStatus, test.wantHealth)
			}
			if string(credential["candidate"]) != "null" {
				t.Fatalf("active candidate role = %s", credential["candidate"])
			}
		})
	}
}

func providerCredentialLifecycleJSON(t *testing.T, view *AdminProviderAccountView) map[string]json.RawMessage {
	t.Helper()
	serialized, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Credentials []map[string]json.RawMessage `json:"credentials"`
	}
	if err := json.Unmarshal(serialized, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Credentials) != 1 {
		t.Fatalf("credentials = %s", serialized)
	}
	return payload.Credentials[0]
}

func providerCredentialRoleJSON(t *testing.T, credential map[string]json.RawMessage, role string) AdminProviderCredentialVersionView {
	t.Helper()
	raw, exists := credential[role]
	if !exists || string(raw) == "null" {
		t.Fatalf("credential %s role is absent: %#v", role, credential)
	}
	var view AdminProviderCredentialVersionView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestKuaiziCredentialCandidateViewPreservesVerificationHealthClassification(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		contentLength int
		wantStatus    string
	}{
		{name: "service unavailable", status: http.StatusServiceUnavailable, body: `maintenance`, wantStatus: "unavailable"},
		{name: "response read error", status: http.StatusOK, body: `{`, contentLength: 10, wantStatus: "unavailable"},
		{name: "upstream rejection", status: http.StatusOK, body: `{"code":9001,"trace_id":"trace-rejected"}`, wantStatus: "rejected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.RWMutex
			status := http.StatusOK
			body := `{"code":0,"data":{"wallet_balance":"100"},"trace_id":"trace-healthy"}`
			contentLength := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				mu.RLock()
				defer mu.RUnlock()
				if contentLength > 0 {
					writer.Header().Set("Content-Length", strconv.Itoa(contentLength))
				}
				writer.WriteHeader(status)
				_, _ = io.WriteString(writer, body)
			}))
			defer server.Close()
			svc, _ := openProviderCredentialService(t)
			admin := providerAdmin()
			activateInitialKuaiziCredential(t, svc, admin, server.URL, "old-healthy-key")
			if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "new-candidate-key"}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			status = test.status
			body = test.body
			contentLength = test.contentLength
			mu.Unlock()
			if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err == nil {
				t.Fatal("candidate verification succeeded")
			}
			view, err := svc.AdminKuaiziProvider(admin)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Credentials) != 1 || view.Credentials[0].Candidate == nil {
				t.Fatalf("candidate view = %#v", view.Credentials)
			}
			if got := view.Credentials[0].Candidate.HealthStatus; got != test.wantStatus {
				t.Fatalf("candidate health = %q, want %q", got, test.wantStatus)
			}
		})
	}
}

func TestKuaiziCredentialZeroBalanceActivatesAsInsufficientBalance(t *testing.T) {
	server := newKuaiziBalanceServer(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"0"},"trace_id":"trace-zero"}`)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "zero-balance-key"}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance")
	if err != nil {
		t.Fatal(err)
	}
	if view.Credentials[0].Active == nil || view.Credentials[0].Active.HealthStatus != "insufficient_balance" || view.Credentials[0].Active.WalletBalanceSubunits != "0" || view.Credentials[0].Candidate != nil {
		t.Fatalf("zero-balance view = %#v", view.Credentials[0])
	}
	assertProviderActiveCounts(t, db, 1, 1)
}

func TestKuaiziEndpointTemporaryFailurePreservesOldHealthyEndpointAndKey(t *testing.T) {
	server := newSwitchableKuaiziBalanceServer(t)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	activateInitialKuaiziCredential(t, svc, admin, server.URL, "healthy-key")
	oldEndpointID := activeEndpointVersionID(t, db)
	server.set(http.StatusServiceUnavailable, `maintenance`)

	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err == nil {
		t.Fatal("temporary endpoint validation failure succeeded")
	}
	if got := activeEndpointVersionID(t, db); got != oldEndpointID {
		t.Fatalf("active endpoint changed from %s to %s", oldEndpointID, got)
	}
	var credential model.ProviderCredential
	if err := db.First(&credential, "family = ?", "seedance").Error; err != nil {
		t.Fatal(err)
	}
	if credential.HealthStatus != "healthy" {
		t.Fatalf("temporary 5xx changed old key health to %q", credential.HealthStatus)
	}
}

func TestKuaiziCredentialConcurrentVerificationKeepsSingleActiveVersion(t *testing.T) {
	server := newKuaiziBalanceServer(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"9"},"trace_id":"trace-race"}`)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "race-key"}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errorsByCall := make([]error, 2)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = svc.VerifyKuaiziCredential(context.Background(), admin, "seedance")
		}(index)
	}
	close(start)
	wait.Wait()
	succeeded := 0
	for _, err := range errorsByCall {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, repository.ErrProviderActivationConflict) {
			t.Fatalf("concurrent verification error = %v", err)
		}
	}
	if succeeded == 0 {
		t.Fatalf("all concurrent verifications failed: %#v", errorsByCall)
	}
	assertProviderActiveCounts(t, db, 1, 1)
}

func TestKuaiziCredentialSupersedesOlderPendingCandidate(t *testing.T) {
	server := newKuaiziBalanceServer(t, http.StatusOK, `{"code":0,"data":{"wallet_balance":"12"},"trace_id":"trace-latest"}`)
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "older-pending-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "latest-pending-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err != nil {
		t.Fatal(err)
	}
	latestActiveID := activeCredentialVersionID(t, db)
	if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err != nil {
		t.Fatal(err)
	}
	if got := activeCredentialVersionID(t, db); got != latestActiveID {
		t.Fatalf("second verification rolled active key back from %s to %s", latestActiveID, got)
	}
	var pendingCount int64
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("status = ?", "pending").Count(&pendingCount).Error; err != nil {
		t.Fatal(err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending credential versions after activation = %d", pendingCount)
	}
}

func TestKuaiziEndpointRejectedAttemptFailsExplicitlyWhenAuditCannotPersist(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SaveKuaiziEndpointCandidate(context.Background(), providerAdmin(), SaveProviderEndpointRequest{BaseURL: "https://api.example.com/path"})
	var authError *AuthError
	if err == nil || errors.As(err, &authError) {
		t.Fatalf("rejected attempt audit failure = %T %v, want explicit persistence error", err, err)
	}
}

func TestKuaiziCredentialActivationConflictCreatesFailureAuditAfterTransactionRollback(t *testing.T) {
	var activate func() error
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		var activationError error
		once.Do(func() { activationError = activate() })
		if activationError != nil {
			t.Errorf("inject activation conflict: %v", activationError)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"code":0,"data":{"wallet_balance":"8"},"trace_id":"trace-conflict"}`)
	}))
	defer server.Close()
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: "conflict-key"}); err != nil {
		t.Fatal(err)
	}
	var endpoint model.ProviderEndpointVersion
	if err := db.First(&endpoint, "status = ?", "pending").Error; err != nil {
		t.Fatal(err)
	}
	var credential model.ProviderCredential
	if err := db.First(&credential, "family = ?", "seedance").Error; err != nil {
		t.Fatal(err)
	}
	var version model.ProviderCredentialVersion
	if err := db.First(&version, "provider_credential_id = ? AND status = ?", credential.ID, "pending").Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	activate = func() error {
		now := time.Now().UTC()
		if err := repo.ActivateProviderEndpointVersion(endpoint.ProviderAccountID, endpoint.ID, "", now); err != nil {
			return err
		}
		return repo.ActivateProviderCredentialVersion(credential.ID, version.ID, "", now)
	}
	_, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance")
	if !errors.Is(err, repository.ErrProviderActivationConflict) {
		t.Fatalf("verification conflict error = %v", err)
	}
	var events []model.AdminAuditEvent
	if err := db.Order("created_at ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if strings.Contains(event.MetadataJSON, `"code":"activation_conflict"`) && strings.Contains(event.MetadataJSON, `"result":"failed"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("activation conflict failure audit missing: %#v", events)
	}
}

func TestKuaiziCredentialAuthenticatedCanceledSaveCreatesFailureAudit(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.SaveKuaiziCredentialCandidate(ctx, providerAdmin(), "seedance", SaveProviderCredentialRequest{Key: "unused-key"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled save error = %v", err)
	}
	var events []model.AdminAuditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !strings.Contains(events[0].MetadataJSON, `"code":"canceled"`) || !strings.Contains(events[0].MetadataJSON, `"result":"failed"`) {
		t.Fatalf("canceled save audits = %#v", events)
	}
}

func TestPublishKuaiziFamilyModelsBindsHealthyCredentialAndKeepsUnpricedModelsDisabled(t *testing.T) {
	svc, db := openProviderCredentialService(t)
	admin := providerAdmin()
	now := time.Now()
	account := model.ProviderAccount{ID: "kuaizi-account", ProviderKind: kuaiziProviderKind, Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	endpoint := model.ProviderEndpointVersion{ID: "kuaizi-endpoint", ProviderAccountID: account.ID, BaseURL: "https://aiopenapi.kuaizi.cn", Status: "active", Version: 1, CreatedAt: now}
	credential := model.ProviderCredential{ID: "seedance-credential", ProviderAccountID: account.ID, Family: "seedance", Enabled: true, HealthStatus: "healthy", CreatedAt: now, UpdatedAt: now}
	version := model.ProviderCredentialVersion{ID: "seedance-version", ProviderCredentialID: credential.ID, KeyCipher: "cipher", KeyFingerprint: "fingerprint", Status: "active", Version: 1, CreatedAt: now}
	channel := model.ModelChannel{ID: "kuaizi-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "筷子兼容接口", BaseURL: endpoint.BaseURL, InterfaceType: model.ChannelInterfaceAIOpenVideoVolcengine, CreatedAt: now, UpdatedAt: now}
	existing := model.ChannelModel{ID: "existing-fast", ChannelID: channel.ID, ModelKey: "doubao-seedance-2-0-fast-260128", DisplayName: "旧名称", BrandKey: model.InferModelBrandKey("doubao-seedance-2-0-fast-260128"), AccessPolicy: model.ModelAccessAuthenticated, Capability: "video", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 1_000_000, PriceConfigured: true, Enabled: true, PriceVersion: 3, CreatedAt: now, UpdatedAt: now}
	disabledPriced := model.ChannelModel{ID: "existing-pro", ChannelID: channel.ID, ModelKey: "doubao-seedance-2-0-260128", DisplayName: "旧 Pro", BrandKey: model.InferModelBrandKey("doubao-seedance-2-0-260128"), AccessPolicy: model.ModelAccessAuthenticated, Capability: "video", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 2_000_000, PriceConfigured: true, Enabled: false, PriceVersion: 2, CreatedAt: now, UpdatedAt: now}
	for _, item := range []any{&account, &endpoint, &credential, &version, &channel, &existing, &disabledPriced} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	view, err := svc.PublishKuaiziFamilyModels(admin, "seedance")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Adapters) != 1 || len(view.Adapters[0].Models) != 4 {
		t.Fatalf("adapters = %#v", view.Adapters)
	}
	var models []model.ChannelModel
	if err := db.Where("channel_id = ?", channel.ID).Order("model_key").Find(&models).Error; err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("model count = %d, want 4", len(models))
	}
	for _, item := range models {
		if item.ProviderCredentialID != credential.ID {
			t.Fatalf("%s credential = %q", item.ModelKey, item.ProviderCredentialID)
		}
		if item.ModelKey == existing.ModelKey {
			if !item.Enabled || !item.PriceConfigured || item.UnitPriceMicrocredits != 1_000_000 {
				t.Fatalf("priced model changed = %#v", item)
			}
		} else if item.ModelKey == disabledPriced.ModelKey {
			if item.Enabled || !item.PriceConfigured || item.UnitPriceMicrocredits != 2_000_000 {
				t.Fatalf("disabled priced model changed = %#v", item)
			}
		} else if item.Enabled || item.PriceConfigured {
			t.Fatalf("unpriced model published as available = %#v", item)
		}
	}
	for _, spec := range view.Adapters[0].Models {
		if !spec.Published || spec.ChannelModelID == "" {
			t.Fatalf("publication facts missing = %#v", spec)
		}
	}
}

func openProviderCredentialService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	t.Setenv("CANVAS_ENVIRONMENT", "development")
	dsn := filepath.Join(t.TempDir(), "provider.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return New(repository.New(db), t.TempDir()), db
}

func providerAdmin() *model.User {
	return &model.User{ID: "provider-admin", Username: "provider-admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
}

func newKuaiziBalanceServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("ApiKey") == "" {
			t.Error("balance request omitted ApiKey")
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	}))
}

type switchableKuaiziBalanceServer struct {
	*httptest.Server
	mu     sync.RWMutex
	status int
	body   string
}

func newSwitchableKuaiziBalanceServer(t *testing.T) *switchableKuaiziBalanceServer {
	t.Helper()
	fixture := &switchableKuaiziBalanceServer{status: http.StatusOK, body: `{"code":0,"data":{"wallet_balance":"100"},"trace_id":"trace-healthy"}`}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fixture.mu.RLock()
		defer fixture.mu.RUnlock()
		writer.WriteHeader(fixture.status)
		_, _ = io.WriteString(writer, fixture.body)
	}))
	return fixture
}

func (s *switchableKuaiziBalanceServer) set(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.body = body
}

func activateInitialKuaiziCredential(t *testing.T, svc *Service, admin *model.User, baseURL string, key string) {
	t.Helper()
	if _, err := svc.SaveKuaiziEndpointCandidate(context.Background(), admin, SaveProviderEndpointRequest{BaseURL: baseURL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveKuaiziCredentialCandidate(context.Background(), admin, "seedance", SaveProviderCredentialRequest{Key: key}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyKuaiziCredential(context.Background(), admin, "seedance"); err != nil {
		t.Fatal(err)
	}
}

func assertProviderActiveCounts(t *testing.T, db *gorm.DB, endpointWant int64, credentialWant int64) {
	t.Helper()
	var endpointCount int64
	if err := db.Model(&model.ProviderEndpointVersion{}).Where("status = ?", "active").Count(&endpointCount).Error; err != nil {
		t.Fatal(err)
	}
	var credentialCount int64
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("status = ?", "active").Count(&credentialCount).Error; err != nil {
		t.Fatal(err)
	}
	if endpointCount != endpointWant || credentialCount != credentialWant {
		t.Fatalf("active counts endpoint=%d credential=%d", endpointCount, credentialCount)
	}
}

func activeEndpointVersionID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var version model.ProviderEndpointVersion
	if err := db.First(&version, "status = ?", "active").Error; err != nil {
		t.Fatal(err)
	}
	return version.ID
}

func activeCredentialVersionID(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var version model.ProviderCredentialVersion
	if err := db.First(&version, "status = ?", "active").Error; err != nil {
		t.Fatal(err)
	}
	return version.ID
}
