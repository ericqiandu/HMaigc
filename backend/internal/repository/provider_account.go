package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProviderActivationConflict  = errors.New("provider activation conflict")
	ErrProviderBillingFactConflict = errors.New("provider billing fact conflict")
)

type ProviderCredentialSecret struct {
	ProviderAccountID    string `gorm:"column:provider_account_id"`
	ProviderCredentialID string `gorm:"column:provider_credential_id"`
	CredentialVersionID  string `gorm:"column:credential_version_id"`
	Version              int64  `gorm:"column:version"`
	KeyCipher            string `json:"-" gorm:"column:key_cipher"`
}

type ProviderCredentialVerification struct {
	CredentialID  string
	VersionID     string
	HealthStatus  string
	HealthCode    string
	HealthMessage string
	Balance       string
	TraceID       string
	CheckedAt     time.Time
	Verified      bool
}

func (r *Repository) ProviderAccountByKind(providerKind string) (*model.ProviderAccount, error) {
	var account model.ProviderAccount
	if err := r.db.First(&account, "provider_kind = ?", providerKind).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *Repository) ProviderEndpointVersions(accountID string) ([]model.ProviderEndpointVersion, error) {
	var versions []model.ProviderEndpointVersion
	err := r.db.Where("provider_account_id = ?", accountID).Order("version DESC").Find(&versions).Error
	return versions, err
}

func (r *Repository) ProviderCredentials(accountID string) ([]model.ProviderCredential, error) {
	var credentials []model.ProviderCredential
	err := r.db.Where("provider_account_id = ?", accountID).Order("family ASC").Find(&credentials).Error
	return credentials, err
}

func (r *Repository) ProviderCredentialByFamily(accountID string, family string) (*model.ProviderCredential, error) {
	var credential model.ProviderCredential
	if err := r.db.First(&credential, "provider_account_id = ? AND family = ?", accountID, family).Error; err != nil {
		return nil, err
	}
	return &credential, nil
}

func (r *Repository) ProviderCredentialVersions(credentialID string) ([]model.ProviderCredentialVersion, error) {
	var versions []model.ProviderCredentialVersion
	err := r.db.Where("provider_credential_id = ?", credentialID).Order("version DESC").Find(&versions).Error
	return versions, err
}

// SaveProviderEndpointCandidate 将首次账号建档、候选版本与管理员审计作为一个事实提交。
func (r *Repository) SaveProviderEndpointCandidate(account *model.ProviderAccount, version *model.ProviderEndpointVersion, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var stored model.ProviderAccount
		lookup := tx.First(&stored, "id = ?", account.ID)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			if err := tx.Create(account).Error; err != nil {
				return err
			}
		} else if lookup.Error != nil {
			return lookup.Error
		} else if stored.ProviderKind != account.ProviderKind {
			return fmt.Errorf("provider account identity conflict: id=%s", account.ID)
		}
		if err := tx.Model(&model.ProviderEndpointVersion{}).
			Where("provider_account_id = ? AND status = ?", version.ProviderAccountID, "pending").
			Updates(map[string]interface{}{"status": "superseded", "retired_at": version.CreatedAt}).Error; err != nil {
			return err
		}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

// SaveProviderCredentialCandidate 保证 family 根、密文版本和审计要么全部提交，要么全部回滚。
func (r *Repository) SaveProviderCredentialCandidate(credential *model.ProviderCredential, version *model.ProviderCredentialVersion, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var stored model.ProviderCredential
		lookup := tx.First(&stored, "id = ?", credential.ID)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			if err := tx.Create(credential).Error; err != nil {
				return err
			}
		} else if lookup.Error != nil {
			return lookup.Error
		} else if stored.ProviderAccountID != credential.ProviderAccountID || stored.Family != credential.Family {
			return fmt.Errorf("provider credential identity conflict: id=%s", credential.ID)
		}
		if err := tx.Model(&model.ProviderCredentialVersion{}).
			Where("provider_credential_id = ? AND status = ?", version.ProviderCredentialID, "pending").
			Updates(map[string]interface{}{"status": "superseded", "retired_at": version.CreatedAt}).Error; err != nil {
			return err
		}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) RecordProviderCredentialVerification(record ProviderCredentialVerification, updateCredential bool, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateProviderCredentialVerificationTx(tx, record); err != nil {
			return err
		}
		if updateCredential {
			if err := updateProviderCredentialHealthTx(tx, record); err != nil {
				return err
			}
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) ActivateProviderCredentialWithVerification(credentialID string, versionID string, expectedActiveID string, record ProviderCredentialVerification, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "credential", credentialID); err != nil {
			return err
		}
		if err := activateProviderCredentialVersionTx(tx, credentialID, versionID, expectedActiveID, now); err != nil {
			return err
		}
		if err := updateProviderCredentialVerificationTx(tx, record); err != nil {
			return err
		}
		if err := updateProviderCredentialHealthTx(tx, record); err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

// ActivateProviderEndpointAndCredentialWithVerification 是首次接入唯一允许的激活路径，防止只启用 endpoint 或只启用 Key。
func (r *Repository) ActivateProviderEndpointAndCredentialWithVerification(accountID string, endpointVersionID string, expectedEndpointID string, credentialID string, credentialVersionID string, expectedCredentialID string, record ProviderCredentialVerification, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "endpoint", accountID); err != nil {
			return err
		}
		if err := lockProviderScopeTx(tx, "credential", credentialID); err != nil {
			return err
		}
		if err := activateProviderEndpointVersionTx(tx, accountID, endpointVersionID, expectedEndpointID, now); err != nil {
			return err
		}
		if err := activateProviderCredentialVersionTx(tx, credentialID, credentialVersionID, expectedCredentialID, now); err != nil {
			return err
		}
		if err := updateProviderCredentialVerificationTx(tx, record); err != nil {
			return err
		}
		if err := updateProviderCredentialHealthTx(tx, record); err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) ActivateProviderEndpointWithAudit(accountID string, versionID string, expectedActiveID string, now time.Time, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "endpoint", accountID); err != nil {
			return err
		}
		if err := activateProviderEndpointVersionTx(tx, accountID, versionID, expectedActiveID, now); err != nil {
			return err
		}
		return tx.Create(audit).Error
	})
}

// ActivateProviderEndpointWithCredentialVerifications 在 endpoint 提交点重新锁定并核对全部已验证凭据，封闭验证期间换 Key 的竞态。
func (r *Repository) ActivateProviderEndpointWithCredentialVerifications(accountID string, versionID string, expectedActiveID string, records []ProviderCredentialVerification, now time.Time, audit *model.AdminAuditEvent) error {
	ordered := append([]ProviderCredentialVerification(nil), records...)
	sort.Slice(ordered, func(left int, right int) bool { return ordered[left].CredentialID < ordered[right].CredentialID })
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "endpoint", accountID); err != nil {
			return err
		}
		for _, record := range ordered {
			if err := lockProviderScopeTx(tx, "credential", record.CredentialID); err != nil {
				return err
			}
			var active model.ProviderCredentialVersion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&active, "provider_credential_id = ? AND status = ?", record.CredentialID, "active").Error; err != nil {
				return err
			}
			if active.ID != record.VersionID {
				return fmt.Errorf("%w: credential=%s verified=%s actual=%s", ErrProviderActivationConflict, record.CredentialID, record.VersionID, active.ID)
			}
		}
		if err := activateProviderEndpointVersionTx(tx, accountID, versionID, expectedActiveID, now); err != nil {
			return err
		}
		for _, record := range ordered {
			if err := updateProviderCredentialVerificationTx(tx, record); err != nil {
				return err
			}
			if err := updateProviderCredentialHealthTx(tx, record); err != nil {
				return err
			}
		}
		return tx.Create(audit).Error
	})
}

func updateProviderCredentialHealthTx(tx *gorm.DB, record ProviderCredentialVerification) error {
	result := tx.Model(&model.ProviderCredential{}).Where("id = ?", record.CredentialID).Updates(map[string]interface{}{
		"health_status": record.HealthStatus, "health_code": record.HealthCode, "health_message": record.HealthMessage,
		"health_checked_at": record.CheckedAt, "updated_at": record.CheckedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrInvalidData
	}
	return nil
}

func updateProviderCredentialVerificationTx(tx *gorm.DB, record ProviderCredentialVerification) error {
	updates := map[string]interface{}{
		"last_verification_code": record.HealthCode, "last_verification_trace_id": record.TraceID,
	}
	if record.Balance != "" {
		updates["last_balance_subunits"] = record.Balance
		updates["last_balance_checked_at"] = record.CheckedAt
	}
	if record.Verified {
		updates["verified_at"] = record.CheckedAt
	}
	result := tx.Model(&model.ProviderCredentialVersion{}).
		Where("id = ? AND provider_credential_id = ?", record.VersionID, record.CredentialID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrInvalidData
	}
	return nil
}

func activateProviderEndpointVersionTx(tx *gorm.DB, accountID string, versionID string, expectedActiveID string, now time.Time) error {
	var candidate model.ProviderEndpointVersion
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&candidate, "id = ? AND provider_account_id = ?", versionID, accountID).Error; err != nil {
		return err
	}
	var active model.ProviderEndpointVersion
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_account_id = ? AND status = ?", accountID, "active").First(&active)
	if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}
	actualActiveID := ""
	if lookup.Error == nil {
		actualActiveID = active.ID
	}
	if actualActiveID != expectedActiveID || candidate.Status != "pending" {
		return fmt.Errorf("%w: endpoint account=%s expected=%s actual=%s candidate_status=%s", ErrProviderActivationConflict, accountID, expectedActiveID, actualActiveID, candidate.Status)
	}
	if active.ID != "" {
		result := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ? AND status = ?", active.ID, "active").Updates(map[string]interface{}{"status": "retired", "retired_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return ErrProviderActivationConflict
		}
	}
	result := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ? AND provider_account_id = ? AND status = ?", versionID, accountID, "pending").Updates(map[string]interface{}{"status": "active", "activated_at": now, "retired_at": nil})
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return result.Error
		}
		return ErrProviderActivationConflict
	}
	return nil
}

func activateProviderCredentialVersionTx(tx *gorm.DB, credentialID string, versionID string, expectedActiveID string, now time.Time) error {
	var candidate model.ProviderCredentialVersion
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&candidate, "id = ? AND provider_credential_id = ?", versionID, credentialID).Error; err != nil {
		return err
	}
	var active model.ProviderCredentialVersion
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_credential_id = ? AND status = ?", credentialID, "active").First(&active)
	if lookup.Error != nil && !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		return lookup.Error
	}
	actualActiveID := ""
	if lookup.Error == nil {
		actualActiveID = active.ID
	}
	if actualActiveID != expectedActiveID || candidate.Status != "pending" {
		return fmt.Errorf("%w: credential=%s expected=%s actual=%s candidate_status=%s", ErrProviderActivationConflict, credentialID, expectedActiveID, actualActiveID, candidate.Status)
	}
	if active.ID != "" {
		result := tx.Model(&model.ProviderCredentialVersion{}).Where("id = ? AND status = ?", active.ID, "active").Updates(map[string]interface{}{"status": "retired", "retired_at": now})
		if result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return ErrProviderActivationConflict
		}
	}
	result := tx.Model(&model.ProviderCredentialVersion{}).Where("id = ? AND provider_credential_id = ? AND status = ?", versionID, credentialID, "pending").Updates(map[string]interface{}{"status": "active", "activated_at": now, "retired_at": nil})
	if result.Error != nil || result.RowsAffected != 1 {
		if result.Error != nil {
			return result.Error
		}
		return ErrProviderActivationConflict
	}
	return nil
}

func (r *Repository) CreateProviderAccount(account *model.ProviderAccount) error {
	return r.db.Create(account).Error
}

func (r *Repository) CreateProviderEndpointVersion(version *model.ProviderEndpointVersion) error {
	return r.db.Create(version).Error
}

func (r *Repository) CreateProviderCredential(credential *model.ProviderCredential) error {
	return r.db.Create(credential).Error
}

func (r *Repository) CreateProviderCredentialVersion(version *model.ProviderCredentialVersion) error {
	return r.db.Create(version).Error
}

// ProviderCredentialSecrets 返回启动校验所需的最小 AAD 与密文事实，调用方不得记录 KeyCipher。
func (r *Repository) ProviderCredentialSecrets() ([]ProviderCredentialSecret, error) {
	var secrets []ProviderCredentialSecret
	err := r.db.Table("provider_credential_versions AS versions").
		Select("COALESCE(credentials.provider_account_id, '') AS provider_account_id, versions.provider_credential_id, versions.id AS credential_version_id, versions.version, versions.key_cipher").
		Joins("LEFT JOIN provider_credentials AS credentials ON credentials.id = versions.provider_credential_id").
		Where("versions.key_cipher <> ''").
		Order("versions.created_at ASC, versions.id ASC").
		Scan(&secrets).Error
	return secrets, err
}

func (r *Repository) CreateProviderTaskFact(fact *model.ProviderTaskFact) error {
	return r.db.Create(fact).Error
}

// ActivateProviderEndpointVersion 以期望活动版本做乐观并发门禁，竞争轮换只有一个调用可以提交。
func (r *Repository) ActivateProviderEndpointVersion(accountID string, versionID string, expectedActiveID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "endpoint", accountID); err != nil {
			return err
		}
		return activateProviderEndpointVersionTx(tx, accountID, versionID, expectedActiveID, now)
	})
}

// ActivateProviderCredentialVersion 与 endpoint 使用相同的期望版本契约，避免并发换 Key 覆盖彼此。
func (r *Repository) ActivateProviderCredentialVersion(credentialID string, versionID string, expectedActiveID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "credential", credentialID); err != nil {
			return err
		}
		return activateProviderCredentialVersionTx(tx, credentialID, versionID, expectedActiveID, now)
	})
}

// RecordProviderBillingFact 只把完全相同 digest 视为上游重放；冲突事实保持原样并显式报错。
func (r *Repository) RecordProviderBillingFact(candidate *model.ProviderBillingFact) (*model.ProviderBillingFact, bool, error) {
	if candidate == nil {
		return nil, false, errors.New("provider billing fact is required")
	}
	if strings.TrimSpace(candidate.PayloadDigest) == "" {
		return nil, false, errors.New("provider billing payload digest is required")
	}
	stored := model.ProviderBillingFact{}
	created := false
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if candidate.UpstreamOrderID != "" {
			if err := lockProviderScopeTx(tx, "billing", candidate.ProviderCredentialVersionID+"\n"+candidate.UpstreamOrderID); err != nil {
				return err
			}
			lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stored,
				"provider_credential_version_id = ? AND upstream_order_id = ?", candidate.ProviderCredentialVersionID, candidate.UpstreamOrderID)
			if lookup.Error == nil {
				if stored.PayloadDigest != candidate.PayloadDigest {
					return fmt.Errorf("%w: credential_version=%s upstream_order=%s", ErrProviderBillingFactConflict, candidate.ProviderCredentialVersionID, candidate.UpstreamOrderID)
				}
				return nil
			}
			if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
		}
		if err := tx.Create(candidate).Error; err != nil {
			return err
		}
		stored = *candidate
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &stored, created, nil
}

func lockProviderScopeTx(tx *gorm.DB, scope string, id string) error {
	lockKey := scope + "\n" + id
	switch tx.Dialector.Name() {
	case "postgres":
		return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, lockKey).Error
	case "sqlite":
		switch scope {
		case "endpoint":
			return tx.Exec("UPDATE provider_accounts SET updated_at = updated_at WHERE id = ?", id).Error
		case "credential":
			return tx.Exec("UPDATE provider_credentials SET updated_at = updated_at WHERE id = ?", id).Error
		case "billing":
			return tx.Exec("UPDATE provider_billing_facts SET observed_at = observed_at WHERE 1 = 0").Error
		default:
			return fmt.Errorf("unsupported provider lock scope %s", scope)
		}
	default:
		return fmt.Errorf("provider locking is unsupported for %s", tx.Dialector.Name())
	}
}
