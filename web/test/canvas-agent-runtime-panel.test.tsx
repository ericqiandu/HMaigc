import "./setup-happy-dom";

import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentRuntimeClient, AgentRuntimeHandle, AgentRuntimeHandleStorage, AgentRuntimeView, AgentThreadHistoryItem } from "../src/services/api/agent-runtime";

let Panel: typeof import("../src/components/canvas/canvas-assistant-panel").CanvasAssistantPanel;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasAssistantPanel: Panel } = await import("../src/components/canvas/canvas-assistant-panel"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    window.localStorage.clear();
});

test("单一运行链回传真实选区、等待审批并展示验收后的最终消息", async () => {
    const calls: string[] = [];
    const selectionView = runtimeView("waiting_tool", {
        pendingToolCall: { toolCallId: "selection-1", toolName: "canvas.read_selection", actionVersion: 1, arguments: {} },
    });
    const approvalView = runtimeView("waiting_approval", {
        stateVersion: 3,
        pendingToolCall: { toolCallId: "apply-1", toolName: "canvas.apply_ops", actionVersion: 2, arguments: { baseRevision: 7, patch: { upsertNodes: [] } } },
    });
    const completedView = runtimeView("succeeded", {
        stateVersion: 4,
        stepNumber: 2,
        pendingToolCall: undefined,
        finalMessage: "画布整理已经完成。",
        verification: { status: "satisfied", rationale: "delivery evidence satisfies every criterion" },
    });
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => {
            calls.push("create-thread");
            return { id: "thread-1", canvasId: "canvas-1", status: "active" };
        },
        startRun: async (_threadId, input) => {
            calls.push(`start:${input.userMessage}`);
            return selectionView;
        },
        getRun: async () => selectionView,
        submitSelection: async (_runId, input) => {
            calls.push(`selection:${input.selection.revision}:${input.selection.nodeIds.join(",")}`);
            return approvalView;
        },
        submitApproval: async (_runId, input) => {
            calls.push(`approval:${input.decision}:${input.toolCallId}:${input.actionVersion}`);
            return completedView;
        },
        subscribe: () => () => undefined,
    };
    await mount(client);
    const textarea = document.querySelector("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    const valueSetter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
    if (!valueSetter) throw new Error("测试 DOM 缺少 textarea value setter");
    await act(async () => {
        valueSetter.call(textarea, "整理当前画布");
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
        textarea.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await act(async () => button("发送").click());
    await settle();
    expect(calls).toEqual(["create-thread", "start:整理当前画布", "selection:7:node-a,node-b"]);
    expect(document.body.textContent).toContain("等待确认");
    expect(document.body.textContent).toContain("canvas.apply_ops");
    await act(async () => button("批准执行").click());
    await settle();
    expect(calls.at(-1)).toBe("approval:approved:apply-1:2");
    expect(document.body.textContent).toContain("画布整理已经完成。");
    expect(document.body.textContent).toContain("交付已验收");
});

test("刷新时从持久句柄恢复运行并从已确认游标续接事件", async () => {
    const calls: string[] = [];
    const runningView = runtimeView("running", { stateVersion: 3, stepNumber: 2 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 9 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async () => runningView,
        getRun: async (runId) => {
            calls.push(`resume:${runId}`);
            return runningView;
        },
        submitSelection: async () => runningView,
        submitApproval: async () => runningView,
        subscribe: (_runId, afterSequence) => {
            calls.push(`subscribe:${afterSequence}`);
            return () => undefined;
        },
    };
    await mount(client, storage);
    expect(calls).toEqual(["resume:run-1", "subscribe:9"]);
    expect(document.body.textContent).toContain("第 2 / 8 步");
});

test("启动响应丢失后复用同一 clientRequestId 收敛运行", async () => {
    const calls: string[] = [];
    const runningView = runtimeView("running", { stateVersion: 2, stepNumber: 1, userMessage: "生成三镜头短片" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", lastSequence: 0, pendingRun: { clientRequestId: "request-stable", userMessage: "生成三镜头短片" } }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (threadId, input) => {
            calls.push(`${threadId}:${input.clientRequestId}:${input.userMessage}`);
            return runningView;
        },
        getRun: async () => runningView,
        submitSelection: async () => runningView,
        submitApproval: async () => runningView,
        subscribe: () => () => undefined,
    };
    await mount(client, storage);
    expect(calls).toEqual(["thread-1:request-stable:生成三镜头短片"]);
    expect(document.body.textContent).toContain("生成三镜头短片");
});

test("启动恢复再次失败时还原原指令并允许复用同一请求身份重试", async () => {
    const calls: string[] = [];
    const completedView = runtimeView("succeeded", {
        userMessage: "生成三镜头短片",
        finalMessage: "已完成",
        verification: { status: "satisfied", rationale: "ok" },
    });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", lastSequence: 0, pendingRun: { clientRequestId: "request-stable", userMessage: "生成三镜头短片" } }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (_threadId, input) => {
            calls.push(input.clientRequestId);
            if (calls.length === 1) throw new Error("启动结果仍不可确认");
            return completedView;
        },
        getRun: async () => completedView,
        submitSelection: async () => completedView,
        submitApproval: async () => completedView,
        subscribe: () => () => undefined,
    };
    await mount(client, storage);
    const textarea = document.querySelector<HTMLTextAreaElement>("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    expect(textarea.value).toBe("生成三镜头短片");
    expect(document.body.textContent).toContain("启动结果仍不可确认");
    await act(async () => button("发送").click());
    await settle();
    expect(calls).toEqual(["request-stable", "request-stable"]);
    expect(document.body.textContent).toContain("已完成");
});

test("切换画布时先清空上一画布的运行事实", async () => {
    const oldView = runtimeView("running", { userMessage: "只属于画布一的任务" });
    const storage: AgentRuntimeHandleStorage = {
        load: async (canvasId) => (canvasId === "canvas-1" ? { threadId: "thread-1", activeRunId: "run-1", lastSequence: 3 } : null),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: `thread-${canvasId}`, canvasId, status: "active" }),
        startRun: async () => oldView,
        getRun: async () => oldView,
        submitSelection: async () => oldView,
        submitApproval: async () => oldView,
        subscribe: () => () => undefined,
    };
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const render = (projectId: string) =>
        createElement(
            App,
            null,
            createElement(Panel, {
                projectId,
                canvasRevision: 7,
                selectedNodeIds: new Set<string>(),
                closing: false,
                onCollapse: () => undefined,
                runtimeClient: client,
                runtimeStorage: storage,
            }),
        );
    await act(async () => root?.render(render("canvas-1")));
    await settle();
    expect(document.body.textContent).toContain("只属于画布一的任务");
    await act(async () => root?.render(render("canvas-2")));
    await settle();
    expect(document.body.textContent).not.toContain("只属于画布一的任务");
    expect(document.body.textContent).toContain("等待任务");
});

test("首页已确认的创作请求只提交一次并在成功后消费", async () => {
    const calls: string[] = [];
    const completedView = runtimeView("succeeded", { userMessage: "生成东方幻想短片", finalMessage: "已完成", verification: { status: "satisfied", rationale: "ok" } });
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (_threadId, input) => {
            calls.push(`start:${input.userMessage}`);
            return completedView;
        },
        getRun: async () => completedView,
        submitSelection: async () => completedView,
        submitApproval: async () => completedView,
        subscribe: () => () => undefined,
    };
    await mount(client, undefined, {
        agentLaunchRequest: { id: "launch-1", source: "home", prompt: "生成东方幻想短片", createdAt: "2026-08-15T00:00:00Z" },
        onAgentLaunchHandled: (id: string) => calls.push(`handled:${id}`),
    });
    expect(calls).toEqual(["start:生成东方幻想短片", "handled:launch-1"]);
});

test("选区事实提交失败后提供显式重试而不是伪装继续", async () => {
    let attempts = 0;
    const selectionView = runtimeView("waiting_tool", { pendingToolCall: { toolCallId: "selection-1", toolName: "canvas.read_selection", actionVersion: 1, arguments: {} } });
    const runningView = runtimeView("running", { stateVersion: 3, stepNumber: 2, pendingToolCall: undefined });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 1 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async () => selectionView,
        getRun: async () => selectionView,
        submitSelection: async () => {
            attempts += 1;
            if (attempts === 1) throw new Error("选区版本冲突");
            return runningView;
        },
        submitApproval: async () => runningView,
        subscribe: () => () => undefined,
    };
    await mount(client, storage);
    expect(document.body.textContent).toContain("选区版本冲突");
    expect(attempts).toBe(1);
    await act(async () => button("重试选区提交").click());
    await settle();
    expect(attempts).toBe(2);
    expect(document.body.textContent).not.toContain("选区版本冲突");
});

test("没有本地句柄时采用服务端最近运行并保存恢复身份", async () => {
    const saved: AgentRuntimeHandle[] = [];
    const running = runtimeView("running", { userMessage: "正在生成" }, { id: "run-active", threadId: "thread-active", lastEventSequence: 6 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => null,
        save: async (_canvasId, handle) => saved.push(handle),
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-active", running, "2026-08-15T04:00:00Z")] }),
        subscribe: (_runId, afterSequence) => {
            expect(afterSequence).toBe(6);
            return () => undefined;
        },
    });
    await mount(client, storage);
    expect(document.body.textContent).toContain("正在生成");
    expect(saved.at(-1)).toMatchObject({ threadId: "thread-active", activeRunId: "run-active", lastSequence: 6 });
});

test("历史对话可切换到旧终态运行并保存所选 Thread", async () => {
    const saved: AgentRuntimeHandle[] = [];
    const current = runtimeView("succeeded", { userMessage: "当前对话", finalMessage: "当前结果", verification: { status: "satisfied", rationale: "ok" } }, { id: "run-current", threadId: "thread-current" });
    const old = runtimeView("succeeded", { userMessage: "旧对话", finalMessage: "旧结果", verification: { status: "satisfied", rationale: "ok" } }, { id: "run-old", threadId: "thread-old" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => null,
        save: async (_canvasId, handle) => saved.push(handle),
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-current", current, "2026-08-15T04:00:00Z"), historyItem("thread-old", old, "2026-08-15T03:00:00Z")] }),
    });
    await mount(client, storage);
    await act(async () => button("历史对话").click());
    await act(async () => button("旧对话").click());
    await settle();
    expect(document.body.textContent).toContain("旧结果");
    expect(saved.at(-1)).toMatchObject({ threadId: "thread-old" });
});

test("历史接口失败独立显示但不阻断本地运行恢复", async () => {
    const running = runtimeView("running", { userMessage: "本地恢复中的任务" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 3 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => {
            throw new Error("历史服务暂不可用");
        },
        getRun: async () => running,
    });
    await mount(client, storage);
    expect(document.body.textContent).toContain("本地恢复中的任务");
    await act(async () => button("历史对话").click());
    expect(document.body.textContent).toContain("历史服务暂不可用");
});

test("服务端历史恢复成功也不会吞掉本地句柄读取错误", async () => {
    const completed = runtimeView("succeeded", { userMessage: "服务端恢复的对话", finalMessage: "服务端结果", verification: { status: "satisfied", rationale: "ok" } });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => {
            throw new Error("本地句柄损坏");
        },
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({ listThreads: async () => ({ items: [historyItem("thread-1", completed, "2026-08-15T04:00:00Z")] }) });
    await mount(client, storage);
    expect(document.body.textContent).toContain("服务端结果");
    expect(document.body.textContent).toContain("本地句柄损坏");
});

test("新运行启动后刷新历史且刷新失败不污染运行状态", async () => {
    let historyCalls = 0;
    const running = runtimeView("running", { userMessage: "启动后的任务" });
    const client = runtimeClient({
        listThreads: async () => {
            historyCalls += 1;
            if (historyCalls > 1) throw new Error("历史刷新失败");
            return { items: [] };
        },
        startRun: async () => running,
    });
    await mount(client);
    await setPrompt("启动后的任务");
    await act(async () => button("发送").click());
    await settle();
    expect(historyCalls).toBe(2);
    expect(document.body.textContent).toContain("启动后的任务");
    expect(document.body.textContent).not.toContain("运行失败");
    await act(async () => button("历史对话").click());
    expect(document.body.textContent).toContain("历史刷新失败");
});

async function mount(client: AgentRuntimeClient, storage?: AgentRuntimeHandleStorage, extra: Record<string, unknown> = {}) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () =>
        root?.render(
            createElement(
                App,
                null,
                createElement(Panel, {
                    projectId: "canvas-1",
                    canvasRevision: 7,
                    selectedNodeIds: new Set(["node-a", "node-b"]),
                    closing: false,
                    onCollapse: () => undefined,
                    runtimeClient: client,
                    runtimeStorage: storage,
                    ...extra,
                }),
            ),
        ),
    );
    await settle();
}

function runtimeView(status: AgentRuntimeView["state"]["status"], patch: Partial<AgentRuntimeView["state"]> = {}, runPatch: Partial<AgentRuntimeView["run"]> = {}): AgentRuntimeView {
    const state = {
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        status,
        userMessage: "整理当前画布",
        ...(status === "succeeded"
            ? {
                  expectedDelivery: { kind: "answer" as const, completionCriteria: [{ fact: "final_message" }] },
              }
            : {}),
        ...patch,
    } satisfies AgentRuntimeView["state"];
    return {
        run: {
            id: "run-1",
            threadId: "thread-1",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status,
            lastEventSequence: 1,
            stateVersion: state.stateVersion,
            stepNumber: state.stepNumber,
            maxSteps: state.maxSteps,
            modelRecordId: "model-1",
            modelKey: "agent-model",
            toolSchemaVersion: 1,
            createdAt: "2026-08-15T00:00:00Z",
            updatedAt: "2026-08-15T00:00:01Z",
            ...runPatch,
        },
        state,
    };
}

function runtimeClient(patch: Partial<AgentRuntimeClient> = {}): AgentRuntimeClient {
    const running = runtimeView("running");
    return {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: "thread-1", canvasId, status: "active" }),
        startRun: async () => running,
        getRun: async () => running,
        submitSelection: async () => running,
        submitApproval: async () => running,
        subscribe: () => () => undefined,
        ...patch,
    };
}

function historyItem(threadId: string, latestRun: AgentRuntimeView | null, activityAt: string): AgentThreadHistoryItem {
    return {
        thread: { id: threadId, canvasId: "canvas-1", status: "active", createdAt: "2026-08-15T00:00:00Z", updatedAt: activityAt },
        activityAt,
        latestRun,
    };
}

async function setPrompt(value: string) {
    const textarea = document.querySelector("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    const valueSetter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
    if (!valueSetter) throw new Error("测试 DOM 缺少 textarea value setter");
    await act(async () => {
        valueSetter.call(textarea, value);
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
        textarea.dispatchEvent(new Event("change", { bubbles: true }));
    });
}

function button(label: string) {
    const compactLabel = label.replace(/\s+/g, "");
    const match = [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "").includes(compactLabel) || item.getAttribute("aria-label")?.replace(/\s+/g, "").includes(compactLabel));
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}`);
    return match;
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
