package database

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const agentRuntimeSkillChecksumMigrationBatchSize = 100

type agentRuntimeJSONFact struct {
	ID      string `gorm:"column:id"`
	Payload string `gorm:"column:payload"`
}

// migrateAgentRuntimeSkillChecksums performs a one-way schema normalization. Historical
// checkpoints and event facts retain their frozen instructions and gain the checksum now
// required by the single current runtime contract; no legacy execution branch remains.
func migrateAgentRuntimeSkillChecksums(db *gorm.DB) error {
	if err := migrateAgentRuntimeSkillChecksumsInTable(db, "agent_checkpoints", "state_json"); err != nil {
		return err
	}
	return migrateAgentRuntimeSkillChecksumsInTable(db, "agent_run_events", "payload_json")
}

func migrateAgentRuntimeSkillChecksumsInTable(db *gorm.DB, table string, column string) error {
	lastID := ""
	for {
		var facts []agentRuntimeJSONFact
		query := db.Table(table).Select("id, " + column + " AS payload").Order("id ASC").Limit(agentRuntimeSkillChecksumMigrationBatchSize)
		if lastID != "" {
			query = query.Where("id > ?", lastID)
		}
		if err := query.Find(&facts).Error; err != nil {
			return fmt.Errorf("读取 Agent Runtime Skill 历史事实失败: %w", err)
		}
		if len(facts) == 0 {
			return nil
		}
		for _, fact := range facts {
			if len(fact.Payload) > agentRuntimeMigrationCheckpointPayloadLimit {
				return fmt.Errorf("Agent Runtime 历史事实超过迁移上限: table=%s id=%s", table, fact.ID)
			}
			migrated, changed, err := addFrozenSkillChecksums(fact.Payload)
			if err != nil {
				return fmt.Errorf("迁移 Agent Runtime Skill 历史事实失败: table=%s id=%s: %w", table, fact.ID, err)
			}
			if changed {
				result := db.Table(table).Where("id = ? AND "+column+" = ?", fact.ID, fact.Payload).Update(column, migrated)
				if result.Error != nil {
					return fmt.Errorf("写入 Agent Runtime Skill 历史事实失败: table=%s id=%s: %w", table, fact.ID, result.Error)
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("Agent Runtime Skill 历史事实迁移发生并发冲突: table=%s id=%s", table, fact.ID)
				}
			}
			lastID = fact.ID
		}
	}
}

func addFrozenSkillChecksums(payload string) (string, bool, error) {
	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return "", false, err
	}
	configurationJSON, exists := state["configuration"]
	if !exists || string(configurationJSON) == "null" {
		return payload, false, nil
	}
	var configuration map[string]json.RawMessage
	if err := json.Unmarshal(configurationJSON, &configuration); err != nil {
		return "", false, err
	}
	skillsJSON, exists := configuration["skills"]
	if !exists || string(skillsJSON) == "null" {
		return payload, false, nil
	}
	var skills []json.RawMessage
	if err := json.Unmarshal(skillsJSON, &skills); err != nil {
		return "", false, err
	}
	changed := false
	for index, skillJSON := range skills {
		var skill map[string]json.RawMessage
		if err := json.Unmarshal(skillJSON, &skill); err != nil {
			return "", false, fmt.Errorf("skills[%d] 无效: %w", index, err)
		}
		var instructions string
		if err := json.Unmarshal(skill["instructions"], &instructions); err != nil || strings.TrimSpace(instructions) == "" {
			return "", false, fmt.Errorf("skills[%d].instructions 无效", index)
		}
		digest := sha256.Sum256([]byte(strings.TrimSpace(instructions)))
		expected := hex.EncodeToString(digest[:])
		checksumJSON, hasChecksum := skill["checksum"]
		if hasChecksum && string(checksumJSON) != "null" {
			var checksum string
			if err := json.Unmarshal(checksumJSON, &checksum); err != nil || checksum != expected {
				return "", false, fmt.Errorf("skills[%d].checksum 与冻结指令不一致", index)
			}
			continue
		}
		encodedChecksum, err := json.Marshal(expected)
		if err != nil {
			return "", false, err
		}
		skill["checksum"] = encodedChecksum
		migratedSkill, err := json.Marshal(skill)
		if err != nil {
			return "", false, err
		}
		skills[index] = migratedSkill
		changed = true
	}
	if !changed {
		return payload, false, nil
	}
	migratedSkills, err := json.Marshal(skills)
	if err != nil {
		return "", false, err
	}
	configuration["skills"] = migratedSkills
	migratedConfiguration, err := json.Marshal(configuration)
	if err != nil {
		return "", false, err
	}
	state["configuration"] = migratedConfiguration
	migratedState, err := json.Marshal(state)
	if err != nil {
		return "", false, err
	}
	return string(migratedState), true, nil
}
