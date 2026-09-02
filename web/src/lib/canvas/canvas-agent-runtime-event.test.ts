import assert from "node:assert/strict";
import { describe, test } from "node:test";

import type { AgentRuntimeEvent } from "@/services/api/agent-runtime";
import { agentCanvasCommittedReceipt, agentCanvasCommittedReceiptFromToolResult, agentVisionAnalyzeResultFromToolResult } from "./canvas-agent-runtime-event";

const proposalHash = "a".repeat(64);

function completedEvent(toolName: string, output: Record<string, unknown>): AgentRuntimeEvent {
    return {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 4,
        createdAt: "2026-09-01T00:00:00Z",
        kind: "item.completed",
        itemId: "item-1",
        itemKind: "tool_call",
        payload: { toolCallId: "call-1", toolName, actionVersion: 1, succeeded: true, output },
    };
}

function validOutput(): Record<string, unknown> {
    return {
        canvasId: "canvas-1",
        baseRevision: 7,
        committedRevision: 8,
        clientMutationId: "mutation-1",
        proposalHash,
        appliedOperationIds: ["operation-1"],
        evidence: {
            addedNodeIds: ["node-1"],
            updatedNodeIds: [],
            deletedNodeIds: [],
            upsertedConnectionIds: [],
            deletedConnectionIds: [],
            selectedNodeIds: ["node-1"],
            viewportApplied: false,
        },
    };
}

describe("agentCanvasCommittedReceipt", () => {
    test("accepts the exact server-authorized canvas.apply_ops receipt", () => {
        assert.deepEqual(agentCanvasCommittedReceipt(completedEvent("canvas.apply_ops", validOutput()), "canvas-1"), validOutput());
    });

    test("rejects retired tools, another canvas, and malformed evidence", () => {
        assert.equal(agentCanvasCommittedReceipt(completedEvent("canvas.project", validOutput()), "canvas-1"), undefined);
        assert.equal(agentCanvasCommittedReceipt(completedEvent("canvas.apply_ops", { ...validOutput(), canvasId: "canvas-2" }), "canvas-1"), undefined);
        assert.equal(agentCanvasCommittedReceipt(completedEvent("canvas.apply_ops", { ...validOutput(), proposalHash: "invalid" }), "canvas-1"), undefined);
        assert.equal(agentCanvasCommittedReceipt(completedEvent("canvas.apply_ops", { ...validOutput(), committedRevision: 9 }), "canvas-1"), undefined);
        assert.equal(agentCanvasCommittedReceipt(completedEvent("canvas.apply_ops", { ...validOutput(), evidence: { addedNodeIds: [] } }), "canvas-1"), undefined);
    });

    test("accepts the authoritative result returned directly by local approval", () => {
        assert.deepEqual(agentCanvasCommittedReceiptFromToolResult({ toolName: "canvas.apply_ops", succeeded: true, output: validOutput() }, "canvas-1"), validOutput());
        assert.equal(agentCanvasCommittedReceiptFromToolResult({ toolName: "canvas.apply_ops", succeeded: false, output: validOutput() }, "canvas-1"), undefined);
    });
});

describe("agentVisionAnalyzeResultFromToolResult", () => {
    const output = {
        taskId: "task-1",
        billingOrderId: "billing-1",
        modelRecordId: "vision-record-1",
        modelKey: "deepseek-v4-flash-vision-exp",
        clientRequestId: "vision-request-1",
        sourceResourceIds: ["resource-1"],
        detail: "low",
        analysis: "角色位于雨夜街道。",
        usage: { inputTokens: 128, cachedTokens: 0, outputTokens: 16 },
    } as const;

    test("accepts only a complete successful vision receipt", () => {
        assert.deepEqual(agentVisionAnalyzeResultFromToolResult({ toolName: "vision.analyze", succeeded: true, output }), output);
        assert.equal(agentVisionAnalyzeResultFromToolResult({ toolName: "vision.analyze", succeeded: false, output }), undefined);
        assert.equal(agentVisionAnalyzeResultFromToolResult({ toolName: "media.generate", succeeded: true, output }), undefined);
        assert.equal(agentVisionAnalyzeResultFromToolResult({ toolName: "vision.analyze", succeeded: true, output: { ...output, providerRequestId: "private" } }), undefined);
    });
});
