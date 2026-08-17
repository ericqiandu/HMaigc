import { expect, test } from "bun:test";

import { agentRuntimeClient, parseAgentRuntimeEvent, parseAgentRuntimeView, parseAgentThreadHistory } from "../src/services/api/agent-runtime";

const state = {
    stateVersion: 2,
    stepNumber: 1,
    maxSteps: 8,
    status: "waiting_approval",
    pendingToolCall: {
        toolCallId: "tool-1",
        toolName: "canvas.apply_ops",
        actionVersion: 3,
        arguments: { baseRevision: 7, patch: { upsertNodes: [] } },
        expectedDelivery: { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision", artifact: "canvas_revision" }] },
    },
    userMessage: "整理当前画布",
    configuration: {
        generationModels: { image: { channelId: "channel-image", model: "gpt-image-2" } },
        skills: [{ dir: "storyboard", name: "分镜", description: "生成分镜", instructions: "按镜头输出", version: 1 }],
        attachments: [{ resourceId: "resource-1", name: "参考图.png", mimeType: "image/png", width: 1024, height: 1024 }],
        executionMode: "automatic",
    },
};

const view = {
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
        toolSchemaVersion: 1,
        createdAt: "2026-08-15T00:00:00Z",
        updatedAt: "2026-08-15T00:00:01Z",
    },
    state,
};

test("Agent Runtime DTO 严格保留审批身份与结构化参数", () => {
    expect(parseAgentRuntimeView(view)).toEqual(view);
    expect(
        parseAgentRuntimeEvent({
            sequence: 4,
            kind: "approval.required",
            payload: state,
            createdAt: "2026-08-15T00:00:01Z",
        }),
    ).toEqual({ sequence: 4, kind: "approval.required", payload: state, createdAt: "2026-08-15T00:00:01Z" });
});

test("模型决策拒绝事件保留结构化自修事实", () => {
    const repairState = {
        ...state,
        status: "running",
        pendingToolCall: undefined,
        decisionFeedback: { code: "model_decision_invalid", reason: "answer delivery facts are inconsistent" },
    };
    expect(
        parseAgentRuntimeEvent({
            sequence: 5,
            kind: "model.rejected",
            payload: repairState,
            createdAt: "2026-08-15T00:00:02Z",
        }),
    ).toEqual({ sequence: 5, kind: "model.rejected", payload: repairState, createdAt: "2026-08-15T00:00:02Z" });
});

test("交付合同漂移事件保留冻结合同修复事实", () => {
    const repairState = {
        ...state,
        status: "running",
        pendingToolCall: undefined,
        expectedDelivery: {
            kind: "generated_asset",
            requiredArtifacts: ["image"],
            completionCriteria: [{ fact: "artifact", artifact: "image" }],
        },
        decisionFeedback: { code: "delivery_contract_changed", reason: "expectedDelivery must remain frozen" },
    };
    expect(
        parseAgentRuntimeEvent({
            sequence: 6,
            kind: "model.rejected",
            payload: repairState,
            createdAt: "2026-08-15T00:00:03Z",
        }),
    ).toEqual({ sequence: 6, kind: "model.rejected", payload: repairState, createdAt: "2026-08-15T00:00:03Z" });
});

test("运行配置缺少附件或执行模式时显式拒绝而不是插入默认值", () => {
    const { executionMode: _executionMode, ...withoutMode } = state.configuration;
    expect(() => parseAgentRuntimeView({ ...view, state: { ...state, configuration: withoutMode } })).toThrow("executionMode");
    const { attachments: _attachments, ...withoutAttachments } = state.configuration;
    expect(() => parseAgentRuntimeView({ ...view, state: { ...state, configuration: withoutAttachments } })).toThrow("attachments");
});

test("未知运行状态与事件类型显式失败", () => {
    expect(() => parseAgentRuntimeView({ ...view, state: { ...state, status: "thinking" } })).toThrow("不受支持");
    expect(() =>
        parseAgentRuntimeEvent({
            sequence: 5,
            kind: "assistant.guessed",
            payload: state,
            createdAt: "2026-08-15T00:00:02Z",
        }),
    ).toThrow("不受支持");
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            run: { ...view.run, status: "succeeded" },
            state: {
                ...state,
                status: "succeeded",
                pendingToolCall: undefined,
                expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "guessed" }] },
            },
        }),
    ).toThrow("不受支持");
});

test("互相冲突的运行状态事实必须显式失败", () => {
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            run: { ...view.run, status: "succeeded" },
            state: { ...state, status: "succeeded", pendingToolCall: undefined, finalMessage: "看似完成" },
        }),
    ).toThrow("成功状态");
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            state: { ...state, pendingToolCall: undefined },
        }),
    ).toThrow("等待审批状态");
});

const history = {
    items: [
        {
            thread: {
                id: "thread-1",
                canvasId: "canvas-1",
                status: "active",
                createdAt: "2026-08-15T01:00:00Z",
                updatedAt: "2026-08-15T01:00:00Z",
            },
            activityAt: "2026-08-15T02:00:00Z",
            latestRun: view,
        },
        {
            thread: {
                id: "thread-empty",
                canvasId: "canvas-1",
                status: "active",
                createdAt: "2026-08-15T00:00:00Z",
                updatedAt: "2026-08-15T00:00:00Z",
            },
            activityAt: "2026-08-15T00:00:00Z",
            latestRun: null,
        },
    ],
};

test("会话历史严格保留最近运行和空会话事实", () => {
    const parsed = parseAgentThreadHistory(history);
    expect(parsed.items[0]?.latestRun?.state.userMessage).toBe(state.userMessage);
    expect(parsed.items[0]?.activityAt).toBe("2026-08-15T02:00:00Z");
    expect(parsed.items[1]?.thread.id).toBe("thread-empty");
    expect(parsed.items[1]?.latestRun).toBeNull();
});

test("会话历史拒绝状态、归属、时间、数量和必填字段冲突", () => {
    expect(() => parseAgentThreadHistory({ items: [{ ...history.items[0], thread: { ...history.items[0]!.thread, status: "archived" } }] })).toThrow("状态");
    expect(() =>
        parseAgentThreadHistory({
            items: [{ ...history.items[0], latestRun: { ...view, run: { ...view.run, threadId: "thread-other" } } }],
        }),
    ).toThrow("归属");
    expect(() => parseAgentThreadHistory({ items: [{ ...history.items[0], activityAt: "2026/08/15 02:00:00" }] })).toThrow("UTC");
    expect(() => parseAgentThreadHistory({ items: Array.from({ length: 21 }, () => history.items[1]) })).toThrow("20");
    expect(() => parseAgentThreadHistory({})).toThrow("items");
    expect(() => parseAgentThreadHistory({ items: [{ ...history.items[0], thread: undefined }] })).toThrow("thread");
    const { latestRun: _latestRun, ...missingLatestRun } = history.items[0]!;
    expect(() => parseAgentThreadHistory({ items: [missingLatestRun] })).toThrow("latestRun");
});

test("会话历史客户端编码画布标识并使用显式 limit", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input) => {
        expect(String(input)).toEndWith("/agent/threads?canvasId=canvas%20%2F%201&limit=7");
        return new Response(JSON.stringify({ code: 0, data: history, msg: "ok" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
        });
    }) as typeof fetch;
    try {
        const parsed = await agentRuntimeClient.listThreads("canvas / 1", 7);
        expect(parsed.items).toHaveLength(2);
    } finally {
        globalThis.fetch = originalFetch;
    }
});
