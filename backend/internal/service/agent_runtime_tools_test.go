package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/database"
)

func TestVideoPromptSpecialistLoadsGovernedRuntimeContract(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	skills, err := svc.resolveAgentRuntimeSkillSelectionsForSpecialist(
		context.Background(),
		agentRuntimeServiceScope().ActorUserID,
		[]string{"video-prompt-specialist"},
		agentruntime.SpecialistVideoAssembly,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Dir != "video-prompt-specialist" || skills[0].Version != 1 ||
		skills[0].SourceKind != "original" || skills[0].SourceLicense != "HMaigc-Proprietary" ||
		len(skills[0].Checksum) != 64 || len(skills[0].CapabilityManifest.Specialists) != 1 ||
		skills[0].CapabilityManifest.Specialists[0] != agentruntime.SpecialistVideoAssembly ||
		len(skills[0].CapabilityManifest.Tools) != 1 || skills[0].CapabilityManifest.Tools[0] != agentruntime.ToolMediaGenerate ||
		len(skills[0].CapabilityManifest.ArtifactSchemas) != 1 ||
		skills[0].CapabilityManifest.ArtifactSchemas[0] != agentruntime.ArtifactSchemaVideoPlanV1 {
		t.Fatalf("loaded video prompt specialist facts = %#v", skills)
	}
}

func TestProductionSystemPromptPublishesStrictMediaGenerateContract(t *testing.T) {
	t.Parallel()

	requiredFacts := []string{
		`"expectedOutputSchema":"media_candidate.v1"`,
		`video_plan.v1`,
		`audio_plan.v1`,
		`assembly_plan.v2`,
		`"toolName":"skill.load|specialist.delegate|vision.analyze|media.generate|canvas.project|media.assemble"`,
		`"fact":"task_backed_resource"`,
		`"fact":"artifact_revision"`,
		`"fact":"resource"`,
		`"fact":"publication"`,
		`"parameters":{"prompt":"...","aspectRatio":"...","resolution":"...","quality":"...","count":1,"transparentBackground":false}`,
		`"parameters":{"prompt":"...","aspectRatio":"...","resolution":"...","durationSeconds":5,"generateAudio":false}`,
		`"directedRegeneration":{"sourceShotRevision":{"artifactId":"...","revisionId":"..."},"approvedCandidateRevision":{"artifactId":"...","revisionId":"..."}}`,
		`"parameters":{"prompt":"...","voice":"...","format":"...","speed":"...","volume":"...","pitch":"...","emotion":"...","languageBoost":"...","sampleRate":"...","bitrate":"...","channel":"...","instructions":"..."}`,
	}
	for _, fact := range requiredFacts {
		if !strings.Contains(agentRuntimeProductionSystemPrompt, fact) {
			t.Fatalf("production system prompt is missing media.generate contract %q", fact)
		}
	}
}

func TestProductionSystemPromptPublishesStrictSpecialistDelegateContract(t *testing.T) {
	t.Parallel()

	requiredFacts := []string{
		`"productionGraph":{"graphKey":"...","stages":[{"stageKey":"...","specialistKey":"narrative|asset|storyboard|visual|video_assembly|audio","dependsOnStageKeys":[],"inputRevisions":[],"expectedDelivery":{...},"reviewPolicy":"required","costPolicy":"none"}]}`,
		`"expectedGraphVersion":0`,
		`"stageKey":"..."`,
	}
	for _, fact := range requiredFacts {
		if !strings.Contains(agentRuntimeProductionSystemPrompt, fact) {
			t.Fatalf("production system prompt is missing specialist.delegate contract %q", fact)
		}
	}
}

func TestFreezeSpecialistDelegateRejectsUndeclaredTool(t *testing.T) {
	t.Parallel()

	request := specialistRuntimeRequestFixture("runtime-model-record", "runtime-model")
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionAutomatic,
		Skills:        request.LoadedSkills,
	}
	arguments, err := json.Marshal(SpecialistDelegateArguments{
		ProductionGraph:      productionGraphForSpecialistDelegate(request),
		ExpectedGraphVersion: 0,
		StageKey:             "visual-analysis",
		SpecialistKey:        request.SpecialistKey,
		Objective:            request.Objective,
		InputRevisions:       request.InputRevisions,
		SkillDirs:            []string{request.LoadedSkills[0].Dir},
		ToolAllowlist:        []agentruntime.AgentToolName{agentruntime.ToolMediaGenerate},
		ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery:     request.ExpectedDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = freezeSpecialistDelegateArguments(configuration, []string{request.LoadedSkills[0].Dir}, arguments)
	if !errors.Is(err, ErrSpecialistToolNotAllowed) {
		t.Fatalf("freeze error = %v, want %v", err, ErrSpecialistToolNotAllowed)
	}
}

func TestFreezeSpecialistDelegateReturnsExactPublishedSkillFacts(t *testing.T) {
	t.Parallel()

	request := specialistRuntimeRequestFixture("runtime-model-record", "runtime-model")
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionAutomatic,
		Skills:        request.LoadedSkills,
	}
	arguments, err := json.Marshal(SpecialistDelegateArguments{
		ProductionGraph:      productionGraphForSpecialistDelegate(request),
		ExpectedGraphVersion: 0,
		StageKey:             "visual-analysis",
		SpecialistKey:        request.SpecialistKey,
		Objective:            request.Objective,
		InputRevisions:       request.InputRevisions,
		SkillDirs:            []string{request.LoadedSkills[0].Dir},
		ToolAllowlist:        request.ToolAllowlist,
		ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery:     request.ExpectedDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}

	frozen, skills, err := freezeSpecialistDelegateArguments(configuration, []string{request.LoadedSkills[0].Dir}, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Checksum != request.LoadedSkills[0].Checksum || frozen.SpecialistKey != request.SpecialistKey {
		t.Fatalf("frozen delegate = %#v, skills = %#v", frozen, skills)
	}

	unknown := append(arguments[:len(arguments)-1], []byte(`,"unexpected":true}`)...)
	if _, _, err := freezeSpecialistDelegateArguments(configuration, []string{request.LoadedSkills[0].Dir}, unknown); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("unknown-field error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}
}

func TestFreezeAgentSpecialistDelegateDecisionRequiresExactDelivery(t *testing.T) {
	t.Parallel()

	request := specialistRuntimeRequestFixture("runtime-model-record", "runtime-model")
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionAutomatic,
		Skills:        request.LoadedSkills,
	}
	arguments, err := json.Marshal(SpecialistDelegateArguments{
		ProductionGraph:      productionGraphForSpecialistDelegate(request),
		ExpectedGraphVersion: 0,
		StageKey:             "visual-analysis",
		SpecialistKey:        request.SpecialistKey,
		Objective:            request.Objective,
		InputRevisions:       request.InputRevisions,
		SkillDirs:            []string{request.LoadedSkills[0].Dir},
		ToolAllowlist:        request.ToolAllowlist,
		ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery:     request.ExpectedDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: "delegate-exact-delivery", ToolName: agentruntime.ToolSpecialistDelegate, ActionVersion: 1,
		Arguments: arguments, ExpectedDelivery: request.ExpectedDelivery,
	}
	frozen, err := freezeAgentSpecialistDelegateDecisionArguments(configuration, []string{request.LoadedSkills[0].Dir}, call)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(frozen) {
		t.Fatalf("frozen arguments are invalid JSON: %s", frozen)
	}

	call.ExpectedDelivery = agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryCanvasChange,
		TargetCanvasID:     "runtime-canvas",
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
	if _, err := freezeAgentSpecialistDelegateDecisionArguments(configuration, []string{request.LoadedSkills[0].Dir}, call); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("delivery conflict error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}
}

func TestFreezeSpecialistDelegateRequiresAlignedProductionGraphStage(t *testing.T) {
	t.Parallel()

	request := specialistRuntimeRequestFixture("runtime-model-record", "runtime-model")
	configuration := agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionAutomatic,
		Skills:        request.LoadedSkills,
	}
	base := SpecialistDelegateArguments{
		ProductionGraph:      productionGraphForSpecialistDelegate(request),
		ExpectedGraphVersion: 0,
		StageKey:             "visual-analysis",
		SpecialistKey:        request.SpecialistKey,
		Objective:            request.Objective,
		InputRevisions:       request.InputRevisions,
		SkillDirs:            []string{request.LoadedSkills[0].Dir},
		ToolAllowlist:        request.ToolAllowlist,
		ExpectedOutputSchema: request.ExpectedOutputSchema,
		ExpectedDelivery:     request.ExpectedDelivery,
	}

	missingGraph := base
	missingGraph.ProductionGraph = agentruntime.ProductionGraphDraft{}
	encodedMissing, err := json.Marshal(missingGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := freezeSpecialistDelegateArguments(configuration, []string{request.LoadedSkills[0].Dir}, encodedMissing); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("missing graph error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}

	mismatchedStage := base
	mismatchedStage.ProductionGraph.Stages[0].ExpectedDelivery = agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryCanvasChange,
		TargetCanvasID:     "runtime-canvas",
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}},
	}
	encodedMismatch, err := json.Marshal(mismatchedStage)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := freezeSpecialistDelegateArguments(configuration, []string{request.LoadedSkills[0].Dir}, encodedMismatch); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
		t.Fatalf("mismatched stage error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
	}
}

func productionGraphForSpecialistDelegate(request agentruntime.SpecialistRequest) agentruntime.ProductionGraphDraft {
	return agentruntime.ProductionGraphDraft{
		GraphKey: "specialist-test-graph",
		Stages: []agentruntime.ProductionStageDraft{{
			StageKey: "visual-analysis", SpecialistKey: request.SpecialistKey,
			DependsOnStageKeys: []string{}, InputRevisions: request.InputRevisions,
			ExpectedDelivery: request.ExpectedDelivery, ReviewPolicy: agentruntime.ReviewRequired, CostPolicy: agentruntime.CostNone,
		}},
	}
}

func TestDecodeProductionToolArgumentsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	answer := `"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}`
	tests := []struct {
		name   string
		decode func([]byte) error
		input  string
	}{
		{
			name: "vision",
			decode: func(payload []byte) error {
				_, err := decodeVisionAnalyzeArguments(payload)
				return err
			},
			input: `{"inputRevisions":[],"resourceIds":["resource-1"],"expectedOutputSchema":"visual_evidence.v1",` + answer + `,"unexpected":true}`,
		},
		{
			name: "media",
			decode: func(payload []byte) error {
				_, err := decodeMediaGenerateArguments(payload)
				return err
			},
			input: `{"inputRevisions":[],"generationModel":{"channelId":"channel-1","model":"image-model"},"capability":"image","parameters":{"size":"1:1"},"outputArtifactKey":"portrait","expectedOutputSchema":"generated_image.v1",` + answer + `,"unexpected":true}`,
		},
		{
			name: "canvas",
			decode: func(payload []byte) error {
				_, err := decodeCanvasProjectArguments(payload)
				return err
			},
			input: `{"artifactRevisions":[{"artifactId":"artifact-1","revisionId":"revision-1"}],"baseRevision":7,` + answer + `,"unexpected":true}`,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if err := testCase.decode([]byte(testCase.input)); !errors.Is(err, ErrAgentRuntimeToolArgumentsInvalid) {
				t.Fatalf("decode error = %v, want %v", err, ErrAgentRuntimeToolArgumentsInvalid)
			}
		})
	}
}
