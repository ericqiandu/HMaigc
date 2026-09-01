import assert from "node:assert/strict";
import { describe, test } from "node:test";

import type { AgentRuntimeEvent } from "@/services/api/agent-runtime";
import { agentCanvasCommittedReceipt } from "./canvas-agent-runtime-event";

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
});
