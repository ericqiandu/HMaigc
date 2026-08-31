package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestAgentCapabilityReadCanvasReturnsOnlyScopedFacts(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	payload := `{"nodes":[{"id":"node-1","type":"image","data":{"prompt":"visible"}}],"connections":[{"id":"edge-1","source":"node-1","target":"node-2"}],"viewport":{"x":12,"y":-4,"k":1.25},"chatSessions":[{"secret":"must-not-leak"}],"privateDraft":"must-not-leak"}`
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
		ToolCallID: "read-canvas", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
		Arguments: json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":["node-1"],"includeViewport":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasRead, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	canvas := decoded.(agentruntime.CanvasReadResult)
	if canvas.CanvasID != "runtime-canvas" || canvas.Revision != 7 || len(canvas.Nodes) != 1 || len(canvas.Edges) != 1 || len(canvas.SelectedNodeIDs) != 1 || canvas.SelectedNodeIDs[0] != "node-1" || canvas.Viewport.X != 12 || canvas.Viewport.Y != -4 || canvas.Viewport.Zoom != 1.25 {
		t.Fatalf("canvas read result = %#v", canvas)
	}
	if strings.Contains(string(result.Output), "must-not-leak") || strings.Contains(string(result.Output), "chatSessions") || strings.Contains(string(result.Output), "privateDraft") {
		t.Fatalf("canvas read leaked unrelated payload: %s", result.Output)
	}
}

func TestAgentCapabilityReadCanvasRejectsStaleScopeAndSelection(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("payload_json", `{"nodes":[{"id":"node-1"}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	call := agentruntime.ToolCallDecision{ToolCallID: "read-canvas", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1, Arguments: json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":["missing"],"includeViewport":true}`)}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); err == nil {
		t.Fatal("stale canvas selection was accepted")
	}
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = "other-project"
	call.Arguments = json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`)
	if _, err := registry.Execute(context.Background(), scope, call); err == nil {
		t.Fatal("stale project scope was accepted")
	}
	call.Arguments = json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":false}`)
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); err == nil {
		t.Fatal("canvas read without viewport was accepted")
	}
}

func TestAgentCapabilityReadAssetsReturnsOnlyProjectOwnedResources(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	now := time.Now().UTC()
	createCapabilityAsset := func(assetID string, versionID string, representationID string, resourceID string, projectID string, ownerID string) {
		t.Helper()
		if err := db.Create(&model.Asset{ID: assetID, UserID: ownerID, Kind: "image", Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: versionID, Title: "角色立绘", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: model.AssetVersionStatusConfirmed, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.Resource{ID: resourceID, UserID: ownerID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 1024, Height: 768, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AssetRepresentation{ID: representationID, TaskID: "task-" + representationID, AssetVersionID: versionID, ResourceID: resourceID, MediaType: "image", Role: "primary", CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ProjectAssetLink{ID: "link-" + assetID, ProjectID: projectID, AssetID: assetID, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	createCapabilityAsset("asset-owned", "version-owned", "representation-owned", "resource-owned", "runtime-project", "runtime-user")
	if err := db.Create(&model.Project{ID: "other-project", UserID: "other-user", Name: "Other", Type: "short-drama", Status: model.ProjectStatusActive, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	createCapabilityAsset("asset-cross", "version-cross", "representation-cross", "resource-cross", "other-project", "other-user")

	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
		ToolCallID: "read-assets", ToolName: agentruntime.ToolAssetsRead, ActionVersion: 1,
		Arguments: json.RawMessage(`{"domainProjectId":"runtime-project","resourceIds":["resource-owned"],"limit":10}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsRead, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	assets := decoded.(agentruntime.AssetsReadResult)
	if assets.DomainProjectID != "runtime-project" || len(assets.Resources) != 1 || assets.Resources[0].ResourceID != "resource-owned" || assets.Resources[0].Name != "角色立绘" || assets.Resources[0].Kind != agentruntime.MediaKindImage {
		t.Fatalf("assets read result = %#v", assets)
	}

	crossScopeCall := agentruntime.ToolCallDecision{ToolCallID: "read-assets-cross", ToolName: agentruntime.ToolAssetsRead, ActionVersion: 1, Arguments: json.RawMessage(`{"domainProjectId":"runtime-project","resourceIds":["resource-cross"],"limit":10}`)}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), crossScopeCall); err == nil {
		t.Fatal("cross-project resource was not rejected")
	}
}

func TestAgentCapabilitySkillLoadRequiresFrozenPublishedVersion(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), agentRuntimeServiceScope().ActorUserID, AgentRuntimeConfigurationInput{
		SkillDirs: []string{"short-drama-director"}, ExecutionMode: agentruntime.ExecutionAutomatic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Skills) != 1 {
		t.Fatalf("resolved skills = %#v", configuration.Skills)
	}
	now := time.Now().UTC()
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "skill-load-run", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: agentRuntimeServiceScope(), ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 24, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "加载已选技能", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	selected := configuration.Skills[0]
	arguments, err := json.Marshal(agentruntime.SkillsLoadArguments{SkillDir: selected.Dir, Version: selected.Version, Checksum: selected.Checksum})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
		ToolCallID: "load-skill", ToolName: agentruntime.ToolSkillsLoad, ActionVersion: 1, Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolSkillsLoad, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	loaded := decoded.(agentruntime.SkillsLoadResult)
	if loaded.SkillDir != selected.Dir || loaded.Version != selected.Version || loaded.Checksum != selected.Checksum || loaded.Instructions != selected.Instructions {
		t.Fatalf("loaded skill = %#v; selected = %#v", loaded, selected)
	}

	arguments, err = json.Marshal(agentruntime.SkillsLoadArguments{SkillDir: selected.Dir, Version: selected.Version, Checksum: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
		ToolCallID: "load-stale-skill", ToolName: agentruntime.ToolSkillsLoad, ActionVersion: 1, Arguments: arguments,
	}); err == nil {
		t.Fatal("stale skill checksum was accepted")
	}
}
