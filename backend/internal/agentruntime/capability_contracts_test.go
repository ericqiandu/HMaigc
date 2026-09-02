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
	case agentruntime.ToolVisionAnalyze:
		return json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1"],"prompt":"描述人物外观","detail":"low","clientRequestId":"vision-1"}`)
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
			name:    "vision analyze",
			tool:    agentruntime.ToolVisionAnalyze,
			payload: `{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"提取可追溯的视觉事实","detail":"original","clientRequestId":"vision-1"}`,
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

func TestVisionAnalyzeArgumentsAreClosedNormalizedAndBounded(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-2"],"prompt":"  描述角色造型与场景关系  ","detail":"low","clientRequestId":"vision-request-1"}`)
	decoded, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolVisionAnalyze, valid)
	if err != nil {
		t.Fatalf("valid vision arguments rejected: %v", err)
	}
	arguments, ok := decoded.(agentruntime.VisionAnalyzeArguments)
	if !ok {
		t.Fatalf("vision arguments type = %T", decoded)
	}
	if arguments.Prompt != "描述角色造型与场景关系" || arguments.Detail != agentruntime.VisionDetailLow || len(arguments.SourceResourceIDs) != 2 {
		t.Fatalf("normalized vision arguments = %#v", arguments)
	}

	invalid := []json.RawMessage{
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":[],"prompt":"描述画面","detail":"low","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1","resource-1"],"prompt":"描述画面","detail":"low","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["r1","r2","r3","r4","r5","r6","r7","r8","r9","r10","r11","r12","r13"],"prompt":"描述画面","detail":"low","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1"],"prompt":"","detail":"low","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1"],"prompt":"描述画面","detail":"auto","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1"],"prompt":"描述画面","detail":"low","clientRequestId":"vision-request-1","imageUrl":"https://example.com/private.png"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["https://example.com/private.png"],"prompt":"描述画面","detail":"low","clientRequestId":"vision-request-1"}`),
		json.RawMessage(`{"modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","sourceResourceIds":["resource-1"],"prompt":"` + strings.Repeat("a", 64*1024+1) + `","detail":"low","clientRequestId":"vision-request-1"}`),
	}
	for index, payload := range invalid {
		if _, decodeErr := agentruntime.DecodeCapabilityArguments(agentruntime.ToolVisionAnalyze, payload); decodeErr == nil {
			t.Fatalf("invalid vision arguments %d were accepted", index)
		}
	}
}

func TestVisionAnalyzeResultUsesClosedUsageContract(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"original","analysis":"角色穿红色外套，站在雨夜街道。","usage":{"inputTokens":384,"cachedTokens":0,"outputTokens":42}}`)
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolVisionAnalyze, valid)
	if err != nil {
		t.Fatalf("valid vision result rejected: %v", err)
	}
	result, ok := decoded.(agentruntime.VisionAnalyzeResult)
	if !ok || result.Usage.InputTokens != 384 || result.Analysis == "" {
		t.Fatalf("vision result = %#v (%T)", decoded, decoded)
	}

	invalid := []json.RawMessage{
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"low","analysis":"","usage":{"inputTokens":1,"cachedTokens":0,"outputTokens":1}}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1","resource-1"],"detail":"low","analysis":"分析","usage":{"inputTokens":1,"cachedTokens":0,"outputTokens":1}}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"auto","analysis":"分析","usage":{"inputTokens":1,"cachedTokens":0,"outputTokens":1}}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"low","analysis":"分析","usage":{"inputTokens":1,"cachedTokens":2,"outputTokens":1}}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"low","analysis":"分析","usage":{"inputTokens":1,"outputTokens":1}}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"billing-1","modelRecordId":"vision-record-1","modelKey":"deepseek-v4-flash-vision-exp","clientRequestId":"vision-request-1","sourceResourceIds":["resource-1"],"detail":"low","analysis":"分析","usage":{"inputTokens":1,"cachedTokens":0,"outputTokens":1},"providerRequestId":"secret-provider-id"}`),
	}
	for index, payload := range invalid {
		if _, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolVisionAnalyze, payload); decodeErr == nil {
			t.Fatalf("invalid vision result %d was accepted", index)
		}
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

	unsupportedNodeType := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[` +
		`{"operationId":"op-1","type":"add_node","node":{"id":"node-1","type":"media","title":"非法媒体占位","position":{"x":0,"y":0},"width":320,"height":320,"metadata":{"status":"loading"}}}]}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, unsupportedNodeType); err == nil {
		t.Fatal("unsupported canvas node type was accepted")
	}

	unsupportedNodeStatus := json.RawMessage(`{"canvasId":"canvas-1","baseRevision":0,"clientMutationId":"mutation-1","operations":[` +
		`{"operationId":"op-1","type":"add_node","node":{"id":"node-1","type":"image","title":"非法状态占位","position":{"x":0,"y":0},"width":320,"height":320,"metadata":{"status":"pending"}}}]}`)
	if _, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, unsupportedNodeStatus); err == nil {
		t.Fatal("unsupported canvas node status was accepted")
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

	valid := json.RawMessage(`{"taskId":"task-1","billingOrderId":"order-1","mediaKind":"video","clientRequestId":"request-1","resources":[{"resourceId":"resource-1","kind":"video","url":"/api/resources/resource-1/file"}]}`)
	result, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, valid)
	if err != nil {
		t.Fatalf("DecodeCapabilityResult() error = %v", err)
	}
	if _, ok := result.(agentruntime.MediaGenerateResult); !ok {
		t.Fatalf("DecodeCapabilityResult() type = %T", result)
	}

	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"taskId":"task-1","mediaKind":"video","clientRequestId":"request-1"}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"order-1","mediaKind":"video","clientRequestId":"request-1","resources":[]}`),
		json.RawMessage(`{"taskId":"task-1","billingOrderId":"order-1","mediaKind":"video","clientRequestId":"request-1","resources":[{"resourceId":"resource-1","kind":"video","url":"https://example.com/final.mp4"}]}`),
	} {
		if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, invalid); err == nil {
			t.Fatal("incomplete or non-authoritative media result was accepted")
		}
	}

	unknown := json.RawMessage(`{"taskId":"task-1","billingOrderId":"order-1","mediaKind":"video","clientRequestId":"request-1","resources":[{"resourceId":"resource-1","kind":"video","url":"/api/resources/resource-1/file"}],"providerUrl":"https://example.com/final.mp4"}`)
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

func TestCanvasReadResultRequiresAuthoritativeExecutionFacts(t *testing.T) {
	t.Parallel()

	valid := json.RawMessage(`{"canvasId":"canvas-1","domainProjectId":"project-1","revision":7,"nodes":[],"edges":[],"selectedNodeIds":[],"viewport":{"x":0,"y":0,"zoom":1},"callableModels":[{"channelId":"channel-1","modelRecordId":"record-1","modelKey":"image-model","displayName":"Image Model","capability":"image","billingMode":"fixed_request","priceStrategy":"flat","unitPriceMicrocredits":100,"priceTiers":[],"providerCapabilities":{}}]}`)
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasRead, valid)
	if err != nil {
		t.Fatalf("canvas read execution facts rejected: %v", err)
	}
	result := decoded.(agentruntime.CanvasReadResult)
	if result.DomainProjectID != "project-1" || len(result.CallableModels) != 1 || result.CallableModels[0].ModelRecordID != "record-1" {
		t.Fatalf("canvas read execution facts = %#v", result)
	}

	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"canvasId":"canvas-1","domainProjectId":"project-1","revision":7,"nodes":[],"edges":[],"selectedNodeIds":[],"viewport":{"x":0,"y":0,"zoom":1}}`),
		json.RawMessage(`{"canvasId":"canvas-1","domainProjectId":"project-1","revision":7,"nodes":[],"edges":[],"selectedNodeIds":[],"viewport":{"x":0,"y":0,"zoom":1},"callableModels":[{"channelId":"channel-1","modelRecordId":"","modelKey":"image-model","displayName":"Image Model","capability":"image","billingMode":"fixed_request","priceStrategy":"flat","unitPriceMicrocredits":100,"priceTiers":[]}]}`),
		json.RawMessage(`{"canvasId":"canvas-1","domainProjectId":"project-1","revision":7,"nodes":[],"edges":[],"selectedNodeIds":[],"viewport":{"x":0,"y":0,"zoom":1},"callableModels":[{"channelId":"channel-1","modelRecordId":"record-1","modelKey":"image-model","displayName":"Image Model","capability":"image","billingMode":"fixed_request","priceStrategy":"flat","unitPriceMicrocredits":0,"priceTiers":[]}]}`),
	} {
		if _, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasRead, invalid); err == nil {
			t.Fatal("canvas read result without valid execution facts was accepted")
		}
	}
}
