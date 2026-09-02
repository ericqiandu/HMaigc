import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { parseAgentCapabilityArguments, parseAgentCapabilityResult, parseAgentRuntimeEvent } from "./agent-runtime";

describe("agent capability wire contracts", () => {
    test("parses the seven current capability argument contracts", () => {
        assert.deepEqual(parseAgentCapabilityArguments("canvas.read", { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true }), {
            canvasId: "canvas-1",
            selectedNodeIds: [],
            includeViewport: true,
        });
        assert.deepEqual(
            parseAgentCapabilityArguments("media.generate", {
                mediaKind: "video",
                modelRecordId: "record-1",
                modelKey: "seedance-2.0",
                parameters: { prompt: "雨夜追逐", durationSeconds: 5 },
                sourceResourceIds: ["resource-1"],
                targetCanvasNodeId: "node-1",
                clientRequestId: "request-1",
            }),
            {
                mediaKind: "video",
                modelRecordId: "record-1",
                modelKey: "seedance-2.0",
                parameters: { prompt: "雨夜追逐", durationSeconds: 5 },
                sourceResourceIds: ["resource-1"],
                targetCanvasNodeId: "node-1",
                clientRequestId: "request-1",
            },
        );
        assert.deepEqual(
            parseAgentCapabilityArguments("vision.analyze", {
                modelRecordId: "vision-record-1",
                modelKey: "deepseek-v4-flash-vision-exp",
                sourceResourceIds: ["resource-1", "resource-2"],
                prompt: "提取可追溯的视觉事实",
                detail: "low",
                clientRequestId: "vision-request-1",
            }),
            {
                modelRecordId: "vision-record-1",
                modelKey: "deepseek-v4-flash-vision-exp",
                sourceResourceIds: ["resource-1", "resource-2"],
                prompt: "提取可追溯的视觉事实",
                detail: "low",
                clientRequestId: "vision-request-1",
            },
        );
    });

    test("strictly decodes a vision result and rejects secret or malformed provider facts", () => {
        const result = {
            taskId: "task-1",
            billingOrderId: "billing-1",
            modelRecordId: "vision-record-1",
            modelKey: "deepseek-v4-flash-vision-exp",
            clientRequestId: "vision-request-1",
            sourceResourceIds: ["resource-1"],
            detail: "original",
            analysis: "人物穿红色外套，站在雨夜街道。",
            usage: { inputTokens: 384, cachedTokens: 0, outputTokens: 42 },
        } as const;

        assert.deepEqual(parseAgentCapabilityResult("vision.analyze", result), result);
        assert.throws(() => parseAgentCapabilityResult("vision.analyze", { ...result, providerRequestId: "provider-secret" }), /未知字段/);
        assert.throws(() => parseAgentCapabilityResult("vision.analyze", { ...result, usage: { inputTokens: 1, cachedTokens: 2, outputTokens: 1 } }), /cachedTokens/);
        assert.throws(() => parseAgentCapabilityResult("vision.analyze", { ...result, sourceResourceIds: ["resource-1", "resource-1"] }), /重复/);
    });

    test("rejects retired tools, unknown fields, and malformed operations", () => {
        assert.throws(() => parseAgentCapabilityArguments("canvas.project", {}), /不受支持/);
        assert.throws(() => parseAgentCapabilityArguments("canvas.read", { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true, fallback: true }), /未知字段/);
        assert.throws(() => parseAgentCapabilityArguments("canvas.read", { canvasId: "canvas-1", includeViewport: true }));
        assert.throws(() =>
            parseAgentCapabilityArguments("assets.publish", {
                resourceId: "urn:asset:1",
                domainProjectId: "project-1",
                displayName: "角色",
                clientMutationId: "publish-1",
            }),
        );
        assert.throws(() =>
            parseAgentCapabilityArguments("canvas.apply_ops", {
                canvasId: "canvas-1",
                baseRevision: 0,
                clientMutationId: "mutation-1",
                operations: [{ operationId: "op-1", type: "connect_nodes", connection: { id: "edge-1", fromNodeId: "node-1" } }],
            }),
        );
        assert.throws(() =>
            parseAgentCapabilityArguments("vision.analyze", {
                modelRecordId: "vision-record-1",
                modelKey: "deepseek-v4-flash-vision-exp",
                sourceResourceIds: ["https://example.com/private.png"],
                prompt: "描述画面",
                detail: "auto",
                clientRequestId: "vision-request-1",
            }),
        );
    });

    test("rejects canvas node types and lifecycle states outside the wire contract", () => {
        const operationArguments = (operation: Record<string, unknown>) => ({
            canvasId: "canvas-1",
            baseRevision: 7,
            clientMutationId: "mutation-1",
            operations: [operation],
        });

        assert.throws(
            () =>
                parseAgentCapabilityArguments(
                    "canvas.apply_ops",
                    operationArguments({
                        operationId: "op-invalid-type",
                        type: "add_node",
                        node: {
                            id: "node-1",
                            type: "media",
                            title: "无效媒体占位",
                            position: { x: 0, y: 0 },
                            width: 320,
                            height: 240,
                            metadata: { status: "loading" },
                        },
                    }),
                ),
            /node\.type/,
        );
        assert.throws(
            () =>
                parseAgentCapabilityArguments(
                    "canvas.apply_ops",
                    operationArguments({
                        operationId: "op-invalid-status",
                        type: "add_node",
                        node: {
                            id: "node-1",
                            type: "image",
                            title: "无效状态占位",
                            position: { x: 0, y: 0 },
                            width: 320,
                            height: 240,
                            metadata: { status: "pending" },
                        },
                    }),
                ),
            /metadata\.status/,
        );
        assert.throws(
            () =>
                parseAgentCapabilityArguments(
                    "canvas.apply_ops",
                    operationArguments({
                        operationId: "op-invalid-update",
                        type: "update_node",
                        nodeId: "node-1",
                        patch: { type: "media", metadata: { status: "pending" } },
                    }),
                ),
            /patch\.type/,
        );
    });

    test("rejects malformed UI protocol events without defaults", () => {
        assert.throws(
            () =>
                parseAgentRuntimeEvent({
                    protocolVersion: 5,
                    threadId: "thread-1",
                    runId: "run-1",
                    sequence: 1,
                    kind: "item.started",
                    itemId: "item-1",
                    itemKind: "tool_call",
                    payload: {
                        toolCallId: "call-1",
                        toolName: "canvas.project",
                        actionVersion: 1,
                    },
                    createdAt: "2026-09-01T00:00:00Z",
                }),
            /不受支持/,
        );
    });
});
