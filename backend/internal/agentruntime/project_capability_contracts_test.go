package agentruntime_test

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestProjectApplyOpsArgumentsAndResultUseClosedRevisionContract(t *testing.T) {
	t.Parallel()

	argumentsPayload := json.RawMessage(`{
		"domainProjectId":"project-1",
		"baseRevision":4,
		"clientMutationId":"project-mutation-1",
		"operations":[
			{
				"operationId":"characters-1",
				"type":"create_character_candidates",
				"unitId":"unit-1",
				"candidates":[{
					"name":"林夏",
					"aliases":[],
					"role":"女主角",
					"appearance":"年轻女性，黑色长发",
					"clothing":"红色风衣",
					"physique":"身形修长",
					"personality":"克制、坚定",
					"props":"银色钥匙",
					"consistencyPrompt":"保持黑色长发和红色风衣",
					"multiViewPrompt":"保持正侧背面服装结构一致",
					"voiceLanguage":"普通话",
					"voiceAge":"青年",
					"voiceTimbre":"清亮、克制"
				}]
			},
			{
				"operationId":"shots-1",
				"type":"replace_unit_shots",
				"unitId":"unit-1",
				"shots":[{"title":"雨夜重逢","description":"中景，林夏站在旧车站门口，顾沉从路灯下走近。","durationMs":5000}]
			}
		]
	}`)
	decoded, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolProjectApplyOps, argumentsPayload)
	if err != nil {
		t.Fatalf("DecodeCapabilityArguments() error = %v", err)
	}
	arguments, ok := decoded.(agentruntime.ProjectApplyOpsArguments)
	if !ok || arguments.DomainProjectID != "project-1" || arguments.BaseRevision != 4 || len(arguments.Operations) != 2 {
		t.Fatalf("project arguments = %#v (%T)", decoded, decoded)
	}

	invalidArguments := []json.RawMessage{
		json.RawMessage(`{"domainProjectId":"project-1","baseRevision":4,"clientMutationId":"mutation-1","operations":[]}`),
		json.RawMessage(`{"domainProjectId":"project-1","baseRevision":4,"clientMutationId":"mutation-1","operations":[{"operationId":"shots-1","type":"replace_unit_shots","unitId":"unit-1","shots":[{"title":"镜头","description":"描述","durationMs":0}]}]}`),
		json.RawMessage(`{"domainProjectId":"project-1","baseRevision":4,"clientMutationId":"mutation-1","operations":[{"operationId":"characters-1","type":"create_character_candidates","unitId":"unit-1","candidates":[{"name":"林夏","aliases":[],"role":"女主角","appearance":"外貌","clothing":"服装","physique":"体型","personality":"性格","props":"","consistencyPrompt":"一致性","multiViewPrompt":"三视图","voiceLanguage":"普通话","voiceAge":"青年","voiceTimbre":"清亮","fallback":true}]}]}`),
	}
	for index, payload := range invalidArguments {
		if _, decodeErr := agentruntime.DecodeCapabilityArguments(agentruntime.ToolProjectApplyOps, payload); decodeErr == nil {
			t.Fatalf("invalid project arguments %d were accepted", index)
		}
	}

	resultPayload := json.RawMessage(`{"domainProjectId":"project-1","baseRevision":4,"committedRevision":5,"clientMutationId":"project-mutation-1","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["characters-1","shots-1"],"candidateIds":["candidate-1"],"shotIds":["shot-1"]}`)
	decodedResult, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, resultPayload)
	if err != nil {
		t.Fatalf("DecodeCapabilityResult() error = %v", err)
	}
	result, ok := decodedResult.(agentruntime.ProjectApplyOpsResult)
	if !ok || result.CommittedRevision != 5 || len(result.CandidateIDs) != 1 || len(result.ShotIDs) != 1 {
		t.Fatalf("project result = %#v (%T)", decodedResult, decodedResult)
	}

	if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolProjectApplyOps, json.RawMessage(`{"domainProjectId":"project-1","baseRevision":4,"committedRevision":6,"clientMutationId":"project-mutation-1","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["characters-1"],"candidateIds":[],"shotIds":[]}`)); err == nil {
		t.Fatal("jumped project revision result was accepted")
	}
}

func TestProjectDeliveryRequiresCurrentProjectRevision(t *testing.T) {
	t.Parallel()

	expected := agentruntime.ExpectedDelivery{
		Kind:               agentruntime.DeliveryProjectChange,
		TargetProjectID:    "project-1",
		CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactProjectRevision}},
	}
	if err := expected.Validate(); err != nil {
		t.Fatalf("ExpectedDelivery.Validate() error = %v", err)
	}

	satisfied := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{ProjectID: "project-1", ProjectRevision: 5, ProjectCurrent: true})
	if satisfied.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("project delivery = %#v, want satisfied", satisfied)
	}

	stale := agentruntime.VerifyDelivery(expected, agentruntime.DeliveryEvidence{ProjectID: "project-1", ProjectRevision: 5, ProjectCurrent: false})
	if stale.Status != agentruntime.VerificationRepairable {
		t.Fatalf("stale project delivery = %#v, want repairable", stale)
	}

	invalidMixed := []agentruntime.ExpectedDelivery{
		{Kind: agentruntime.DeliveryMixed, TargetCanvasID: "canvas-1", TargetProjectID: "project-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}}},
		{Kind: agentruntime.DeliveryMixed, TargetCanvasID: "canvas-1", CompletionCriteria: []agentruntime.DeliveryCriterion{{Fact: agentruntime.DeliveryFactCanvasRevision}, {Fact: agentruntime.DeliveryFactProjectRevision}}},
	}
	for index, candidate := range invalidMixed {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("mixed delivery with mismatched project target %d was accepted", index)
		}
	}
}
