import assert from "node:assert/strict";
import { test } from "node:test";

import { agentRuntimeClient, parseAgentRuntimeEvent, parseAgentRuntimeView } from "./agent-runtime";

const approvalView = {
    run: {
        id: "run-1",
        threadId: "thread-1",
        actorUserId: "user-1",
        clientRequestId: "request-1",
        status: "waiting_approval",
        lastEventSequence: 4,
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        modelRecordId: "model-1",
        modelKey: "agent-model",
        toolSchemaVersion: 6,
        runtimeVersion: 5,
        policyVersion: 5,
        createdAt: "2026-09-01T00:00:00Z",
        updatedAt: "2026-09-01T00:00:01Z",
    },
    state: {
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        status: "waiting_approval",
        pendingToolCall: {
            toolCallId: "call-1",
            toolName: "media.generate",
            actionVersion: 1,
            arguments: {
                mediaKind: "video",
                modelRecordId: "video-model-record",
                modelKey: "seedance-2.0",
                parameters: { prompt: "雨夜追逐", durationSeconds: 5 },
                sourceResourceIds: [],
                targetCanvasNodeId: "video-node",
                clientRequestId: "generate-video-1",
            },
            expectedDelivery: { kind: "generated_asset", requiredArtifacts: ["video"], completionCriteria: [{ fact: "artifact", artifact: "video" }] },
        },
        clarificationHistory: [],
        userMessage: "生成一段视频",
        configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" },
    },
    pendingApproval: {
        toolCallId: "call-1",
        toolName: "media.generate",
        actionVersion: 1,
        proposalHash: "a".repeat(64),
        expiresAt: "2026-09-01T00:05:00Z",
        effect: { kind: "media_generation", summary: "生成 video 媒体", targetIds: ["video-node"] },
        quote: { modelRecordId: "video-model-record", modelKey: "seedance-2.0", priceVersion: 7, amountMicrocredits: 1_250_000 },
    },
};

test("Agent UI 只接受协议 v5 并拒绝退役生产图事件", () => {
    const current = {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 5,
        kind: "item.completed",
        itemId: "item-1",
        itemKind: "agent_message",
        payload: { message: "完成" },
        createdAt: "2026-09-01T00:00:02Z",
    } as const;
    assert.deepEqual(parseAgentRuntimeEvent(current), current);
    assert.throws(() => parseAgentRuntimeEvent({ ...current, protocolVersion: 4 }), /协议版本/);
    assert.throws(() => parseAgentRuntimeEvent({ ...current, itemKind: "artifact", payload: { contentType: "artifact_review" } }), /退役/);
    assert.throws(() => parseAgentRuntimeEvent({ ...current, itemKind: "approval", payload: { contentType: "stage_review_resolution" } }), /退役/);
});

test("审批卡严格保留影响、冻结报价、过期时间并提交完整身份", async () => {
    const parsed = parseAgentRuntimeView(approvalView);
    assert.deepEqual(parsed.pendingApproval, approvalView.pendingApproval);

    const originalFetch = globalThis.fetch;
    let submittedBody: unknown;
    globalThis.fetch = (async (_input, init) => {
        submittedBody = JSON.parse(String(init?.body));
        return new Response(JSON.stringify({ code: 0, data: approvalView, msg: "ok" }), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as typeof fetch;
    try {
        await agentRuntimeClient.submitApproval("run-1", {
            toolCallId: "call-1",
            actionVersion: 1,
            proposalHash: "a".repeat(64),
            decision: "approved",
        });
        assert.deepEqual(submittedBody, { toolCallId: "call-1", actionVersion: 1, proposalHash: "a".repeat(64), decision: "approved" });
    } finally {
        globalThis.fetch = originalFetch;
    }
});
