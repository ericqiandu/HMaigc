import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { agentRuntimeStatusLabel, useAgentRuntime } from "../src/components/canvas/use-agent-runtime";
import { AgentRuntimeRequestError, type AgentRuntimeClient, type AgentRuntimeHandleStorage, type AgentRuntimeView } from "../src/services/api/agent-runtime";

let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;
let runtime: ReturnType<typeof useAgentRuntime> | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    runtime = null;
    document.body.replaceChildren();
});

test("追问回答使用当前 stateVersion 并采用服务端新状态", async () => {
    const calls: Array<Record<string, unknown>> = [];
    const waiting = waitingView(4);
    const running = runningView(5);
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(waiting)] }),
        getRun: async () => waiting,
        submitClarificationResponse: async (runId, requestId, input) => {
            calls.push({ runId, requestId, ...input });
            return running;
        },
    });
    await mount(client);

    await act(async () => {
        await runtime?.submitClarificationResponse({ requestId: "request-1", questionId: "question-1", answer: { selectedOptionIds: ["option-1"], customText: "", skipped: false }, complete: true });
    });

    expect(calls).toEqual([{ runId: "run-1", requestId: "request-1", expectedStateVersion: 4, questionId: "question-1", answer: { selectedOptionIds: ["option-1"], customText: "", skipped: false }, complete: true }]);
    expect(runtime?.view?.state.status).toBe("running");
});

test("追问回答发生 409 时只刷新一次并要求用户核对后重试", async () => {
    let submitCalls = 0;
    let refreshCalls = 0;
    const waiting = waitingView(4);
    const refreshed = waitingView(6);
    let initialLoad = true;
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(waiting)] }),
        getRun: async () => {
            if (initialLoad) {
                initialLoad = false;
                return waiting;
            }
            refreshCalls += 1;
            return refreshed;
        },
        submitClarificationResponse: async () => {
            submitCalls += 1;
            throw new AgentRuntimeRequestError("状态版本冲突", 409, "agent_state_conflict", 6);
        },
    });
    await mount(client);

    await act(async () => {
        await runtime?.submitClarificationResponse({ requestId: "request-1", questionId: "question-1", answer: { selectedOptionIds: ["option-1"], customText: "", skipped: false }, complete: true });
    });

    expect(submitCalls).toBe(1);
    expect(refreshCalls).toBe(1);
    expect(runtime?.view?.state.stateVersion).toBe(6);
    expect(runtime?.error).toContain("其他页面");
    expect(runtime?.error).toContain("核对后重试");
});

test("waiting_input 显示为询问中而不是执行中", () => {
    expect([
        agentRuntimeStatusLabel("queued"),
        agentRuntimeStatusLabel("running"),
        agentRuntimeStatusLabel("waiting_input"),
        agentRuntimeStatusLabel("waiting_tool"),
        agentRuntimeStatusLabel("waiting_approval"),
        agentRuntimeStatusLabel("succeeded"),
        agentRuntimeStatusLabel("failed"),
        agentRuntimeStatusLabel("cancelled"),
    ]).toEqual(["准备中", "思考中", "询问中", "执行中", "等待确认", "已完成", "已失败", "已取消"]);
});

test("等待用户输入或审批时不保持无效的实时订阅", async () => {
    for (const status of ["waiting_input", "waiting_approval"] as const) {
        let subscribeCalls = 0;
        const paused = status === "waiting_input" ? waitingView(4) : view("waiting_approval", 4, {});
        const client = runtimeClient({
            listThreads: async () => ({ items: [historyItem(paused)] }),
            getRun: async () => paused,
            subscribe: () => {
                subscribeCalls += 1;
                return () => undefined;
            },
        });
        await mount(client);

        expect(subscribeCalls).toBe(0);
        expect(runtime?.connection).toBe("idle");

        if (root) await act(async () => root?.unmount());
        root = null;
        runtime = null;
        document.body.replaceChildren();
    }
});

async function mount(client: AgentRuntimeClient) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(Harness, { client })));
    await settle();
}

function Harness({ client }: { client: AgentRuntimeClient }) {
    runtime = useAgentRuntime({ canvasId: "canvas-1", client, storage });
    return createElement("div", { className: "agent-runtime-test-harness" });
}

const storage: AgentRuntimeHandleStorage = {
    load: async () => null,
    save: async () => undefined,
    clear: async () => undefined,
};

function runtimeClient(patch: Partial<AgentRuntimeClient>): AgentRuntimeClient {
    const running = runningView(5);
    return {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async () => running,
        getRun: async () => running,
        steer: async () => running,
        interrupt: async () => running,
        submitApproval: async () => running,
        submitClarificationResponse: async () => running,
        subscribe: () => () => undefined,
        ...patch,
    };
}

function thread() {
    return { id: "thread-1", canvasId: "canvas-1", status: "active" as const, createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:01Z" };
}

function historyItem(runtimeView: AgentRuntimeView) {
    return {
        thread: thread(),
        activityAt: "2026-08-18T00:00:01Z",
        turns: [
            {
                run: {
                    id: runtimeView.run.id,
                    threadId: runtimeView.run.threadId,
                    status: runtimeView.run.status,
                    lastEventSequence: runtimeView.run.lastEventSequence,
                    stateVersion: runtimeView.run.stateVersion,
                    stepNumber: runtimeView.run.stepNumber,
                    maxSteps: runtimeView.run.maxSteps,
                    modelKey: runtimeView.run.modelKey,
                    toolSchemaVersion: runtimeView.run.toolSchemaVersion,
                    runtimeVersion: 1,
                    policyVersion: 1,
                    createdAt: runtimeView.run.createdAt,
                    updatedAt: runtimeView.run.updatedAt,
                },
                items: [
                    {
                        id: "item-user-1",
                        runId: runtimeView.run.id,
                        kind: "user_message" as const,
                        status: "completed" as const,
                        ordinal: 1,
                        sourceEventSequence: 1,
                        content: { message: runtimeView.state.userMessage },
                        startedAt: runtimeView.run.createdAt,
                        completedAt: runtimeView.run.createdAt,
                        createdAt: runtimeView.run.createdAt,
                        updatedAt: runtimeView.run.createdAt,
                    },
                ],
            },
        ],
    };
}

function waitingView(stateVersion: number): AgentRuntimeView {
    const delivery = { kind: "answer" as const, completionCriteria: [{ fact: "final_message" as const }] };
    return view("waiting_input", stateVersion, {
        expectedDelivery: delivery,
        pendingClarification: {
            request: {
                requestId: "request-1",
                expectedDelivery: delivery,
                questions: [{ id: "question-1", prompt: "选择广告时长", type: "single_choice", options: [{ id: "option-1", label: "30 秒" }, { id: "option-2", label: "60 秒" }], allowCustomAnswer: false }],
            },
            answers: [],
        },
    });
}

function runningView(stateVersion: number) {
    return view("running", stateVersion, {});
}

function view(status: AgentRuntimeView["state"]["status"], stateVersion: number, patch: Partial<AgentRuntimeView["state"]>): AgentRuntimeView {
    return {
        run: { id: "run-1", threadId: "thread-1", actorUserId: "user-1", clientRequestId: "client-1", status, lastEventSequence: 1, stateVersion, stepNumber: 1, maxSteps: 8, modelRecordId: "model-1", modelKey: "agent", toolSchemaVersion: 1, createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:01Z" },
        state: { stateVersion, stepNumber: 1, maxSteps: 8, status, clarificationHistory: [], userMessage: "生成广告", configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" }, ...patch },
    };
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
