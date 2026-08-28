package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestProductionSystemPromptPublishesStrictMediaGenerateContract(t *testing.T) {
	t.Parallel()

	requiredFacts := []string{
		`"expectedOutputSchema":"media_candidate.v1"`,
		`video_plan.v1`,
		`audio_plan.v1`,
		`assembly_plan.v1`,
		`"fact":"artifact_revision"`,
		`"fact":"resource"`,
		`"fact":"publication"`,
		`"parameters":{"prompt":"...","aspectRatio":"...","resolution":"...","quality":"...","count":1,"transparentBackground":false}`,
		`"parameters":{"prompt":"...","aspectRatio":"...","resolution":"...","durationSeconds":5,"generateAudio":false}`,
		`"parameters":{"prompt":"...","voice":"...","format":"...","speed":"...","volume":"...","pitch":"...","emotion":"...","languageBoost":"...","sampleRate":"...","bitrate":"...","channel":"...","instructions":"..."}`,
	}
	for _, fact := range requiredFacts {
		if !strings.Contains(agentRuntimeProductionSystemPrompt, fact) {
			t.Fatalf("production system prompt is missing media.generate contract %q", fact)
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
