import "../../../test/setup-happy-dom";

import assert from "node:assert/strict";
import { afterEach, before, test } from "node:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage, AgentRuntimeView } from "../../services/api/agent-runtime";
import { useAgentRuntime } from "./use-agent-runtime";

let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;
let runtime: ReturnType<typeof useAgentRuntime> | null = null;

before(async () => {
    ({ createRoot } = await import("react-dom/client"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    runtime = null;
    document.body.replaceChildren();
});

test("刷新后从持久游标续传且不会重复应用旧事件", async () => {
    const restored = runtimeView("running", 4);
    let subscribedAfter = -1;
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const received: AgentRuntimeEvent[] = [];
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 4 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [] }),
        getRun: async () => restored,
        subscribe: (_runId, afterSequence, nextHandlers) => {
            subscribedAfter = afterSequence;
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client, storage, (event) => received.push(event));
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    const oldEvent = event(4, "旧事件");
    const newEvent = event(5, "新事件");
    await act(async () => {
        handlers?.onEvent(oldEvent);
        handlers?.onEvent(newEvent);
    });

    assert.equal(subscribedAfter, 4);
    assert.deepEqual(runtime?.events, [newEvent]);
    assert.deepEqual(received, [newEvent]);
});

test("新建网站 Agent 运行使用足够完成媒体链路与一次冲突重试的决策预算", async () => {
    let submittedMaxSteps = 0;
    const storage: AgentRuntimeHandleStorage = {
        load: async () => null,
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        startRun: async (_threadId, input) => {
            submittedMaxSteps = input.maxSteps;
            return runtimeView("running", 1);
        },
    });
    await mount(client, storage, () => undefined);

    await act(async () => {
        await runtime?.submit("生成短片", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" });
    });

    assert.equal(submittedMaxSteps, 24);
});

async function mount(client: AgentRuntimeClient, storage: AgentRuntimeHandleStorage, onRuntimeEvent: (event: AgentRuntimeEvent) => void) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(Harness, { client, storage, onRuntimeEvent })));
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}

function Harness({ client, storage, onRuntimeEvent }: { client: AgentRuntimeClient; storage: AgentRuntimeHandleStorage; onRuntimeEvent: (event: AgentRuntimeEvent) => void }) {
    runtime = useAgentRuntime({ canvasId: "canvas-1", client, storage, onRuntimeEvent });
    return createElement("div", { className: "agent-runtime-reconnect-harness" });
}

function runtimeClient(patch: Partial<AgentRuntimeClient>): AgentRuntimeClient {
    const view = runtimeView("running", 4);
    return {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: "thread-1", canvasId, status: "active" }),
        startRun: async () => view,
        getRun: async () => view,
        steer: async () => view,
        interrupt: async () => view,
        submitApproval: async () => view,
        submitClarificationResponse: async () => view,
        subscribe: () => () => undefined,
        ...patch,
    };
}

function runtimeView(status: AgentRuntimeView["state"]["status"], sequence: number): AgentRuntimeView {
    return {
        run: {
            id: "run-1",
            threadId: "thread-1",
            reasoningHost: "managed",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status,
            lastEventSequence: sequence,
            stateVersion: 2,
            stepNumber: 1,
            maxSteps: 8,
            modelRecordId: "model-1",
            modelKey: "agent",
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
            status,
            clarificationHistory: [],
            userMessage: "生成短片",
            configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" },
        },
    };
}

function event(sequence: number, delta: string): AgentRuntimeEvent {
    return {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence,
        kind: "item.delta",
        itemId: "item-1",
        itemKind: "agent_message",
        payload: { delta, userVisible: true },
        createdAt: "2026-09-01T00:00:02Z",
    };
}
