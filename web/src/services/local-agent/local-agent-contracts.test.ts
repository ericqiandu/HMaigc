import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { parseLocalAgentEvent, parseLocalAgentStartResponse } from "./local-agent-contracts";

const expectedDelivery = {
    kind: "canvas_change",
    targetCanvasId: "canvas-1",
    completionCriteria: [{ fact: "canvas_revision" }],
} as const;

describe("local Agent contracts", () => {
    it("parses structured tool calls and final decisions", () => {
        assert.deepEqual(
            parseLocalAgentEvent({
                protocolVersion: 1,
                kind: "tool_call",
                requestId: "request-1",
                threadId: "thread-1",
                turnId: "turn-1",
                toolName: "canvas.read",
                arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
                expectedDelivery,
                createdAt: "2026-09-01T00:00:00.000Z",
            }),
            {
                protocolVersion: 1,
                kind: "tool_call",
                requestId: "request-1",
                threadId: "thread-1",
                turnId: "turn-1",
                toolName: "canvas.read",
                arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
                expectedDelivery,
                createdAt: "2026-09-01T00:00:00.000Z",
            },
        );

        assert.deepEqual(
            parseLocalAgentEvent({
                kind: "final_decision",
                threadId: "thread-1",
                turnId: "turn-1",
                message: "画布已更新",
                expectedDelivery,
            }),
            { kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "画布已更新", expectedDelivery },
        );
    });

    it("rejects unknown fields and incomplete delivery facts", () => {
        assert.throws(() => parseLocalAgentEvent({ kind: "connected", token: "leak" }), /未知字段/);
        assert.throws(() =>
            parseLocalAgentEvent({
                kind: "final_decision",
                threadId: "thread-1",
                turnId: "turn-1",
                message: "完成",
                expectedDelivery: { kind: "answer", completionCriteria: [] },
            }),
        );
    });

    it("parses the exact start response", () => {
        assert.deepEqual(parseLocalAgentStartResponse({ threadId: "thread-1", turnId: "turn-1" }), { threadId: "thread-1", turnId: "turn-1" });
        assert.throws(() => parseLocalAgentStartResponse({ threadId: "thread-1", turnId: "turn-1", extra: true }), /未知字段/);
    });
});
