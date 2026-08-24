package service

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestProductionStoryboardTaskInputUsesSucceededReferenceAssets(t *testing.T) {
	decisionServer, _ := newAgentRuntimeDecisionServer(t, `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	defer decisionServer.Close()
	svc, db, _ := newAgentRuntimeServiceFixture(t, decisionServer.URL)
	createAgentRuntimeCanvas(t, db)
	scope := agentRuntimeServiceScope()
	started, err := svc.StartAgentRuntime(StartAgentRuntimeInput{
		Scope: scope, ClientRequestID: "reference-input", UserMessage: "生成一致分镜", MaxSteps: 6,
		Configuration: AgentRuntimeConfigurationInput{ExecutionMode: agentruntime.ExecutionAutomatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft := agentruntime.ProductionPlanDraft{
		Title: "雨夜怀表", TargetDurationMS: 6_000, Script: "顾棠拨动怀表。",
		References: []agentruntime.ReferenceAssetDraft{
			{ReferenceKey: "hero", Role: "character", Title: "顾棠", ImagePrompt: "顾棠角色参考图"},
			{ReferenceKey: "watch", Role: "prop", Title: "月牙怀表", ImagePrompt: "月牙怀表参考图"},
		},
		Shots: []agentruntime.ShotPlanDraft{{
			ShotKey: "shot-1", Order: 1, DurationMS: 6_000, ScriptText: "顾棠拿起怀表。",
			Deliverables: agentRuntimeDualProductionDeliverables(),
			ImagePrompt: "顾棠拿起月牙怀表", VideoPrompt: "镜头推进",
			ReferenceKeys: []string{"hero", "watch"}, Dependencies: []string{},
		}},
	}
	record, err := svc.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: started.Run.ID, PlanKey: "reference-input-plan", BaseVersion: 0, Draft: draft, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	resources := map[string]model.Resource{
		"hero":  {ID: "reference-hero", UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"},
		"watch": {ID: "reference-watch", UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"},
	}
	for key, resource := range resources {
		if err := db.Create(&resource).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.AgentProductionArtifact{}).
			Where("plan_version_id = ? AND reference_key = ? AND kind = ?", record.Plan.ID, key, model.AgentProductionArtifactReferenceImage).
			Select("status", "resource_id").
			Updates(model.AgentProductionArtifact{Status: model.AgentProductionArtifactSucceeded, ResourceID: resource.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var storyboard model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactStoryboardImage {
			storyboard = artifact
			break
		}
	}
	input, taskType, err := svc.productionRenderTaskInput(scope, agentruntime.ProductionRenderArguments{
		PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version,
		ImageConfig: &agentruntime.ImageRenderConfig{Size: "9:16", Count: 1},
	}, storyboard, "顾棠拿起月牙怀表")
	if err != nil {
		t.Fatal(err)
	}
	if taskType != "canvas_image" || len(input.ReferenceImages) != 2 ||
		input.ReferenceImages[0].ID != "reference-hero" || input.ReferenceImages[1].ID != "reference-watch" {
		t.Fatalf("storyboard task input = %#v", input)
	}
}
