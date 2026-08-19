import { expect, test } from "bun:test";

import { agentRuntimeClient, parseAgentRuntimeEvent, parseAgentRuntimeView, parseAgentThreadHistory } from "../src/services/api/agent-runtime";

const state = {
    stateVersion: 2,
    stepNumber: 1,
    maxSteps: 8,
    status: "waiting_approval",
    clarificationHistory: [],
    pendingToolCall: {
        toolCallId: "tool-1",
        toolName: "production.render",
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

test.each(["skill.load", "production.plan", "production.render", "canvas.commit"])("Agent Runtime DTO 接受当前生产工具 %s", (toolName) => {
    const productionState = {
        ...state,
        status: "waiting_tool",
        pendingToolCall: {
            ...state.pendingToolCall,
            toolName,
        },
    };
    const productionView = {
        ...view,
        run: { ...view.run, status: "waiting_tool" },
        state: productionState,
    };

    expect(parseAgentRuntimeView(productionView).state.pendingToolCall?.toolName).toBe(toolName);
});

test("模型决策拒绝事件保留结构化自修事实", () => {
    const { pendingToolCall: _pendingToolCall, ...stateWithoutPendingTool } = state;
    const repairState = {
        ...stateWithoutPendingTool,
        status: "running",
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

test.each(["required_skill_not_loaded", "clarification_identity_reused"])("Agent Runtime DTO 接受后端自修反馈 %s", (code) => {
    const { pendingToolCall: _pendingToolCall, ...stateWithoutPendingTool } = state;
    const parsed = parseAgentRuntimeView({
        ...view,
        run: { ...view.run, status: "running" },
        state: { ...stateWithoutPendingTool, status: "running", decisionFeedback: { code, reason: "repair required" } },
    });

    expect(parsed.state.decisionFeedback?.code).toBe(code);
});

test("交付合同漂移事件保留冻结合同修复事实", () => {
    const { pendingToolCall: _pendingToolCall, ...stateWithoutPendingTool } = state;
    const repairState = {
        ...stateWithoutPendingTool,
        status: "running",
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

const clarificationRequest = {
    requestId: "vehicle-ad-brief",
    questions: [
        {
            id: "style",
            prompt: "广告的核心风格是什么？",
            type: "multi_choice",
            options: [
                { id: "luxury", label: "豪华感" },
                { id: "performance", label: "性能激情" },
            ],
            allowCustomAnswer: true,
        },
        { id: "brand", prompt: "车型与品牌是什么？", type: "free_text" },
    ],
    expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
};

const waitingInputState = {
    ...state,
    status: "waiting_input",
    pendingToolCall: undefined,
    expectedDelivery: clarificationRequest.expectedDelivery,
    pendingClarification: {
        request: clarificationRequest,
        answers: [{ questionId: "style", selectedOptionIds: ["luxury"], customText: "都市夜景", skipped: false }],
    },
    clarificationHistory: [
        {
            request: { ...clarificationRequest, requestId: "earlier-brief", questions: [clarificationRequest.questions[1]] },
            answers: [{ questionId: "brand", selectedOptionIds: [], customText: "BMW X5", skipped: false }],
            completionQuestionId: "brand",
            completionExpectedStateVersion: 2,
        },
    ],
};

test("结构化追问 DTO 保留 pending、历史与三类持久事件", () => {
    const parsed = parseAgentRuntimeView({
        ...view,
        run: { ...view.run, status: "waiting_input" },
        state: waitingInputState,
    });
    expect(parsed.state.pendingClarification?.request.questions[0]?.options.map((option) => option.id)).toEqual(["luxury", "performance"]);
    expect(parsed.state.pendingClarification?.answers[0]?.customText).toBe("都市夜景");
    expect(parsed.state.clarificationHistory[0]?.answers[0]?.customText).toBe("BMW X5");
    for (const kind of ["clarification.requested", "clarification.answer_saved", "clarification.responded"] as const) {
        expect(parseAgentRuntimeEvent({ sequence: 7, kind, payload: waitingInputState, createdAt: "2026-08-15T00:00:04Z" }).kind).toBe(kind);
    }
});

test("结构化追问 DTO 拒绝未知类型、重复身份、非法答案和未知字段", () => {
    const parseWaiting = (pendingClarification: unknown) =>
        parseAgentRuntimeView({
            ...view,
            run: { ...view.run, status: "waiting_input" },
            state: { ...waitingInputState, pendingClarification },
        });
    expect(() =>
        parseWaiting({
            request: { ...clarificationRequest, questions: [{ ...clarificationRequest.questions[0], type: "ranking" }] },
            answers: [],
        }),
    ).toThrow("问题类型");
    expect(() =>
        parseWaiting({
            request: { ...clarificationRequest, questions: [clarificationRequest.questions[0], clarificationRequest.questions[0]] },
            answers: [],
        }),
    ).toThrow("重复");
    expect(() =>
        parseWaiting({
            request: clarificationRequest,
            answers: [{ questionId: "style", selectedOptionIds: ["unknown"], customText: "", skipped: false }],
        }),
    ).toThrow("选项");
    expect(() =>
        parseWaiting({
            request: { ...clarificationRequest, questions: [{ ...clarificationRequest.questions[0], debug: true }] },
            answers: [],
        }),
    ).toThrow("未知字段");
    expect(() =>
        parseWaiting({
            request: clarificationRequest,
        }),
    ).toThrow("answers");
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            state: { ...view.state, clarificationHistory: undefined },
        }),
    ).toThrow("clarificationHistory");
});

test("waiting_input 与 pending clarification 必须保持一致", () => {
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            run: { ...view.run, status: "waiting_input" },
            state: { ...waitingInputState, pendingClarification: undefined },
        }),
    ).toThrow("追问");
    expect(() =>
        parseAgentRuntimeView({
            ...view,
            run: { ...view.run, status: "running" },
            state: { ...waitingInputState, status: "running" },
        }),
    ).toThrow("追问");
});

test("追问回答客户端编码路径并保留 409 结构化错误", async () => {
    const originalFetch = globalThis.fetch;
    let requestCount = 0;
    globalThis.fetch = (async (input, init) => {
        requestCount += 1;
        expect(String(input)).toEndWith("/agent/runs/run%20%2F1/clarifications/request%20%2F1/responses");
        expect(init?.method).toBe("POST");
        expect(JSON.parse(String(init?.body))).toEqual({
            expectedStateVersion: 4,
            questionId: "style",
            answer: { selectedOptionIds: ["luxury"], customText: "", skipped: false },
            complete: false,
        });
        return new Response(
            JSON.stringify({ code: 409, data: { errorCode: "agent_clarification_conflict", latestStateVersion: 5 }, msg: "问题已更新" }),
            { status: 409, headers: { "Content-Type": "application/json" } },
        );
    }) as typeof fetch;
    try {
        await expect(
            agentRuntimeClient.submitClarificationResponse("run /1", "request /1", {
                expectedStateVersion: 4,
                questionId: "style",
                answer: { selectedOptionIds: ["luxury"], customText: "", skipped: false },
                complete: false,
            }),
        ).rejects.toEqual(expect.objectContaining({ name: "AgentRuntimeRequestError", status: 409, code: "agent_clarification_conflict", latestStateVersion: 5 }));
        expect(requestCount).toBe(1);
    } finally {
        globalThis.fetch = originalFetch;
    }
});
