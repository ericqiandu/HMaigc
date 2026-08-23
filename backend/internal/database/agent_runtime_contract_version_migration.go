package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const agentContractVersionMigrationID = "20260823-agent-contract-versions-v1"
const legacyAgentRuntimeVersion = 1
const legacyAgentPolicyVersion = 1

func migrateLegacyAgentContractVersions(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var completed []model.DataMigration
		result := tx.Where("id = ? AND completed_at IS NOT NULL", agentContractVersionMigrationID).Limit(1).Find(&completed)
		if result.Error != nil {
			return fmt.Errorf("读取 Agent 契约版本迁移状态失败: %w", result.Error)
		}
		if len(completed) == 1 {
			return rejectUnversionedAgentRuns(tx)
		}

		now := time.Now().UTC()
		pending := model.DataMigration{ID: agentContractVersionMigrationID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pending).Error; err != nil {
			return fmt.Errorf("创建 Agent 契约版本迁移状态失败: %w", err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&pending, "id = ?", agentContractVersionMigrationID).Error; err != nil {
			return fmt.Errorf("锁定 Agent 契约版本迁移状态失败: %w", err)
		}
		if pending.CompletedAt != nil {
			return nil
		}

		var runs []model.AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("runtime_version <= 0 OR policy_version <= 0").
			Order("created_at, id").Find(&runs).Error; err != nil {
			return fmt.Errorf("读取未版本化 Agent 运行失败: %w", err)
		}
		for _, run := range runs {
			if err := validateLegacyUnversionedAgentRun(run); err != nil {
				return err
			}
			if err := validateLegacyUnversionedAgentCheckpoint(tx, run); err != nil {
				return err
			}
			updated := tx.Model(&model.AgentRun{}).
				Where("id = ? AND runtime_version = 0 AND policy_version = 0", run.ID).
				Updates(struct {
					RuntimeVersion int `gorm:"column:runtime_version"`
					PolicyVersion  int `gorm:"column:policy_version"`
				}{RuntimeVersion: legacyAgentRuntimeVersion, PolicyVersion: legacyAgentPolicyVersion})
			if updated.Error != nil {
				return fmt.Errorf("回填 Agent 契约版本失败: run_id=%s: %w", run.ID, updated.Error)
			}
			if updated.RowsAffected != 1 {
				return fmt.Errorf("Agent 契约版本迁移发生并发冲突: run_id=%s", run.ID)
			}
		}

		completedAt := time.Now().UTC()
		completion := tx.Model(&model.DataMigration{}).
			Where("id = ? AND completed_at IS NULL", agentContractVersionMigrationID).
			Updates(struct {
				CompletedAt *time.Time
				UpdatedAt   time.Time
			}{CompletedAt: &completedAt, UpdatedAt: completedAt})
		if completion.Error != nil {
			return fmt.Errorf("完成 Agent 契约版本迁移状态失败: %w", completion.Error)
		}
		if completion.RowsAffected != 1 {
			return errors.New("Agent 契约版本迁移完成状态发生并发冲突")
		}
		return nil
	})
}

func validateLegacyUnversionedAgentCheckpoint(db *gorm.DB, run model.AgentRun) error {
	var checkpoint model.AgentCheckpoint
	err := db.Where("run_id = ? AND sequence = ?", run.ID, run.LastEventSequence).Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("未版本化 Agent 运行缺少最终 checkpoint: run_id=%s sequence=%d", run.ID, run.LastEventSequence)
	}
	if err != nil {
		return fmt.Errorf("读取未版本化 Agent 运行 checkpoint 失败: run_id=%s: %w", run.ID, err)
	}
	var state legacyAgentRuntimeStateV1
	if err := json.Unmarshal([]byte(checkpoint.StateJSON), &state); err != nil {
		return fmt.Errorf("未版本化 Agent 运行 checkpoint 无效: run_id=%s: %w", run.ID, err)
	}
	if checkpoint.StateVersion != run.StateVersion || state.StateVersion != run.StateVersion ||
		state.StepNumber != run.StepNumber || state.MaxSteps != run.MaxSteps || state.Status != run.Status {
		return fmt.Errorf(
			"未版本化 Agent 运行与最终 checkpoint 冲突: run_id=%s run=%s/%d/%d/%d checkpoint=%d/%s/%d/%d/%d",
			run.ID, run.Status, run.StateVersion, run.StepNumber, run.MaxSteps,
			checkpoint.StateVersion, state.Status, state.StateVersion, state.StepNumber, state.MaxSteps,
		)
	}
	return nil
}

func validateLegacyUnversionedAgentRun(run model.AgentRun) error {
	terminal := run.Status == agentruntime.RunSucceeded || run.Status == agentruntime.RunFailed || run.Status == agentruntime.RunCancelled
	if run.RuntimeVersion != 0 || run.PolicyVersion != 0 || run.ToolSchemaVersion != 1 || !terminal ||
		run.StateVersion < 1 || run.MaxSteps < 1 || run.LastEventSequence < 1 || run.CompletedAt == nil ||
		strings.TrimSpace(run.ModelRecordID) == "" || strings.TrimSpace(run.ModelKey) == "" {
		return fmt.Errorf(
			"未版本化 Agent 运行事实不符合首代终态契约: run_id=%s status=%s tool_schema_version=%d runtime_version=%d policy_version=%d state_version=%d max_steps=%d last_event_sequence=%d completed=%t",
			run.ID, run.Status, run.ToolSchemaVersion, run.RuntimeVersion, run.PolicyVersion,
			run.StateVersion, run.MaxSteps, run.LastEventSequence, run.CompletedAt != nil,
		)
	}
	return nil
}

func rejectUnversionedAgentRuns(db *gorm.DB) error {
	var run model.AgentRun
	result := db.Where("runtime_version <= 0 OR policy_version <= 0").Order("created_at, id").Limit(1).Find(&run)
	if result.Error != nil {
		return fmt.Errorf("核对 Agent 契约版本失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}
	return fmt.Errorf("Agent 契约版本迁移完成后出现未版本化运行: run_id=%s runtime_version=%d policy_version=%d", run.ID, run.RuntimeVersion, run.PolicyVersion)
}
