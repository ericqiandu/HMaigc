import { expect, test } from "bun:test";

import { parseAgentRuntimeEvent, parseAgentRuntimeView } from "../src/services/api/agent-runtime";

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
    },
    userMessage: "整理当前画布",
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
