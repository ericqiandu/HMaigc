package repository

import (
	"errors"
	"fmt"
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

func (r *Repository) CreateProviderTaskFact(fact *model.ProviderTaskFact) error {
	return r.db.Create(fact).Error
}

// ActivateProviderEndpointVersion 以期望活动版本做乐观并发门禁，竞争轮换只有一个调用可以提交。
func (r *Repository) ActivateProviderEndpointVersion(accountID string, versionID string, expectedActiveID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "endpoint", accountID); err != nil {
			return err
		}
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
		if actualActiveID != expectedActiveID {
			return fmt.Errorf("%w: endpoint account=%s expected=%s actual=%s", ErrProviderActivationConflict, accountID, expectedActiveID, actualActiveID)
		}
		if candidate.Status != "pending" {
			return fmt.Errorf("%w: endpoint version=%s status=%s", ErrProviderActivationConflict, candidate.ID, candidate.Status)
		}
		if active.ID != "" {
			result := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ? AND status = ?", active.ID, "active").Updates(map[string]any{
				"status": "retired", "retired_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrProviderActivationConflict
			}
		}
		result := tx.Model(&model.ProviderEndpointVersion{}).Where("id = ? AND provider_account_id = ? AND status = ?", candidate.ID, accountID, "pending").Updates(map[string]any{
			"status": "active", "activated_at": now, "retired_at": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProviderActivationConflict
		}
		return nil
	})
}

// ActivateProviderCredentialVersion 与 endpoint 使用相同的期望版本契约，避免并发换 Key 覆盖彼此。
func (r *Repository) ActivateProviderCredentialVersion(credentialID string, versionID string, expectedActiveID string, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockProviderScopeTx(tx, "credential", credentialID); err != nil {
			return err
		}
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
		if actualActiveID != expectedActiveID {
			return fmt.Errorf("%w: credential=%s expected=%s actual=%s", ErrProviderActivationConflict, credentialID, expectedActiveID, actualActiveID)
		}
		if candidate.Status != "pending" {
			return fmt.Errorf("%w: credential version=%s status=%s", ErrProviderActivationConflict, candidate.ID, candidate.Status)
		}
		if active.ID != "" {
			result := tx.Model(&model.ProviderCredentialVersion{}).Where("id = ? AND status = ?", active.ID, "active").Updates(map[string]any{
				"status": "retired", "retired_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrProviderActivationConflict
			}
		}
		result := tx.Model(&model.ProviderCredentialVersion{}).Where("id = ? AND provider_credential_id = ? AND status = ?", candidate.ID, credentialID, "pending").Updates(map[string]any{
			"status": "active", "activated_at": now, "retired_at": nil,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProviderActivationConflict
		}
		return nil
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
