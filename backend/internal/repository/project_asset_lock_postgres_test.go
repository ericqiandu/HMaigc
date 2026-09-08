package repository

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"
)

func TestPostgresProjectAssetReplacementLock(t *testing.T) {
	for _, scenario := range []struct{ bindingFirst, reference bool }{{true, false}, {false, false}, {true, true}, {false, true}} {
		bindingFirst := scenario.bindingFirst
		name := "replacement_first"
		if bindingFirst {
			name = "binding_first"
		}
		if scenario.reference {
			name += "_reference"
		}
		t.Run(name, func(t *testing.T) {
			db := testsupport.OpenPaymentIntegrationPostgres(t)
			if err := database.MigrateBaseSchema(db); err != nil {
				t.Fatal(err)
			}
			project := model.Project{ID: "lock-project", UserID: "lock-user", Revision: 1}
			unit := model.ProjectUnit{ID: "lock-unit", ProjectID: project.ID}
			shot := model.Shot{ID: "old-shot", ProjectID: project.ID, UnitID: unit.ID}
			if err := db.Create(&project).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&unit).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&shot).Error; err != nil {
				t.Fatal(err)
			}
			second := openSecondAgentPostgresConnection(t, db)
			pool, err := second.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			var pid int
			if err := second.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
				t.Fatal(err)
			}
			bind := func(conn *gorm.DB) error {
				if scenario.reference {
					return New(conn).UpsertShotAssetReference(project.UserID, project.ID, &model.ShotAssetReference{ID: "reference", ShotID: shot.ID, AssetVersionID: "version", Role: "reference", Status: "linked"})
				}
				return New(conn).CreateProjectAssetCandidates(project.UserID, project.ID, []model.ProjectAssetCandidate{{ID: "bound-candidate", ProjectID: project.ID, UnitID: unit.ID, ShotID: shot.ID, Name: "bound"}})
			}
			if err := db.Create(&model.AssetVersion{ID: "version", AssetID: "asset", Version: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ProjectAssetLink{ID: "link", ProjectID: project.ID, AssetID: "asset"}).Error; err != nil {
				t.Fatal(err)
			}
			replace := func(conn *gorm.DB) error {
				return conn.Transaction(func(tx *gorm.DB) error {
					if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model.Project{}, "id = ?", project.ID).Error; err != nil {
						return err
					}
					return replaceProjectUnitShotsTx(tx, project.ID, unit.ID, []model.Shot{{ID: "new-shot", ProjectID: project.ID, UnitID: unit.ID}})
				})
			}
			firstOp, secondOp := replace, bind
			if bindingFirst {
				firstOp, secondOp = bind, replace
			}
			first := db.Begin()
			if first.Error != nil {
				t.Fatal(first.Error)
			}
			defer first.Rollback()
			if err := firstOp(first); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- secondOp(second) }()
			deadline := time.Now().Add(5 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				if err := db.Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&blocked).Error; err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("second connection never waited for project lock")
			}
			if err := first.Commit().Error; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if bindingFirst && !errors.Is(err, ErrProjectShotsInUse) {
					t.Fatalf("want protected shots, got %v", err)
				}
				if !bindingFirst && !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("want stale shot rejection, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("second operation did not finish")
			}
			var orphans int64
			if err := db.Raw("SELECT count(*) FROM project_asset_candidates c LEFT JOIN shots s ON s.id=c.shot_id WHERE c.shot_id <> '' AND s.id IS NULL").Scan(&orphans).Error; err != nil {
				t.Fatal(err)
			}
			if orphans != 0 {
				t.Fatalf("orphan candidates: %d", orphans)
			}
			if err := db.Raw("SELECT count(*) FROM shot_asset_references r LEFT JOIN shots s ON s.id=r.shot_id WHERE s.id IS NULL").Scan(&orphans).Error; err != nil {
				t.Fatal(err)
			}
			if orphans != 0 {
				t.Fatalf("orphan references: %d", orphans)
			}
		})
	}
}
