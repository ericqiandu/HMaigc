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
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeImageModel(t, db, fixture)
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
	if canvas.DomainProjectID != "runtime-project" || len(canvas.CallableModels) != 1 {
		t.Fatalf("canvas execution facts = %#v", canvas)
	}
	callableModel := canvas.CallableModels[0]
	if callableModel.ChannelID != "runtime-image-channel" || callableModel.ModelRecordID != "runtime-image-model" ||
		callableModel.ModelKey != "kz_gpt_image2" || callableModel.Capability != "image" ||
		callableModel.UnitPriceMicrocredits != 250 || callableModel.ProviderCapabilities == nil {
		t.Fatalf("canvas callable model facts = %#v", callableModel)
	}
	if strings.Contains(string(result.Output), "must-not-leak") || strings.Contains(string(result.Output), "chatSessions") || strings.Contains(string(result.Output), "privateDraft") {
		t.Fatalf("canvas read leaked unrelated payload: %s", result.Output)
	}
}

func TestAgentCapabilityReadCanvasSupportsStandaloneCanvas(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").
		Select("project_id", "payload_json").
		Updates(model.CanvasProject{
			ProjectID:   "",
			PayloadJSON: `{"nodes":[],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	scope := agentRuntimeServiceScope()
	scope.DomainProjectID = ""
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), scope, agentruntime.ToolCallDecision{
		ToolCallID: "read-standalone-canvas", ToolName: agentruntime.ToolCanvasRead, ActionVersion: 1,
		Arguments: json.RawMessage(`{"canvasId":"runtime-canvas","selectedNodeIds":[],"includeViewport":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasRead, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	canvas := decoded.(agentruntime.CanvasReadResult)
	if canvas.CanvasID != "runtime-canvas" || canvas.DomainProjectID != "" || canvas.Revision != 7 || len(canvas.Nodes) != 0 || len(canvas.Edges) != 0 {
		t.Fatalf("standalone canvas read result = %#v", canvas)
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

func TestAgentCapabilityReadProjectReturnsCanvasLinkedUnitShotsAndConfirmedCharacterEvidence(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	now := time.Now().UTC()
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), agentRuntimeServiceScope(), guidedAgentRuntimeConfigurationInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CreateInitializedAgentRun(repository.CreateInitializedAgentRunInput{
		Create: repository.CreateAgentRunInput{Scope: agentRuntimeServiceScope(), ClientRequestID: "project-read-run", Now: now},
		Initialize: repository.InitializeAgentRunInput{
			Scope: agentRuntimeServiceScope(), ModelRecordID: fixture.channelModel.ID, ModelKey: fixture.channelModel.ModelKey,
			MaxSteps: 24, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
			PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "读取项目事实", Configuration: configuration, Now: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Project{}).Where("id = ?", "runtime-project").Update("aspect_ratio", "9:16").Error; err != nil {
		t.Fatal(err)
	}
	unit := model.ProjectUnit{
		ID: "runtime-unit", ProjectID: "runtime-project", Kind: model.ProjectUnitKindChapter,
		Title: "雨夜归来", SourceText: "林夏在雨夜回到旧车站。", Status: model.ProjectUnitStatusReady,
		Position: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CanvasUnitLink{ID: "runtime-canvas-unit", ProjectID: "runtime-project", CanvasID: "runtime-canvas", UnitID: unit.ID, Role: "storyboard", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	shots := []model.Shot{
		{ID: "shot-1", ProjectID: "runtime-project", UnitID: unit.ID, Title: "雨夜车站", Description: "林夏站在雨幕中的旧车站。", Position: 0, DurationMs: 5000, Status: "draft", CreatedAt: now, UpdatedAt: now},
		{ID: "shot-2", ProjectID: "runtime-project", UnitID: unit.ID, Title: "纸船入水", Description: "林夏将白色纸船放入积水。", Position: 1, DurationMs: 4000, Status: "draft", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&shots).Error; err != nil {
		t.Fatal(err)
	}
	character := model.Asset{ID: "character-linxia", UserID: "runtime-user", Kind: "entity", Category: model.AssetCategoryCharacter, Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: "character-linxia-v3", Title: "林夏", CreatedAt: now, UpdatedAt: now}
	version := model.AssetVersion{ID: "character-linxia-v3", AssetID: character.ID, Version: 3, Status: model.AssetVersionStatusConfirmed, DefinitionJSON: `{"appearance":"短黑发，黄色雨衣","consistencyPrompt":"保持短黑发、黄色雨衣和白色纸船"}`, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Resource{ID: "resource-linxia-three-view", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 2048, Height: 1024, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AssetRepresentation{ID: "representation-linxia-three-view", AssetVersionID: version.ID, ResourceID: "resource-linxia-three-view", MediaType: "image", Role: "three_view", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ProjectAssetLink{ID: "project-character-linxia", ProjectID: "runtime-project", AssetID: character.ID, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
		ToolCallID: "read-project", ToolName: agentruntime.ToolProjectRead, ActionVersion: 1,
		Arguments: json.RawMessage(`{"domainProjectId":"runtime-project","canvasId":"runtime-canvas"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectRead, execution.Output)
	if err != nil {
		t.Fatal(err)
	}
	project := decoded.(agentruntime.ProjectReadResult)
	if project.DomainProjectID != "runtime-project" || project.Revision != 1 || project.CanvasID != "runtime-canvas" || len(project.Units) != 1 {
		t.Fatalf("project facts = %#v", project)
	}
	if project.Units[0].UnitID != unit.ID || project.Units[0].SourceText != unit.SourceText || len(project.Units[0].Shots) != 2 || project.Units[0].Shots[1].ShotID != "shot-2" {
		t.Fatalf("linked unit facts = %#v", project.Units)
	}
	if len(project.Characters) != 1 || project.Characters[0].AssetID != character.ID || project.Characters[0].VersionID != version.ID || project.Characters[0].Version != 3 || len(project.Characters[0].Representations) != 1 || project.Characters[0].Representations[0].ResourceID != "resource-linxia-three-view" {
		t.Fatalf("character evidence = %#v", project.Characters)
	}
	if err := db.Create(&model.AgentToolCall{
		ID: "project-read-record", RunID: "runtime-run", ToolCallID: "read-project", ActionVersion: 1,
		ToolName: string(agentruntime.ToolProjectRead), Status: agentruntime.ToolCallSucceeded,
		RiskLevel: agentruntime.ToolRiskRead, RequiredAccess: agentruntime.AccessViewer,
		InputJSON:  `{"domainProjectId":"runtime-project","canvasId":"runtime-canvas"}`,
		OutputJSON: string(execution.Output), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), ""); err != nil {
		t.Fatalf("project.read must be neutral delivery evidence: %v", err)
	}
	for name, arguments := range map[string]string{
		"other project": `{"domainProjectId":"other-project","canvasId":"runtime-canvas"}`,
		"other canvas":  `{"domainProjectId":"runtime-project","canvasId":"other-canvas"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, readErr := registry.Execute(context.Background(), agentRuntimeServiceScope(), agentruntime.ToolCallDecision{
				ToolCallID: "read-project-cross-scope", ToolName: agentruntime.ToolProjectRead, ActionVersion: 1,
				Arguments: json.RawMessage(arguments),
			})
			if readErr == nil {
				t.Fatal("cross-scope project read was not rejected")
			}
		})
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
	configuration, err := svc.resolveAgentRuntimeConfiguration(context.Background(), agentRuntimeServiceScope(), AgentRuntimeConfigurationInput{
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
