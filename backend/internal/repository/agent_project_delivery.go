package repository

import (
	"database/sql"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
)

type AgentProjectDeliverySnapshot struct {
	Project    model.Project
	Shots      []model.Shot
	Candidates []model.ProjectAssetCandidate
}

// 同一只读快照内校验权限并读取实体，避免把不同修订的数据拼成“当前产物”。
func (r *Repository) AgentProjectDeliveryFacts(userID, projectID string) (AgentProjectDeliverySnapshot, error) {
	var facts AgentProjectDeliverySnapshot
	err := r.db.Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		project, err := repo.ProjectReadableForUser(userID, projectID)
		if err != nil {
			return err
		}
		facts.Project = *project
		facts.Shots, err = repo.ProjectShots(projectID)
		if err != nil {
			return err
		}
		facts.Candidates, err = repo.ProjectAssetCandidates(projectID)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return AgentProjectDeliverySnapshot{}, err
	}
	return facts, nil
}
