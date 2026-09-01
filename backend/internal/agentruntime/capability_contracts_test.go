package agentruntime_test

import (
	"encoding/json"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func validCapabilityArgumentsForTest(tool agentruntime.ToolName) json.RawMessage {
	switch tool {
	case agentruntime.ToolCanvasRead:
		return json.RawMessage(`{"canvasId":"canvas-1","selectedNodeIds":[],"includeViewport":true}`)
	case agentruntime.ToolCanvasApplyOps:
		return json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[{"operationId":"op-1","type":"select_nodes","nodeIds":[]}]}`)
	case agentruntime.ToolAssetsRead:
		return json.RawMessage(`{"domainProjectId":"project-1","resourceIds":[],"limit":20}`)
	case agentruntime.ToolAssetsPublish:
		return json.RawMessage(`{"resourceId":"resource-1","domainProjectId":"project-1","displayName":"资产","clientMutationId":"publish-1"}`)
	case agentruntime.ToolMediaGenerate:
		return json.RawMessage(`{"mediaKind":"image","modelRecordId":"model-record-1","modelKey":"image-model","parameters":{"prompt":"主视觉"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"media-1"}`)
	case agentruntime.ToolSkillsLoad:
		return json.RawMessage(`{"skillDir":"storyboard-director","version":7,"checksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
	default:
		panic("test capability tool is unsupported")
	}
}

func TestCapabilityArgumentsDecodeExactCurrentContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tool    agentruntime.ToolName
		payload string
	}{
		{
			name:    "canvas read",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"canvas-1","selectedNodeIds":["node-1"],"includeViewport":true}`,
		},
		{
			name:    "canvas read without a selection",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"canvas-1","selectedNodeIds":[],"includeViewport":false}`,
		},
		{
			name: "canvas apply operations",
			tool: agentruntime.ToolCanvasApplyOps,
			payload: `{"canvasId":"canvas-1","baseRevision":3,"clientMutationId":"mutation-1","operations":[` +
				`{"operationId":"op-1","type":"add_node","node":{"id":"node-2","type":"image","title":"主视觉","position":{"x":12,"y":24},"width":480,"height":320,"metadata":{"prompt":"雨夜"}}},` +
				`{"operationId":"op-2","type":"connect_nodes","connection":{"id":"edge-1","fromNodeId":"node-1","toNodeId":"node-2"}}]}`,
		},
		{
			name:    "assets read",
			tool:    agentruntime.ToolAssetsRead,
			payload: `{"domainProjectId":"project-1","resourceIds":["resource-1"],"limit":20}`,
		},
		{
			name:    "assets publish",
			tool:    agentruntime.ToolAssetsPublish,
			payload: `{"resourceId":"resource-1","domainProjectId":"project-1","displayName":"角色立绘","clientMutationId":"publish-1"}`,
		},
		{
			name: "media generate",
			tool: agentruntime.ToolMediaGenerate,
			payload: `{"mediaKind":"video","modelRecordId":"model-record-1","modelKey":"seedance-2.0",` +
				`"parameters":{"prompt":"雨夜追逐","durationSeconds":5},"sourceResourceIds":["resource-1"],` +
				`"targetCanvasNodeId":"node-2","clientRequestId":"media-1"}`,
		},
		{
			name:    "skills load",
			tool:    agentruntime.ToolSkillsLoad,
			payload: `{"skillDir":"skills/storyboard","version":2,"checksum":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			arguments, err := agentruntime.DecodeCapabilityArguments(testCase.tool, json.RawMessage(testCase.payload))
			if err != nil {
				t.Fatalf("DecodeCapabilityArguments() error = %v", err)
			}
			if arguments == nil {
				t.Fatal("DecodeCapabilityArguments() returned nil")
			}
		})
	}
}

func TestCapabilityArgumentsRoundTripPreservesExplicitEmptyCollections(t *testing.T) {
	t.Parallel()

	for _, tool := range []agentruntime.ToolName{
		agentruntime.ToolCanvasRead,
		agentruntime.ToolCanvasApplyOps,
	} {
		arguments, err := agentruntime.DecodeCapabilityArguments(tool, validCapabilityArgumentsForTest(tool))
		if err != nil {
			t.Fatalf("DecodeCapabilityArguments(%q) error = %v", tool, err)
		}

		encoded, err := json.Marshal(arguments)
		if err != nil {
			t.Fatalf("json.Marshal(%q) error = %v", tool, err)
		}
		if _, err := agentruntime.DecodeCapabilityArguments(tool, encoded); err != nil {
			t.Fatalf("round-trip DecodeCapabilityArguments(%q) error = %v; payload = %s", tool, err, encoded)
		}
	}
}

func TestCapabilityArgumentsRejectUnknownFieldsAndInvalidIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tool    agentruntime.ToolName
		payload string
	}{
		{
			name:    "unknown field",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"canvas-1","selectedNodeIds":[],"includeViewport":true,"fallback":true}`,
		},
		{
			name:    "missing explicit boolean",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"canvas-1","selectedNodeIds":[]}`,
		},
		{
			name:    "missing explicit selection",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"canvas-1","includeViewport":false}`,
		},
		{
			name:    "empty identifier",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":" ","selectedNodeIds":[],"includeViewport":false}`,
		},
		{
			name:    "oversized identifier",
			tool:    agentruntime.ToolCanvasRead,
			payload: `{"canvasId":"` + strings.Repeat("a", 121) + `","selectedNodeIds":[],"includeViewport":false}`,
		},
		{
			name:    "assets read limit smaller than explicit resource set",
			tool:    agentruntime.ToolAssetsRead,
			payload: `{"domainProjectId":"project-1","resourceIds":["resource-1","resource-2"],"limit":1}`,
		},
		{
			name:    "retired tool",
			tool:    agentruntime.ToolCanvasProject,
			payload: `{}`,
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := agentruntime.DecodeCapabilityArguments(testCase.tool, json.RawMessage(testCase.payload)); err == nil {
				t.Fatal("DecodeCapabilityArguments() error = nil, want rejection")
			}
		})
	}
}

func TestCapabilityCanvasOperationsRejectDuplicateAndInvalidStructures(t *testing.T) {
	t.Parallel()

	duplicate := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[` +
		`{"operationId":"op-1","type":"delete_node","nodeId":"node-1"},` +
		`{"operationId":"op-1","type":"delete_node","nodeId":"node-2"}]}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, duplicate); err == nil {
		t.Fatal("duplicate operation IDs were accepted")
	}

	invalidConnection := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[` +
		`{"operationId":"op-1","type":"connect_nodes","connection":{"id":"edge-1","fromNodeId":"node-1"}}]}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, invalidConnection); err == nil {
		t.Fatal("invalid connection structure was accepted")
	}

	generationMixedIntoCanvas := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[` +
		`{"operationId":"op-1","type":"run_generation","nodeId":"node-1"}]}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, generationMixedIntoCanvas); err == nil {
		t.Fatal("paid generation was accepted as a canvas operation")
	}
}

func TestCapabilityMediaGenerationUsesAuthoritativeCapabilityFacts(t *testing.T) {
	t.Parallel()

	decoded, err := agentruntime.DecodeCapabilityArguments(
		agentruntime.ToolMediaGenerate,
		json.RawMessage(`{"mediaKind":"video","modelRecordId":"model-record-1","modelKey":"seedance-2.0","parameters":{"prompt":"雨夜追逐"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"request-1"}`),
	)
	if err != nil {
		t.Fatalf("DecodeCapabilityArguments() error = %v", err)
	}
	arguments, ok := decoded.(agentruntime.MediaGenerateArguments)
	if !ok {
		t.Fatalf("DecodeCapabilityArguments() type = %T", decoded)
	}
	if err := agentruntime.ValidateMediaGenerateModelCapability(arguments, "image"); err == nil {
		t.Fatal("model capability mismatch was accepted")
	}
	if err := agentruntime.ValidateMediaGenerateModelCapability(arguments, "video"); err != nil {
		t.Fatalf("matching model capability rejected: %v", err)
	}

	clientCommercialFacts := json.RawMessage(`{"mediaKind":"video","modelRecordId":"model-record-1","modelKey":"seedance-2.0","parameters":{"prompt":"雨夜追逐"},"sourceResourceIds":[],"targetCanvasNodeId":"node-1","clientRequestId":"request-1","price":10,"ownerId":"user-1","finalUrl":"https://example.com/final.mp4","taskStatus":"succeeded","billingStatus":"paid"}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolMediaGenerate, clientCommercialFacts); err == nil {
		t.Fatal("client-supplied commercial or result facts were accepted")
	}
}

func TestCapabilityAssetsPublishAcceptsResourceIdentityOnly(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"resourceId":"resource-1","domainProjectId":"project-1","displayName":"角色立绘","clientMutationId":"publish-1"}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolAssetsPublish, valid); err != nil {
		t.Fatalf("authoritative resource identity rejected: %v", err)
	}

	for _, payload := range []json.RawMessage{
		json.RawMessage(`{"resourceId":"https://example.com/image.png","domainProjectId":"project-1","displayName":"角色立绘","clientMutationId":"publish-1"}`),
		json.RawMessage(`{"resourceId":"urn:asset:1","domainProjectId":"project-1","displayName":"角色立绘","clientMutationId":"publish-1"}`),
		json.RawMessage(`{"resourceId":"resource-1","resourceUrl":"https://example.com/image.png","domainProjectId":"project-1","displayName":"角色立绘","clientMutationId":"publish-1"}`),
	} {
		if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolAssetsPublish, payload); err == nil {
			t.Fatal("URL-based asset publication was accepted")
		}
	}
}

func TestCapabilityResultsUseStrictVersionedSchemas(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"taskId":"task-1","mediaKind":"video","clientRequestId":"request-1"}`)
	result, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, valid)
	if err != nil {
		t.Fatalf("DecodeCapabilityResult() error = %v", err)
	}
	if _, ok := result.(agentruntime.MediaGenerateResult); !ok {
		t.Fatalf("DecodeCapabilityResult() type = %T", result)
	}

	unknown := json.RawMessage(`{"taskId":"task-1","mediaKind":"video","clientRequestId":"request-1","url":"https://example.com/final.mp4"}`)
	if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, unknown); err == nil {
		t.Fatal("unknown result field was accepted")
	}

	canvasResult := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":7,"committedRevision":8,"clientMutationId":"mutation-1","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["operation-1"],"evidence":{"addedNodeIds":["node-1"],"updatedNodeIds":[],"deletedNodeIds":[],"upsertedConnectionIds":[],"deletedConnectionIds":[],"selectedNodeIds":["node-1"],"viewportApplied":false}}`)
	if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, canvasResult); err != nil {
		t.Fatalf("exact canvas commit receipt rejected: %v", err)
	}
	jumpedRevision := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":7,"committedRevision":9,"clientMutationId":"mutation-1","proposalHash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","appliedOperationIds":["operation-1"],"evidence":{"addedNodeIds":["node-1"],"updatedNodeIds":[],"deletedNodeIds":[],"upsertedConnectionIds":[],"deletedConnectionIds":[],"selectedNodeIds":["node-1"],"viewportApplied":false}}`)
	if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, jumpedRevision); err == nil {
		t.Fatal("canvas receipt that skipped revisions was accepted")
	}
}
