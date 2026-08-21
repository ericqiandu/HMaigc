package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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

// SystemProxyRuntime 只在一次受控上游请求的内存边界内存在，不进入 DTO、日志或数据库。
type SystemProxyRuntime struct {
	BaseURL                     string
	HeaderName                  string
	APIKey                      string
	ProviderEndpointVersionID   string
	ProviderCredentialVersionID string
}

func (s *Service) resolveFrozenKuaiziBillingRuntime(order *model.BillingOrder) (SystemProxyRuntime, error) {
	if order == nil || strings.TrimSpace(order.ProviderEndpointVersionID) == "" || strings.TrimSpace(order.ProviderCredentialVersionID) == "" {
		return SystemProxyRuntime{}, errors.New("筷子账单缺少冻结的供应商版本")
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil {
		return SystemProxyRuntime{}, err
	}
	endpoints, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return SystemProxyRuntime{}, err
	}
	var endpoint *model.ProviderEndpointVersion
	for index := range endpoints {
		if endpoints[index].ID == order.ProviderEndpointVersionID {
			endpoint = &endpoints[index]
			break
		}
	}
	credentials, err := s.repo.ProviderCredentials(account.ID)
	if err != nil {
		return SystemProxyRuntime{}, err
	}
	var credential *model.ProviderCredential
	var version *model.ProviderCredentialVersion
	for index := range credentials {
		versions, versionsErr := s.repo.ProviderCredentialVersions(credentials[index].ID)
		if versionsErr != nil {
			return SystemProxyRuntime{}, versionsErr
		}
		for versionIndex := range versions {
			if versions[versionIndex].ID == order.ProviderCredentialVersionID {
				credential = &credentials[index]
				version = &versions[versionIndex]
				break
			}
		}
		if version != nil {
			break
		}
	}
	if endpoint == nil || credential == nil || version == nil {
		return SystemProxyRuntime{}, errors.New("冻结的筷子供应商版本不存在")
	}
	key, err := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, credential.ID, version.Version, version.KeyCipher)
	if err != nil {
		return SystemProxyRuntime{}, errors.New("解密冻结的筷子 Key 失败")
	}
	return SystemProxyRuntime{
		BaseURL: endpoint.BaseURL, HeaderName: "ApiKey", APIKey: key,
		ProviderEndpointVersionID: endpoint.ID, ProviderCredentialVersionID: version.ID,
	}, nil
}

func (s *Service) ResolveSystemProxyRuntime(channel *model.ModelChannel, modelKey string) (SystemProxyRuntime, error) {
	if channel == nil {
		return SystemProxyRuntime{}, ServiceUnavailable("系统渠道不存在")
	}
	modelKey = strings.TrimPrefix(strings.TrimSpace(modelKey), "models/")
	if channel.InterfaceType == model.ChannelInterfaceChatCompletion {
		selected, err := s.PublicAgentDefaultModel()
		if err != nil {
			return SystemProxyRuntime{}, err
		}
		if selected == nil || selected.ChannelID != channel.ID || selected.ModelKey != modelKey {
			return SystemProxyRuntime{}, ServiceUnavailable("Agent 请求只能使用管理员配置且当前健康的全站默认模型")
		}
	}
	family, spec, managed := kuaiziProviderFamilyForModel(modelKey)
	if !isKuaiziChatChannelID(channel.ID) {
		return SystemProxyRuntime{BaseURL: channel.BaseURL, HeaderName: systemProxyHeaderName(channel.APIFormat), APIKey: channel.APIKey}, nil
	}
	if !managed || spec.Capability != "text" || channel.ID != deterministicKuaiziChatChannelID(family) {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子 Agent 渠道与模型系列不匹配")
	}
	item, err := s.repo.ChannelModelByKey(channel.ID, modelKey)
	if err != nil {
		return SystemProxyRuntime{}, fmt.Errorf("读取筷子 Agent 模型配置失败：%w", err)
	}
	if item.ProviderCredentialID == "" {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子 Agent 模型尚未发布或启用")
	}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if err != nil {
		return SystemProxyRuntime{}, fmt.Errorf("读取筷子科技账号失败：%w", err)
	}
	if !account.Enabled {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子科技账号不可用")
	}
	credential, err := s.repo.ProviderCredentialByFamily(account.ID, kuaiziAccountCredentialFamily)
	if err != nil {
		return SystemProxyRuntime{}, fmt.Errorf("读取筷子科技账号凭据失败：%w", err)
	}
	if credential.ID != item.ProviderCredentialID || !credential.Enabled || credential.HealthStatus != "healthy" {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子科技账号凭据不可用")
	}
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return SystemProxyRuntime{}, err
	}
	endpoint := activeEndpointVersion(endpointVersions)
	if endpoint == nil {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子科技服务地址不可用")
	}
	credentialVersions, err := s.repo.ProviderCredentialVersions(credential.ID)
	if err != nil {
		return SystemProxyRuntime{}, err
	}
	version := activeCredentialVersion(credentialVersions)
	if version == nil {
		return SystemProxyRuntime{}, ServiceUnavailable("筷子科技账号凭据没有活动版本")
	}
	key, err := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, credential.ID, version.Version, version.KeyCipher)
	if err != nil {
		return SystemProxyRuntime{}, ServiceUnavailable("解密筷子科技账号 Key 失败")
	}
	return SystemProxyRuntime{
		BaseURL: kuaiziChatCompletionsBaseURL(endpoint.BaseURL), HeaderName: "ApiKey", APIKey: key,
		ProviderEndpointVersionID: endpoint.ID, ProviderCredentialVersionID: version.ID,
	}, nil
}

func systemProxyHeaderName(apiFormat string) string {
	if apiFormat == "gemini" {
		return "x-goog-api-key"
	}
	return "Authorization"
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

type providerFailureAudit struct {
	actor       *model.User
	action      string
	family      string
	version     int64
	fingerprint string
	code        string
	traceID     string
	audited     bool
}

func (s *Service) AdminKuaiziProvider(actor *model.User) (view *AdminProviderAccountView, err error) {
	if err := s.requireProviderAdmin(actor, "provider.read"); err != nil {
		return nil, err
	}
	attempt := providerFailureAudit{actor: actor, action: "provider.read"}
	defer s.finalizeProviderFailureAudit(&attempt, &err)
	return s.adminKuaiziProviderView()
}

func (s *Service) SaveKuaiziEndpointCandidate(ctx context.Context, actor *model.User, req SaveProviderEndpointRequest) (view *AdminProviderAccountView, err error) {
	if err := s.requireProviderAdmin(actor, "provider.endpoint.save"); err != nil {
		return nil, err
	}
	attempt := providerFailureAudit{actor: actor, action: "provider.endpoint.save"}
	defer s.finalizeProviderFailureAudit(&attempt, &err)
	parsed, err := ValidateKuaiziBaseURL(ctx, req.BaseURL, strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")))
	if err != nil {
		attempt.code = "invalid_endpoint"
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
		if !credential.Enabled {
			continue
		}
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
		attempt.action = "provider.endpoint.verify"
		attempt.family = active.credential.Family
		attempt.version = candidate.Version
		attempt.fingerprint = active.version.KeyFingerprint
		key, decryptErr := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, active.credential.ID, active.version.Version, active.version.KeyCipher)
		if decryptErr != nil {
			attempt.code = "decrypt_failed"
			return nil, decryptErr
		}
		fact, verifyErr := client.Balance(ctx, candidate.BaseURL, key)
		if verifyErr != nil {
			verificationError := kuaiziVerificationError(verifyErr)
			attempt.code = verificationError.Code
			attempt.traceID = verificationError.TraceID
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
	attempt.action = "provider.endpoint.activate"
	attempt.family = ""
	attempt.version = candidate.Version
	attempt.fingerprint = ""
	attempt.code = ""
	attempt.traceID = ""
	if err := s.repo.ActivateProviderEndpointWithCredentialVerifications(account.ID, candidate.ID, activeEndpointID, verificationRecords, now, activationAudit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) SaveKuaiziCredentialCandidate(ctx context.Context, actor *model.User, req SaveProviderCredentialRequest) (view *AdminProviderAccountView, err error) {
	if err := s.requireProviderAdmin(actor, "provider.credential.save"); err != nil {
		return nil, err
	}
	attempt := providerFailureAudit{actor: actor, action: "provider.credential.save"}
	defer s.finalizeProviderFailureAudit(&attempt, &err)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		attempt.code = "missing_key"
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
	credential, credentialErr := s.repo.ProviderCredentialByFamily(account.ID, kuaiziAccountCredentialFamily)
	if credentialErr != nil && !errors.Is(credentialErr, gorm.ErrRecordNotFound) {
		return nil, credentialErr
	}
	if credential == nil {
		credential = &model.ProviderCredential{
			ID: deterministicProviderCredentialID(kuaiziAccountCredentialFamily), ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily,
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
	attempt.version = nextVersion
	attempt.fingerprint = fingerprint
	version := &model.ProviderCredentialVersion{
		ID: newID(), ProviderCredentialID: credential.ID, KeyCipher: ciphertext, KeyFingerprint: fingerprint,
		Status: "pending", Version: nextVersion, CreatedBy: actor.ID, CreatedAt: now,
	}
	audit, err := newAdminAuditEvent(actor, "provider.credential.save", "provider_credential", credential.ID, "保存筷子账号凭据候选", providerAuditMetadata{
		Provider: kuaiziProviderKind, Version: nextVersion, Fingerprint: fingerprint, Result: "candidate_saved",
	})
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveProviderCredentialCandidate(credential, version, audit); err != nil {
		return nil, err
	}
	return s.adminKuaiziProviderView()
}

func (s *Service) VerifyKuaiziCredential(ctx context.Context, actor *model.User) (view *AdminProviderAccountView, err error) {
	if err := s.requireProviderAdmin(actor, "provider.credential.verify"); err != nil {
		return nil, err
	}
	attempt := providerFailureAudit{actor: actor, action: "provider.credential.verify"}
	defer s.finalizeProviderFailureAudit(&attempt, &err)
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
	credential, err := s.repo.ProviderCredentialByFamily(account.ID, kuaiziAccountCredentialFamily)
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
	attempt.version = version.Version
	attempt.fingerprint = version.KeyFingerprint
	key, err := NewProviderSecretCipher(s.dataDir).Decrypt(account.ID, credential.ID, version.Version, version.KeyCipher)
	if err != nil {
		attempt.code = "decrypt_failed"
		return nil, err
	}
	checkedAt := time.Now().UTC()
	client := NewKuaiziClient(KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), 15*time.Second))
	defer client.httpClient.CloseIdleConnections()
	fact, verifyErr := client.Balance(ctx, endpoint.BaseURL, key)
	if verifyErr != nil {
		verificationError := kuaiziVerificationError(verifyErr)
		attempt.code = verificationError.Code
		attempt.traceID = verificationError.TraceID
		record := repository.ProviderCredentialVerification{
			CredentialID: credential.ID, VersionID: version.ID, HealthStatus: verificationError.HealthStatus,
			HealthCode: verificationError.Code, HealthMessage: providerHealthMessage(verificationError.HealthStatus), TraceID: verificationError.TraceID, CheckedAt: checkedAt,
		}
		audit, auditErr := newAdminAuditEvent(actor, "provider.credential.verify", "provider_credential", credential.ID, "验证筷子账号凭据失败", providerAuditMetadata{
			Provider: kuaiziProviderKind, Version: version.Version, Fingerprint: version.KeyFingerprint,
			Result: "failed", Code: verificationError.Code, TraceID: verificationError.TraceID,
		})
		if auditErr != nil {
			return nil, auditErr
		}
		updateActiveHealth := version.Status == "active" && verificationError.HealthStatus != "unavailable" && verificationError.HealthStatus != "unknown"
		if recordErr := s.repo.RecordProviderCredentialVerification(record, updateActiveHealth, audit); recordErr != nil {
			return nil, recordErr
		}
		attempt.audited = true
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
	audit, err := newAdminAuditEvent(actor, "provider.credential.verify", "provider_credential", credential.ID, "验证并激活筷子账号凭据", providerAuditMetadata{
		Provider: kuaiziProviderKind, Version: version.Version, Fingerprint: version.KeyFingerprint,
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

// AuditKuaiziProviderRejection 供已完成身份认证、但尚未进入 service DTO 的 HTTP 解析拒绝使用。
func (s *Service) AuditKuaiziProviderRejection(actor *model.User, action string, family string, code string) error {
	return s.auditProviderAttempt(actor, action, family, 0, "", "rejected", code, "")
}

func (s *Service) finalizeProviderFailureAudit(attempt *providerFailureAudit, operationError *error) {
	if attempt == nil || operationError == nil || *operationError == nil || attempt.audited {
		return
	}
	result, code, traceID := providerFailureAuditDetails(*operationError)
	if attempt.code != "" {
		code = attempt.code
	}
	if attempt.traceID != "" {
		traceID = attempt.traceID
	}
	if auditErr := s.auditProviderAttempt(
		attempt.actor, attempt.action, attempt.family, attempt.version, attempt.fingerprint, result, code, traceID,
	); auditErr != nil {
		// 审计失败优先暴露，禁止用原业务错误掩盖不可追溯事实。
		*operationError = auditErr
	}
}

func providerFailureAuditDetails(err error) (string, string, string) {
	if errors.Is(err, repository.ErrProviderActivationConflict) {
		return "failed", "activation_conflict", ""
	}
	var verificationError *KuaiziVerificationError
	if errors.As(err, &verificationError) {
		result := "failed"
		if verificationError.HealthStatus == "invalid" || verificationError.HealthStatus == "blocked" || verificationError.HealthStatus == "rejected" {
			result = "rejected"
		}
		return result, verificationError.Code, verificationError.TraceID
	}
	var authError *AuthError
	if errors.As(err, &authError) {
		code := "request_rejected"
		switch authError.Status {
		case 400:
			code = "bad_request"
		case 401:
			code = "unauthorized"
		case 403:
			code = "forbidden"
		case 409:
			code = "conflict"
		}
		return "rejected", code, ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "failed", "timeout", ""
	}
	if errors.Is(err, context.Canceled) {
		return "failed", "canceled", ""
	}
	return "failed", "service_failure", ""
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
	return newKuaiziVerificationError("verification_error", "")
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
