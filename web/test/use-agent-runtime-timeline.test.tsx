import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import { useAgentRuntime } from "../src/components/canvas/use-agent-runtime";
import {
    AgentRuntimeRequestError,
    type AgentRuntimeClient,
    type AgentRuntimeEvent,
    type AgentRuntimeHandleStorage,
    type AgentRuntimeStartConfiguration,
    type AgentRuntimeView,
    type AgentThreadHistoryItem,
} from "../src/services/api/agent-runtime";

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

test("服务端 turns/items 是恢复真源且重复或乱序事件不会重复触发副作用", async () => {
    const running = runtimeView("running", 2, 2);
    const history = historyItem(running, [timelineItem("item-user-1", "user_message", { message: "生成短片" }, 1, 1)]);
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const received: AgentRuntimeEvent[] = [];
    const client = runtimeClient({
        listThreads: async () => ({ items: [history] }),
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client, (event) => received.push(event));

    expect(runtime?.turns[0]?.items[0]?.content.message).toBe("生成短片");
    const delta = uiItemEvent(3, "item.delta", { delta: "第一句", userVisible: true });
    const snapshot: AgentRuntimeEvent = {
        protocolVersion: 5,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 4,
        kind: "state.snapshot",
        payload: { status: "running", stateVersion: 2 },
        createdAt: "2026-08-18T00:00:03Z",
    };
    if (!handlers) throw new Error("Agent SSE 未建立订阅");
    await act(async () => {
        handlers?.onEvent(delta);
        handlers?.onEvent(delta);
        handlers?.onEvent({ ...delta, sequence: 2 });
        handlers?.onEvent(snapshot);
        handlers?.onEvent(snapshot);
    });
    await settle();

    expect(runtime?.events).toEqual([delta, snapshot]);
    expect(runtime?.lastSequence).toBe(4);
    expect(received).toEqual([delta, snapshot]);
});

test("活动 Run 刷新后从持久游标续传且忽略迟到重复事件", async () => {
    const running = runtimeView("running", 2, 4);
    let subscribedAfter = -1;
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const received: AgentRuntimeEvent[] = [];
    const restoredStorage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 4 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(running)] }),
        getRun: async () => running,
        subscribe: (_runId, afterSequence, nextHandlers) => {
            subscribedAfter = afterSequence;
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client, (event) => received.push(event), restoredStorage);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    const replayed = uiItemEvent(3, "item.delta", { delta: "已流出", userVisible: true });
    const fresh = uiItemEvent(5, "item.delta", { delta: "新增量", userVisible: true });
    await act(async () => {
        handlers?.onEvent(replayed);
        handlers?.onEvent(fresh);
    });

    expect(subscribedAfter).toBe(4);
    expect(runtime?.events).toEqual([fresh]);
    expect(runtime?.lastSequence).toBe(5);
    expect(received).toEqual([fresh]);
});

test("长流式回复保留完整气泡但不会把全部 delta 留在 React 事件窗口", async () => {
    const running = runtimeView("running", 2, 0);
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(running)] }),
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    await act(async () => {
        for (let sequence = 1; sequence <= 600; sequence += 1) {
            handlers?.onEvent(uiItemEvent(sequence, "item.delta", { delta: "x", userVisible: true }));
        }
    });

    expect(runtime?.conversation.items[0]?.text).toBe("x".repeat(600));
    expect(runtime?.events.length).toBeLessThanOrEqual(256);
});

test("活动运行再次发送走 steer，终态运行才创建新 Run", async () => {
    const running = runtimeView("running", 2, 2);
    const succeeded = runtimeView("succeeded", 3, 4);
    let steerCalls = 0;
    let startCalls = 0;
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(running)] }),
        getRun: async () => running,
        steer: async (_runId, input) => {
            steerCalls += 1;
            expect(input.message).toBe("加快节奏");
            expect(input.expectedStateVersion).toBe(2);
            return succeeded;
        },
        startRun: async () => {
            startCalls += 1;
            return running;
        },
    });
    await mount(client);

    await act(async () => {
        await runtime?.sendOrSteer("加快节奏", configuration);
    });
    expect(steerCalls).toBe(1);
    expect(startCalls).toBe(0);

    await act(async () => {
        await runtime?.sendOrSteer("开始下一轮", configuration);
    });
    expect(startCalls).toBe(1);
});

test("点击停止后在接口返回前立即忽略迟到的正文增量", async () => {
    const running = runtimeView("running", 4, 2);
    const interrupted = runtimeView("cancelled", 5, 3);
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    let finishInterrupt: (view: AgentRuntimeView) => void = () => undefined;
    const interruptResponse = new Promise<AgentRuntimeView>((resolve) => {
        finishInterrupt = resolve;
    });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(running)] }),
        getRun: async () => running,
        interrupt: async () => interruptResponse,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    let stopPromise: Promise<boolean> | undefined;
    await act(async () => {
        stopPromise = runtime?.interrupt();
        await Promise.resolve();
    });
    await act(async () => {
        handlers?.onEvent(uiItemEvent(3, "item.delta", { delta: "不应显示", userVisible: true }));
    });

    expect(runtime?.conversation.items).toEqual([]);

    await act(async () => {
        finishInterrupt(interrupted);
        await stopPromise;
    });
    expect(runtime?.view?.state.status).toBe("cancelled");
});

test("interrupt 使用当前版本，409 后刷新 Run 与 History 并保留明确冲突", async () => {
    const running = runtimeView("running", 4, 3);
    const refreshed = runtimeView("running", 6, 5);
    let getRunCalls = 0;
    let historyCalls = 0;
    const client = runtimeClient({
        listThreads: async () => {
            historyCalls += 1;
            return { items: [historyItem(running)] };
        },
        getRun: async () => {
            getRunCalls += 1;
            return getRunCalls === 1 ? running : refreshed;
        },
        interrupt: async (_runId, input) => {
            expect(input.expectedStateVersion).toBe(4);
            throw new AgentRuntimeRequestError("状态已变化", 409, "agent_interrupt_conflict", 6);
        },
    });
    await mount(client);

    await act(async () => {
        await runtime?.interrupt();
    });

    expect(runtime?.view?.state.stateVersion).toBe(6);
    expect(runtime?.error).toContain("停止请求未执行");
    expect(historyCalls).toBe(2);
});

test("中断后重读历史保留迟到 Artifact，且不会把 Run 改回运行中", async () => {
    const interrupted = runtimeView("cancelled", 5, 4);
    const baseItems = [timelineItem("item-user-1", "user_message", { message: "生成视频" }, 1, 1)];
    let includeArtifact = false;
    const client = runtimeClient({
        listThreads: async () => ({
            items: [
                historyItem(
                    interrupted,
                    includeArtifact
                        ? [...baseItems, timelineItem("item-artifact-1", "artifact", { artifactId: "artifact-1", kind: "video", planKey: "plan-1", planVersion: 1, resourceId: "resource-video-1", status: "succeeded" }, 2, 5)]
                        : baseItems,
                ),
            ],
        }),
        getRun: async () => interrupted,
    });
    await mount(client);
    includeArtifact = true;

    await act(async () => {
        await runtime?.reloadThreads();
    });

    expect(runtime?.turns[0]?.items.at(-1)?.content.resourceId).toBe("resource-video-1");
    expect(runtime?.view?.state.status).toBe("cancelled");
});

test("切换 Thread 会关闭旧订阅并清空旧事件", async () => {
    const first = runtimeView("running", 2, 2, "run-1", "thread-1");
    const second = runtimeView("succeeded", 3, 4, "run-2", "thread-2");
    const items = [historyItem(first), historyItem(second)];
    let unsubscribeCalls = 0;
    const client = runtimeClient({
        listThreads: async () => ({ items }),
        getRun: async (runId) => (runId === "run-1" ? first : second),
        subscribe: () => () => {
            unsubscribeCalls += 1;
        },
    });
    await mount(client);

    const secondThread = runtime?.threads[1];
    if (!secondThread) throw new Error("缺少第二个 Agent Thread");
    await act(async () => {
        await runtime?.selectThread(secondThread);
    });
    await settle();

    expect(unsubscribeCalls).toBe(1);
    expect(runtime?.selectedThreadId).toBe("thread-2");
    expect(runtime?.events).toEqual([]);
});

test("切换画布会取消旧画布已排队的历史刷新", async () => {
    const running = runtimeView("running", 2, 2);
    const historyCalls: string[] = [];
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const client = runtimeClient({
        listThreads: async (canvasId) => {
            historyCalls.push(canvasId);
            return { items: canvasId === "canvas-1" ? [historyItem(running)] : [] };
        },
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    await act(async () => handlers?.onEvent(uiItemEvent(3, "item.delta", { delta: "第一句", userVisible: true })));
    await act(async () => root?.render(createElement(Harness, { canvasId: "canvas-2", client })));
    await new Promise((resolve) => setTimeout(resolve, 160));
    await settle();

    expect(historyCalls).toEqual(["canvas-1", "canvas-2"]);
    expect(runtime?.threads).toEqual([]);
});

test("订阅协议错误保持为用户可见事实", async () => {
    const running = runtimeView("running", 2, 2);
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem(running)] }),
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");

    await act(async () => handlers?.onError(new Error("不受支持的 Agent UI 协议版本: 1")));

    expect(runtime?.error).toContain("协议版本");
    expect(runtime?.connection).toBe("idle");
});

const configuration: AgentRuntimeStartConfiguration = { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" };
const storage: AgentRuntimeHandleStorage = { load: async () => null, save: async () => undefined, clear: async () => undefined };

async function mount(client: AgentRuntimeClient, onRuntimeEvent?: (event: AgentRuntimeEvent) => void, handleStorage: AgentRuntimeHandleStorage = storage) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(Harness, { client, onRuntimeEvent, storage: handleStorage })));
    await settle();
}

function Harness({ canvasId = "canvas-1", client, onRuntimeEvent, storage: handleStorage = storage }: { canvasId?: string; client: AgentRuntimeClient; onRuntimeEvent?: (event: AgentRuntimeEvent) => void; storage?: AgentRuntimeHandleStorage }) {
    runtime = useAgentRuntime({ canvasId, client, storage: handleStorage, onRuntimeEvent });
    return createElement("div", { className: "agent-runtime-timeline-harness" });
}

function runtimeClient(patch: Partial<AgentRuntimeClient>): AgentRuntimeClient {
    const running = runtimeView("running", 2, 2);
    return {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: "thread-1", canvasId, status: "active" }),
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

function runtimeView(status: AgentRuntimeView["state"]["status"], stateVersion: number, sequence: number, runId = "run-1", threadId = "thread-1"): AgentRuntimeView {
    const succeeded = status === "succeeded" ? { expectedDelivery: { kind: "answer" as const, completionCriteria: [{ fact: "final_message" as const }] }, verification: { status: "satisfied" as const, rationale: "ok" }, finalMessage: "完成" } : {};
    return {
        run: { id: runId, threadId, actorUserId: "user-1", clientRequestId: `request-${runId}`, status, lastEventSequence: sequence, stateVersion, stepNumber: 1, maxSteps: 8, modelRecordId: "model-1", modelKey: "agent", toolSchemaVersion: 1, createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:01Z" },
        state: { stateVersion, stepNumber: 1, maxSteps: 8, status, clarificationHistory: [], userMessage: "生成短片", configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" }, ...succeeded },
    };
}

function historyItem(view: AgentRuntimeView, items = [timelineItem(`${view.run.id}-user`, "user_message", { message: view.state.userMessage }, 1, 1)]): AgentThreadHistoryItem {
    return {
        thread: { id: view.run.threadId, canvasId: "canvas-1", status: "active", createdAt: view.run.createdAt, updatedAt: view.run.updatedAt },
        activityAt: view.run.updatedAt,
        turns: [
            {
                run: { id: view.run.id, threadId: view.run.threadId, status: view.run.status, lastEventSequence: view.run.lastEventSequence, stateVersion: view.run.stateVersion, stepNumber: view.run.stepNumber, maxSteps: view.run.maxSteps, modelKey: view.run.modelKey, toolSchemaVersion: view.run.toolSchemaVersion, runtimeVersion: 1, policyVersion: 1, createdAt: view.run.createdAt, updatedAt: view.run.updatedAt, completedAt: view.run.completedAt },
                items,
            },
        ],
    };
}

function timelineItem(id: string, kind: "user_message" | "artifact", content: Record<string, unknown>, ordinal: number, sequence: number) {
    return { id, runId: "run-1", kind, status: "completed" as const, ordinal, sourceEventSequence: sequence, content, startedAt: "2026-08-18T00:00:00Z", completedAt: "2026-08-18T00:00:01Z", createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:01Z" };
}

function uiItemEvent(sequence: number, kind: "item.delta", payload: Record<string, unknown>): AgentRuntimeEvent {
    return { protocolVersion: 5, threadId: "thread-1", runId: "run-1", sequence, kind, itemId: "item-message-1", itemKind: "agent_message", payload, createdAt: "2026-08-18T00:00:02Z" };
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
