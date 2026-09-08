package service

import (
	"context"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestAgentProjectReplacementPreservesAssetAssociations(t *testing.T) {
	for _, kind := range []string{"reference", "candidate"} {
		t.Run(kind, func(t *testing.T) {
			svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
			seedAgentProjectUnit(t, db)
			shot := model.Shot{ID: "protected-shot", ProjectID: "runtime-project", UnitID: "runtime-unit", Title: "original", Status: "draft"}
			if err := db.Create(&shot).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "reference" {
				if err := db.Create(&model.ShotAssetReference{ID: "reference", ShotID: shot.ID, AssetVersionID: "asset-version", Role: "character"}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Create(&model.ProjectAssetCandidate{ID: "candidate", ProjectID: shot.ProjectID, UnitID: shot.UnitID, ShotID: shot.ID, Name: "original candidate"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			call := agentProjectApplyOpsCall(`{"domainProjectId":"runtime-project","baseRevision":1,"clientMutationId":"protected-replacement","operations":[{"operationId":"shots","type":"replace_unit_shots","unitId":"runtime-unit","shots":[{"title":"replacement","description":"must not replace linked shot","durationMs":3000}]}]}`)
			seedApprovedAgentCanvasProposal(t, svc, db, call)
			registry, err := newAgentCapabilityRegistry(svc)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "project_shots_in_use") {
					t.Fatalf("want project_shots_in_use, got %v", err)
				}
			}
			var shots []model.Shot
			if err := db.Where("project_id = ?", shot.ProjectID).Find(&shots).Error; err != nil {
				t.Fatal(err)
			}
			if len(shots) != 1 || shots[0].ID != shot.ID || shots[0].Title != "original" {
				t.Fatalf("shots changed: %#v", shots)
			}
			var count int64
			if kind == "reference" {
				err = db.Model(&model.ShotAssetReference{}).Where("id = ?", "reference").Count(&count).Error
			} else {
				err = db.Model(&model.ProjectAssetCandidate{}).Where("id = ?", "candidate").Count(&count).Error
			}
			if err != nil || count != 1 {
				t.Fatalf("association lost: count=%d err=%v", count, err)
			}
			var project model.Project
			if err := db.First(&project, "id = ?", shot.ProjectID).Error; err != nil {
				t.Fatal(err)
			}
			if project.Revision != 1 {
				t.Fatalf("revision changed: %d", project.Revision)
			}
			var record model.AgentToolCall
			if err := db.Where("run_id = ? AND tool_call_id = ?", "runtime-run", call.ToolCallID).First(&record).Error; err != nil {
				t.Fatal(err)
			}
			if record.OutputJSON != "{}" {
				t.Fatalf("failure committed receipt: %s", record.OutputJSON)
			}
		})
	}
}
