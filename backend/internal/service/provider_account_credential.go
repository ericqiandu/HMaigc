package service

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const kuaiziAccountCredentialFamily = "account"

type legacyKuaiziCredential struct {
	credential model.ProviderCredential
	version    model.ProviderCredentialVersion
	plaintext  string
}

// MigrateKuaiziAccountCredential 将历史系列凭据硬切为唯一账号凭据。
// 迁移只在所有启用系列的活动 Key 原文完全一致时提交。
func (s *Service) MigrateKuaiziAccountCredential() error {
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	credentials, err := s.repo.ProviderCredentials(account.ID)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		if credential.Family != kuaiziAccountCredentialFamily {
			continue
		}
		if !credential.Enabled {
			return errors.New("筷子账号统一凭据已存在但未启用")
		}
		versions, versionErr := s.repo.ProviderCredentialVersions(credential.ID)
		if versionErr != nil {
			return versionErr
		}
		if activeCredentialVersion(versions) == nil {
			return errors.New("筷子账号统一凭据缺少活动版本")
		}
		for _, other := range credentials {
			if other.ID != credential.ID && other.Enabled {
				return fmt.Errorf("筷子账号统一凭据已启用，但旧系列 %s 仍处于启用状态", other.Family)
			}
		}
		credentialIDs := make([]string, 0, len(credentials))
		for _, item := range credentials {
			credentialIDs = append(credentialIDs, item.ID)
		}
		models, modelsErr := s.repo.ChannelModelsByProviderCredentials(credentialIDs)
		if modelsErr != nil {
			return modelsErr
		}
		for _, item := range models {
			if item.ProviderCredentialID != credential.ID {
				return fmt.Errorf("筷子模型 %s 仍绑定旧系列凭据", item.ModelKey)
			}
		}
		return nil
	}

	legacy := make([]legacyKuaiziCredential, 0, len(credentials))
	cipher := NewProviderSecretCipher(s.dataDir)
	for _, credential := range credentials {
		if !credential.Enabled {
			continue
		}
		versions, versionErr := s.repo.ProviderCredentialVersions(credential.ID)
		if versionErr != nil {
			return versionErr
		}
		version := activeCredentialVersion(versions)
		if version == nil {
			return fmt.Errorf("筷子系列 %s 没有活动凭据，无法迁移", credential.Family)
		}
		plaintext, decryptErr := cipher.Decrypt(account.ID, credential.ID, version.Version, version.KeyCipher)
		if decryptErr != nil {
			return fmt.Errorf("解密筷子系列 %s 凭据失败: %w", credential.Family, decryptErr)
		}
		legacy = append(legacy, legacyKuaiziCredential{credential: credential, version: *version, plaintext: plaintext})
	}
	if len(legacy) == 0 {
		return nil
	}
	sort.Slice(legacy, func(left int, right int) bool {
		return legacy[left].credential.Family < legacy[right].credential.Family
	})
	for index := 1; index < len(legacy); index++ {
		if len(legacy[index].plaintext) != len(legacy[0].plaintext) || subtle.ConstantTimeCompare([]byte(legacy[index].plaintext), []byte(legacy[0].plaintext)) != 1 {
			return fmt.Errorf("筷子系列凭据不一致: %s 与 %s", legacy[0].credential.Family, legacy[index].credential.Family)
		}
	}

	now := time.Now().UTC()
	credentialID := deterministicProviderCredentialID(kuaiziAccountCredentialFamily)
	ciphertext, err := s.EncryptProviderSecret(account.ID, credentialID, 1, legacy[0].plaintext)
	if err != nil {
		return err
	}
	healthStatus := "healthy"
	for _, item := range legacy {
		if item.credential.HealthStatus != "healthy" {
			healthStatus = "unverified"
			break
		}
	}
	concurrencyLimit := legacy[0].credential.ConcurrencyLimit
	for _, item := range legacy[1:] {
		if item.credential.ConcurrencyLimit < concurrencyLimit {
			concurrencyLimit = item.credential.ConcurrencyLimit
		}
	}
	shared := model.ProviderCredential{
		ID: credentialID, ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily,
		HealthStatus: healthStatus, HealthCode: legacy[0].credential.HealthCode, HealthMessage: legacy[0].credential.HealthMessage,
		Enabled: true, ConcurrencyLimit: concurrencyLimit, HealthCheckedAt: legacy[0].credential.HealthCheckedAt,
		CreatedAt: now, UpdatedAt: now,
	}
	activatedAt := now
	version := model.ProviderCredentialVersion{
		ID: newID(), ProviderCredentialID: credentialID, KeyCipher: ciphertext, KeyFingerprint: providerKeyFingerprint(legacy[0].plaintext),
		Status: "active", Version: 1, VerifiedAt: legacy[0].version.VerifiedAt, LastBalanceCheckedAt: legacy[0].version.LastBalanceCheckedAt,
		ActivatedAt: &activatedAt, LastVerificationCode: legacy[0].version.LastVerificationCode,
		LastVerificationTraceID: legacy[0].version.LastVerificationTraceID, LastBalanceSubunits: legacy[0].version.LastBalanceSubunits,
		CreatedBy: "system-migration", CreatedAt: now,
	}
	legacyVersionIDs := make(map[string]string, len(legacy))
	families := make([]string, 0, len(legacy))
	for _, item := range legacy {
		legacyVersionIDs[item.credential.ID] = item.version.ID
		families = append(families, item.credential.Family)
	}
	metadata, err := json.Marshal(struct {
		Families    []string `json:"families"`
		Fingerprint string   `json:"fingerprint"`
	}{Families: families, Fingerprint: version.KeyFingerprint})
	if err != nil {
		return err
	}
	audit := model.AdminAuditEvent{
		ID: newID(), ActorUserID: "system-migration", Action: "provider.credential.migrate_account",
		TargetType: "provider_account", TargetID: account.ID, Summary: "将筷子系列凭据收敛为账号统一凭据",
		MetadataJSON: string(metadata), CreatedAt: now,
	}
	return s.repo.MigrateKuaiziAccountCredential(repository.KuaiziAccountCredentialMigration{
		AccountID: account.ID, SharedCredential: shared, SharedVersion: version,
		LegacyCredentialVersionIDs: legacyVersionIDs, Audit: audit,
	})
}
