import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { parseAgentCapabilityArguments, parseAgentRuntimeEvent } from "./agent-runtime";

describe("agent capability wire contracts", () => {
    test("parses the six current capability argument contracts", () => {
        assert.deepEqual(
            parseAgentCapabilityArguments("canvas.read", { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true }),
            {
                canvasId: "canvas-1",
                selectedNodeIds: [],
                includeViewport: true,
            },
        );
        assert.deepEqual(parseAgentCapabilityArguments("media.generate", {
                mediaKind: "video",
                modelRecordId: "record-1",
                modelKey: "seedance-2.0",
                parameters: { prompt: "雨夜追逐", durationSeconds: 5 },
                sourceResourceIds: ["resource-1"],
                targetCanvasNodeId: "node-1",
                clientRequestId: "request-1",
            }), {
                mediaKind: "video",
                modelRecordId: "record-1",
                modelKey: "seedance-2.0",
                parameters: { prompt: "雨夜追逐", durationSeconds: 5 },
                sourceResourceIds: ["resource-1"],
                targetCanvasNodeId: "node-1",
                clientRequestId: "request-1",
            });
    });

    test("rejects retired tools, unknown fields, and malformed operations", () => {
        assert.throws(() => parseAgentCapabilityArguments("canvas.project", {}), /不受支持/);
        assert.throws(
            () => parseAgentCapabilityArguments("canvas.read", { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true, fallback: true }),
            /未知字段/,
        );
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
    });

    test("rejects malformed UI protocol events without defaults", () => {
        assert.throws(
            () => parseAgentRuntimeEvent({
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
                    arguments: {},
                    expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
                },
                createdAt: "2026-09-01T00:00:00Z",
            }),
            /不受支持/,
        );
    });
});
