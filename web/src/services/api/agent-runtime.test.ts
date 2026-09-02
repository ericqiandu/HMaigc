import assert from "node:assert/strict";
import { test } from "node:test";

import { AgentRuntimeRequestError, agentLocalRuntimeClient, agentRuntimeClient, parseAgentRuntimeEvent, parseAgentRuntimeView } from "./agent-runtime";

const approvalView = {
    run: {
        id: "run-1",
        threadId: "thread-1",
        reasoningHost: "managed",
        actorUserId: "user-1",
        clientRequestId: "request-1",
        status: "waiting_approval",
        lastEventSequence: 4,
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        modelRecordId: "model-1",
        modelKey: "agent-model",
        toolSchemaVersion: 8,
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
        configuration: {
            generationModels: { vision: { channelId: "channel-deepseek", modelRecordId: "vision-record-1", model: "deepseek-v4-flash-vision-exp", priceVersion: 4 } },
            skills: [],
            attachments: [],
            executionMode: "guided",
        },
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

const localRuntimeView = {
    run: {
        ...approvalView.run,
        reasoningHost: "local_codex",
        status: "running",
        modelRecordId: "",
        modelKey: "",
    },
    state: {
        ...approvalView.state,
        status: "running",
        pendingToolCall: undefined,
    },
};

test("Agent Runtime 严格解析 managed 与 local_codex 推理来源", () => {
    assert.equal(parseAgentRuntimeView(approvalView).run.reasoningHost, "managed");
    assert.equal(parseAgentRuntimeView(localRuntimeView).run.reasoningHost, "local_codex");

    const missingHost = { ...localRuntimeView, run: { ...localRuntimeView.run, reasoningHost: undefined } };
    const invalidHost = { ...localRuntimeView, run: { ...localRuntimeView.run, reasoningHost: "browser" } };
    assert.throws(() => parseAgentRuntimeView(missingHost), /reasoningHost/);
    assert.throws(() => parseAgentRuntimeView(invalidHost), /reasoningHost/);
});

test("本机 Run 不依赖网站专属视觉模型，网站 Run 仍必须冻结视觉模型", () => {
    const { vision: _vision, ...localGenerationModels } = localRuntimeView.state.configuration.generationModels;
    const localWithoutVision = {
        ...localRuntimeView,
        state: {
            ...localRuntimeView.state,
            configuration: {
                ...localRuntimeView.state.configuration,
                generationModels: localGenerationModels,
            },
        },
    };
    assert.equal(parseAgentRuntimeView(localWithoutVision).run.reasoningHost, "local_codex");

    const managedWithoutVision = {
        ...approvalView,
        state: {
            ...approvalView.state,
            configuration: {
                ...approvalView.state.configuration,
                generationModels: localGenerationModels,
            },
        },
    };
    assert.throws(() => parseAgentRuntimeView(managedWithoutVision), /vision/);
});

test("本机 Agent 客户端只使用 local 网关、携带会话并投影 CAS 冲突", async () => {
    const originalFetch = globalThis.fetch;
    const requests: Array<{ url: string; credentials?: RequestCredentials }> = [];
    globalThis.fetch = (async (input, init) => {
        requests.push({ url: String(input), credentials: init?.credentials });
        if (String(input).endsWith("/agent/local/runs")) {
            return new Response(JSON.stringify({ code: 0, data: localRuntimeView, msg: "ok" }), {
                status: 200,
                headers: { "Content-Type": "application/json" },
            });
        }
        if (String(input).endsWith("/agent/runs/run-1/interrupt")) {
            const cancelledView = {
                run: { ...localRuntimeView.run, status: "cancelled", stateVersion: 3 },
                state: { ...localRuntimeView.state, status: "cancelled", stateVersion: 3, failureCode: "agent_run_cancelled" },
            };
            return new Response(JSON.stringify({ code: 0, data: cancelledView, msg: "ok" }), {
                status: 200,
                headers: { "Content-Type": "application/json" },
            });
        }
        return new Response(
            JSON.stringify({
                code: 409,
                data: { errorCode: "agent_external_decision_conflict", latestStateVersion: 4 },
                msg: "状态已经变化",
            }),
            { status: 409, headers: { "Content-Type": "application/json" } },
        );
    }) as typeof fetch;

    try {
        const started = await agentLocalRuntimeClient.startRun({
            canvasId: "canvas-1",
            externalThreadId: "codex-thread-1",
            clientRequestId: "turn-1",
            userMessage: "读取画布",
            maxSteps: 8,
            configuration: { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" },
        });
        assert.equal(started.run.reasoningHost, "local_codex");

        await assert.rejects(
            () =>
                agentLocalRuntimeClient.submitDecision("run-1", {
                    clientRequestId: "decision-1",
                    expectedStateVersion: 2,
                    decision: {
                        kind: "final",
                        final: {
                            message: "读取完成",
                            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
                        },
                    },
                }),
            (error: unknown) => {
                assert.ok(error instanceof AgentRuntimeRequestError);
                assert.equal(error.status, 409);
                assert.equal(error.code, "agent_external_decision_conflict");
                assert.equal(error.latestStateVersion, 4);
                return true;
            },
        );
        const interrupted = await agentLocalRuntimeClient.interrupt("run-1", { expectedStateVersion: 2 });
        assert.equal(interrupted.state.status, "cancelled");
        assert.ok(requests[0]?.url.endsWith("/agent/local/runs"));
        assert.ok(requests[1]?.url.endsWith("/agent/local/runs/run-1/decisions"));
        assert.ok(requests[2]?.url.endsWith("/agent/runs/run-1/interrupt"));
        assert.deepEqual(
            requests.map((entry) => entry.credentials),
            ["include", "include", "include"],
        );
    } finally {
        globalThis.fetch = originalFetch;
    }
});

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

test("工具时间线完成事件只要求审计事实而不是完整待执行调用", () => {
    const completedToolActivity = {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 6,
        kind: "item.completed",
        itemId: "tool-item-1",
        itemKind: "tool_call",
        payload: {
            toolCallId: "call-1",
            toolName: "canvas.read",
            actionVersion: 1,
            succeeded: true,
        },
        createdAt: "2026-09-01T00:00:02Z",
    } as const;

    assert.deepEqual(parseAgentRuntimeEvent(completedToolActivity), completedToolActivity);
});

test("媒体完成事件只接受与资源身份精确匹配的站内稳定地址", () => {
    const mediaCompleted = {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 7,
        kind: "item.completed",
        itemId: "tool-item-media-1",
        itemKind: "tool_call",
        payload: {
            toolCallId: "call-media-1",
            toolName: "media.generate",
            actionVersion: 1,
            succeeded: true,
            output: {
                taskId: "task-1",
                resources: [{ resourceId: "resource-1", kind: "image", url: "/api/resources/resource-1/file" }],
            },
        },
        createdAt: "2026-09-01T00:00:03Z",
    } as const;

    assert.deepEqual(parseAgentRuntimeEvent(mediaCompleted), mediaCompleted);
    assert.throws(
        () =>
            parseAgentRuntimeEvent({
                ...mediaCompleted,
                payload: {
                    ...mediaCompleted.payload,
                    output: { ...mediaCompleted.payload.output, resources: [{ resourceId: "resource-1", kind: "image", url: "https://oss.example.com/signed" }] },
                },
            }),
        /短期媒体地址/,
    );
    assert.throws(
        () =>
            parseAgentRuntimeEvent({
                ...mediaCompleted,
                payload: {
                    ...mediaCompleted.payload,
                    output: { ...mediaCompleted.payload.output, resources: [{ resourceId: "resource-1", kind: "image", url: "/api/resources/resource-2/file" }] },
                },
            }),
        /资源身份不匹配/,
    );
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
