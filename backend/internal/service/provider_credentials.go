package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const (
	kuaiziProviderKind = "kuaizi"
	kuaiziAccountID    = "provider-account-kuaizi"
)

type SaveProviderEndpointRequest struct {
	BaseURL string `json:"baseUrl"`
}

type SaveProviderCredentialRequest struct {
	Key string `json:"key"`
}

type providerAuditMetadata struct {
	Provider    string `json:"provider"`
	Family      string `json:"family,omitempty"`
	Version     int64  `json:"version,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Result      string `json:"result"`
	Code        string `json:"code,omitempty"`
	TraceID     string `json:"traceId,omitempty"`
}

func (s *Service) AdminKuaiziProvider(actor *model.User) (*AdminProviderAccountView, error) {
	if err := s.requireProviderAdmin(actor, "provider.read"); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) SaveKuaiziEndpointCandidate(ctx context.Context, actor *model.User, req SaveProviderEndpointRequest) (*AdminProviderAccountView, error) {
	if err := s.requireProviderAdmin(actor, "provider.endpoint.save"); err != nil {
		return nil, err
	}
	parsed, err := ValidateKuaiziBaseURL(ctx, req.BaseURL, strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")))
	if err != nil {
		if auditErr := s.auditProviderAttempt(actor, "provider.endpoint.save", "", 0, "", "rejected", "invalid_endpoint", ""); auditErr != nil {
			return nil, auditErr
		}
		return nil, err
	}
	now := time.Now().UTC()
	account, accountErr := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if accountErr != nil && !errors.Is(accountErr, gorm.ErrRecordNotFound) {
		return nil, accountErr
	}
	if account == nil {
		account = &model.ProviderAccount{ID: kuaiziAccountID, ProviderKind: kuaiziProviderKind, Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now}
	}
	versions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return nil, err
	}
	nextVersion := int64(1)
	if len(versions) > 0 {
		nextVersion = versions[0].Version + 1
	}
	candidate := &model.ProviderEndpointVersion{
		ID: newID(), ProviderAccountID: account.ID, BaseURL: parsed.String(), Status: "pending",
		CreatedBy: actor.ID, Version: nextVersion, CreatedAt: now,
	}
	audit, err := newAdminAuditEvent(actor, "provider.endpoint.save", "provider_account", account.ID, "保存筷子服务地址候选", providerAuditMetadata{
		Provider: kuaiziProviderKind, Version: nextVersion, Result: "candidate_saved",
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveProviderEndpointCandidate(account, candidate, audit); err != nil {
		return nil, err
	}

	activeEndpointID := activeEndpointID(versions)
	credentials, err := s.repo.ProviderCredentials(account.ID)
	if err != nil {
		return nil, err
	}
	activeCredentials := make([]struct {
		credential model.ProviderCredential
		version    model.ProviderCredentialVersion
	}, 0, len(credentials))
	for _, credential := range credentials {
		credentialVersions, versionsErr := s.repo.ProviderCredentialVersions(credential.ID)
		if versionsErr != nil {
			return nil, versionsErr
		}
		if active := activeCredentialVersion(credentialVersions); active != nil {
			activeCredentials = append(activeCredentials, struct {
				credential model.ProviderCredential
				version    model.ProviderCredentialVersion
			}{credential: credential, version: *active})
		}
	}
	if len(activeCredentials) == 0 {
		return s.adminKuaiziProviderView()
	}
	client := NewKuaiziClient(KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), 15*time.Second))
	defer client.httpClient.CloseIdleConnections()
	verificationRecords := make([]repository.ProviderCredentialVerification, 0, len(activeCredentials))
	for _, active := range activeCredentials {
		key, decryptErr := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, active.credential.ID, active.version.Version, active.version.KeyCipher)
		if decryptErr != nil {
			if auditErr := s.auditProviderAttempt(actor, "provider.endpoint.verify", active.credential.Family, candidate.Version, active.version.KeyFingerprint, "failed", "decrypt_failed", ""); auditErr != nil {
				return nil, auditErr
			}
			return nil, decryptErr
		}
		fact, verifyErr := client.Balance(ctx, candidate.BaseURL, key)
		if verifyErr != nil {
			verificationError := kuaiziVerificationError(verifyErr)
			if auditErr := s.auditProviderAttempt(actor, "provider.endpoint.verify", active.credential.Family, candidate.Version, active.version.KeyFingerprint, "failed", verificationError.Code, verificationError.TraceID); auditErr != nil {
				return nil, auditErr
			}
			return nil, verifyErr
		}
		if auditErr := s.auditProviderAttempt(actor, "provider.endpoint.verify", active.credential.Family, candidate.Version, active.version.KeyFingerprint, "verified", "verified", fact.TraceID); auditErr != nil {
			return nil, auditErr
		}
		healthStatus := "healthy"
		if fact.WalletBalanceSubunits == "0" {
			healthStatus = "insufficient_balance"
		}
		verificationRecords = append(verificationRecords, repository.ProviderCredentialVerification{
			CredentialID: active.credential.ID, VersionID: active.version.ID, HealthStatus: healthStatus,
			HealthCode: "verified", HealthMessage: providerHealthMessage(healthStatus), Balance: fact.WalletBalanceSubunits,
			TraceID: fact.TraceID, CheckedAt: time.Now().UTC(), Verified: true,
		})
	}
	activationAudit, err := newAdminAuditEvent(actor, "provider.endpoint.activate", "provider_account", account.ID, "激活筷子服务地址", providerAuditMetadata{
		Provider: kuaiziProviderKind, Version: candidate.Version, Result: "activated", Code: "verified",
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.ActivateProviderEndpointWithCredentialVerifications(account.ID, candidate.ID, activeEndpointID, verificationRecords, now, activationAudit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) SaveKuaiziCredentialCandidate(ctx context.Context, actor *model.User, family string, req SaveProviderCredentialRequest) (*AdminProviderAccountView, error) {
	if err := s.requireProviderAdmin(actor, "provider.credential.save"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	family = strings.TrimSpace(family)
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		return nil, err
	}
	if _, ok := registry.Descriptor(kuaiziProviderKind, family); !ok {
		if auditErr := s.auditProviderAttempt(actor, "provider.credential.save", family, 0, "", "rejected", "unsupported_family", ""); auditErr != nil {
			return nil, auditErr
		}
		return nil, BadAuthRequest("筷子凭据系列尚未实现")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		if auditErr := s.auditProviderAttempt(actor, "provider.credential.save", family, 0, "", "rejected", "missing_key", ""); auditErr != nil {
			return nil, auditErr
		}
		return nil, BadAuthRequest("筷子 API Key 不能为空")
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, Conflict("请先保存筷子服务地址")
	}
	if err != nil {
		return nil, err
	}
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return nil, err
	}
	if len(endpointVersions) == 0 {
		return nil, Conflict("请先保存筷子服务地址")
	}
	now := time.Now().UTC()
	credential, credentialErr := s.repo.ProviderCredentialByFamily(account.ID, family)
	if credentialErr != nil && !errors.Is(credentialErr, gorm.ErrRecordNotFound) {
		return nil, credentialErr
	}
	if credential == nil {
		credential = &model.ProviderCredential{
			ID: deterministicProviderCredentialID(family), ProviderAccountID: account.ID, Family: family,
			HealthStatus: "unverified", Enabled: true, ConcurrencyLimit: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	versions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return nil, err
	}
	nextVersion := int64(1)
	if len(versions) > 0 {
		nextVersion = versions[0].Version + 1
	}
	ciphertext, err := s.EncryptProviderSecret(account.ID, credential.ID, nextVersion, key)
	if err != nil {
		return nil, err
	}
	fingerprint := providerKeyFingerprint(key)
	version := &model.ProviderCredentialVersion{
		ID: newID(), ProviderCredentialID: credential.ID, KeyCipher: ciphertext, KeyFingerprint: fingerprint,
		Status: "pending", Version: nextVersion, CreatedBy: actor.ID, CreatedAt: now,
	}
	audit, err := newAdminAuditEvent(actor, "provider.credential.save", "provider_credential", credential.ID, "保存筷子系列凭据候选", providerAuditMetadata{
		Provider: kuaiziProviderKind, Family: family, Version: nextVersion, Fingerprint: fingerprint, Result: "candidate_saved",
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveProviderCredentialCandidate(credential, version, audit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) VerifyKuaiziCredential(ctx context.Context, actor *model.User, family string) (*AdminProviderAccountView, error) {
	if err := s.requireProviderAdmin(actor, "provider.credential.verify"); err != nil {
		return nil, err
	}
	family = strings.TrimSpace(family)
	registry, registryErr := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if registryErr != nil {
		return nil, registryErr
	}
	if _, ok := registry.Descriptor(kuaiziProviderKind, family); !ok {
		if auditErr := s.auditProviderAttempt(actor, "provider.credential.verify", family, 0, "", "rejected", "unsupported_family", ""); auditErr != nil {
			return nil, auditErr
		}
		return nil, BadAuthRequest("筷子凭据系列尚未实现")
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil {
		return nil, err
	}
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return nil, err
	}
	endpoint := preferredVerificationEndpoint(endpointVersions)
	if endpoint == nil {
		return nil, Conflict("没有可验证的筷子服务地址")
	}
	credential, err := s.repo.ProviderCredentialByFamily(account.ID, family)
	if err != nil {
		return nil, err
	}
	versions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return nil, err
	}
	version := preferredVerificationCredential(versions)
	if version == nil {
		return nil, Conflict("没有可验证的筷子凭据候选")
	}
	key, err := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, credential.ID, version.Version, version.KeyCipher)
	if err != nil {
		if auditErr := s.auditProviderAttempt(actor, "provider.credential.verify", family, version.Version, version.KeyFingerprint, "failed", "decrypt_failed", ""); auditErr != nil {
			return nil, auditErr
		}
		return nil, err
	}
	checkedAt := time.Now().UTC()
	client := NewKuaiziClient(KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), 15*time.Second))
	defer client.httpClient.CloseIdleConnections()
	fact, verifyErr := client.Balance(ctx, endpoint.BaseURL, key)
	if verifyErr != nil {
		verificationError := kuaiziVerificationError(verifyErr)
		record := repository.ProviderCredentialVerification{
			CredentialID: credential.ID, VersionID: version.ID, HealthStatus: verificationError.HealthStatus,
			HealthCode: verificationError.Code, HealthMessage: providerHealthMessage(verificationError.HealthStatus), TraceID: verificationError.TraceID, CheckedAt: checkedAt,
		}
		audit, auditErr := newAdminAuditEvent(actor, "provider.credential.verify", "provider_credential", credential.ID, "验证筷子系列凭据失败", providerAuditMetadata{
			Provider: kuaiziProviderKind, Family: family, Version: version.Version, Fingerprint: version.KeyFingerprint,
			Result: "failed", Code: verificationError.Code, TraceID: verificationError.TraceID,
		})
		if auditErr != nil {
			return nil, auditErr
		}
		updateActiveHealth := version.Status == "active" && verificationError.HealthStatus != "unavailable" && verificationError.HealthStatus != "unknown"
		if recordErr := s.repo.RecordProviderCredentialVerification(record, updateActiveHealth, audit); recordErr != nil {
			return nil, recordErr
		}
		return nil, verifyErr
	}
	healthStatus := "healthy"
	if fact.WalletBalanceSubunits == "0" {
		healthStatus = "insufficient_balance"
	}
	record := repository.ProviderCredentialVerification{
		CredentialID: credential.ID, VersionID: version.ID, HealthStatus: healthStatus, HealthCode: "verified",
		HealthMessage: providerHealthMessage(healthStatus), Balance: fact.WalletBalanceSubunits, TraceID: fact.TraceID, CheckedAt: checkedAt, Verified: true,
	}
	audit, err := newAdminAuditEvent(actor, "provider.credential.verify", "provider_credential", credential.ID, "验证并激活筷子系列凭据", providerAuditMetadata{
		Provider: kuaiziProviderKind, Family: family, Version: version.Version, Fingerprint: version.KeyFingerprint,
		Result: "activated", Code: "verified", TraceID: fact.TraceID,
	})
	if err != nil {
		return nil, err
	}
	activeEndpoint := activeEndpointVersion(endpointVersions)
	activeCredential := activeCredentialVersion(versions)
	activeCredentialID := ""
	if activeCredential != nil {
		activeCredentialID = activeCredential.ID
	}
	if version.Status == "active" {
		if err := s.repo.RecordProviderCredentialVerification(record, true, audit); err != nil {
			return nil, err
		}
	} else if activeEndpoint == nil {
		if endpoint.Status != "pending" {
			return nil, repository.ErrProviderActivationConflict
		}
		if err := s.repo.ActivateProviderEndpointAndCredentialWithVerification(account.ID, endpoint.ID, "", credential.ID, version.ID, activeCredentialID, record, checkedAt, audit); err != nil {
			return nil, err
		}
	} else {
		if err := s.repo.ActivateProviderCredentialWithVerification(credential.ID, version.ID, activeCredentialID, record, checkedAt, audit); err != nil {
			return nil, err
		}
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) requireProviderAdmin(actor *model.User, action string) error {
	err := s.RequireAdmin(actor)
	if err != nil && actor != nil {
		if auditErr := s.auditProviderAttempt(actor, action, "", 0, "", "rejected", "forbidden", ""); auditErr != nil {
			return auditErr
		}
	}
	return err
}

func (s *Service) auditProviderAttempt(actor *model.User, action string, family string, version int64, fingerprint string, result string, code string, traceID string) error {
	if actor == nil {
		return nil
	}
	return s.appendAdminAudit(actor, action, "provider_account", kuaiziAccountID, "筷子账号管理尝试", providerAuditMetadata{
		Provider: kuaiziProviderKind, Family: family, Version: version, Fingerprint: fingerprint, Result: result, Code: code, TraceID: traceID,
	})
}

func deterministicProviderCredentialID(family string) string {
	sum := sha256.Sum256([]byte(kuaiziProviderKind + "\n" + family))
	return "pc-" + hex.EncodeToString(sum[:16])
}

func providerKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func kuaiziVerificationError(err error) *KuaiziVerificationError {
	var verificationError *KuaiziVerificationError
	if errors.As(err, &verificationError) {
		return verificationError
	}
	return newKuaiziVerificationError("unknown", "verification_error", "")
}

func providerHealthMessage(status string) string {
	switch status {
	case "healthy":
		return "凭据验证成功"
	case "insufficient_balance":
		return "凭据有效但余额不足"
	case "invalid":
		return "凭据无效"
	case "blocked":
		return "供应商拒绝当前出口 IP"
	case "rejected":
		return "供应商拒绝凭据验证"
	case "unavailable":
		return "供应商暂时不可用"
	default:
		return "凭据验证结果未知"
	}
}
