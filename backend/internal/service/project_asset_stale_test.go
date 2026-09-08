package service

import (
	"infinite-canvas/backend/internal/model"
	"testing"
)

func TestProjectAssetRepositoryRejectsMissingShot(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	ref := model.ShotAssetReference{ID: "orphan", ShotID: "deleted-shot", AssetVersionID: "version", Role: "reference"}
	if err := svc.repo.UpsertShotAssetReference(agentRuntimeServiceScope().ActorUserID, "runtime-project", &ref); err == nil {
		t.Fatal("missing shot accepted")
	}
	var count int64
	if err := db.Model(&model.ShotAssetReference{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("orphan reference persisted")
	}
}

func TestProjectCandidateRepositoryRejectsMissingShotAtomically(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	err := svc.repo.CreateProjectAssetCandidates(agentRuntimeServiceScope().ActorUserID, "runtime-project", []model.ProjectAssetCandidate{
		{ID: "valid", ProjectID: "runtime-project", Name: "valid"},
		{ID: "orphan", ProjectID: "runtime-project", ShotID: "deleted-shot", Name: "invalid"},
	})
	if err == nil {
		t.Fatal("missing shot accepted")
	}
	var count int64
	if err := db.Model(&model.ProjectAssetCandidate{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("partial candidates persisted")
	}
	var project model.Project
	if err := db.First(&project, "id = ?", "runtime-project").Error; err != nil {
		t.Fatal(err)
	}
	if project.Revision != 1 {
		t.Fatalf("revision changed: %d", project.Revision)
	}
}
