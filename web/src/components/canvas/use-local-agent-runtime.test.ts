import "../../../test/setup-happy-dom";

import assert from "node:assert/strict";
import { afterEach, before, test } from "node:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentLocalRuntimeClient, AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeView } from "@/services/api/agent-runtime";
import type { LocalAgentClientPort, LocalAgentToolResultInput } from "@/services/local-agent/local-agent-client";
import type { LocalAgentAuthoritativeToolResult } from "@/services/local-agent/local-agent-bridge";
import type { LocalAgentEvent } from "@/services/local-agent/local-agent-contracts";
import type { LocalAgentConnection, LocalAgentSessionStore } from "@/services/local-agent/local-agent-session";

import { useLocalAgentRuntime } from "./use-local-agent-runtime";

let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;
let runtime: ReturnType<typeof useLocalAgentRuntime> | null = null;

before(async () => {
    ({ createRoot } = await import("react-dom/client"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    runtime = null;
    document.body.replaceChildren();
});

test("恢复会话配置时不自动连接或发送请求", async () => {
    let clientCreated = 0;
    const saved = { baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) };
    await mount({
        sessionStore: fakeSessionStore(saved),
        createClient: () => {
            clientCreated += 1;
            return fakeLocalClient();
        },
        runtimeClient: fakeRuntimeClient(),
    });
    assert.equal(runtime?.connection, "disconnected");
    assert.deepEqual(runtime?.savedConnection, saved);
    assert.equal(clientCreated, 0);
});

test("连接事件流未就绪时显式超时并保持断开", async () => {
    await mount({
        sessionStore: fakeSessionStore(null),
        connectionTimeoutMs: 5,
        createClient: () => fakeLocalClient({ streamEvents: async () => new Promise<void>(() => {}) }),
        runtimeClient: fakeRuntimeClient(),
    });
    let failure: unknown;
    await act(async () => {
        try {
            await runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) });
        } catch (cause) {
            failure = cause;
        }
    });
    assert.match(failure instanceof Error ? failure.message : "", /连接超时/);
    assert.equal(runtime?.connection, "disconnected");
});

test("已连接事件流异常中断时保留 Agent 上下文并展示底层原因", async () => {
    let rejectStream: ((cause: Error) => void) | undefined;
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            onEvent({ kind: "connected" });
            await new Promise<void>((_resolve, reject) => {
                rejectStream = reject;
            });
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));

    await act(async () => {
        rejectStream?.(new Error("network error"));
        await flushAsyncWork();
    });

    assert.equal(runtime?.connection, "disconnected");
    assert.equal(runtime?.error, "本机 Agent 事件流已断开：network error");
});

test("显式连接后建立事件流并由同一桥接链提交本机 turn", async () => {
    const calls: string[] = [];
    let eventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    const local = fakeLocalClient({
        health: async () => {
            calls.push("health");
            return { version: "0.1.0", protocolVersion: 1, ready: true };
        },
        streamEvents: async (_signal, onEvent) => {
            calls.push("events");
            eventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
        startTurn: async () => {
            calls.push("local.start");
            return { threadId: "thread-1", turnId: "turn-1" };
        },
    });
    const backend = fakeLocalRuntimeClient({
        startRun: async () => {
            calls.push("backend.start");
            return runtimeView();
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: backend,
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    assert.ok(eventHandler);
    assert.equal(runtime?.connection, "connected");
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.deepEqual(calls, ["health", "events", "local.start", "backend.start"]);
    assert.equal(runtime?.view?.run.reasoningHost, "local_codex");
});

test("新建本机 Agent 审计运行使用足够完成媒体链路与一次冲突重试的决策预算", async () => {
    let submittedMaxSteps = 0;
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
    });
    const backend = fakeLocalRuntimeClient({
        startRun: async (input) => {
            submittedMaxSteps = input.maxSteps;
            return runtimeView();
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: backend,
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));

    await act(async () => runtime?.submit("生成短片", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));

    assert.equal(submittedMaxSteps, 24);
});

test("后端拒绝本机最终决策时向用户暴露权威失败原因", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
    });
    const failed = runtimeView({ status: "failed", stateVersion: 3 });
    failed.state.failureCode = "step_budget_exhausted";
    const backend = fakeLocalRuntimeClient({ submitDecision: async () => failed });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: backend,
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("生成短片", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(localEventHandler);

    await act(async () => {
        localEventHandler?.({
            kind: "final_decision",
            threadId: "thread-1",
            turnId: "turn-1",
            message: "已成功生成短片",
            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
        });
        await flushAsyncWork();
    });

    assert.equal(runtime?.bridgeState.kind, "failed");
    assert.equal(runtime?.error, "step_budget_exhausted");
});

test("本机事件处理短暂失败后成功收口会清除旧错误", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
    });
    const succeeded = runtimeView({ status: "succeeded", stateVersion: 3, lastEventSequence: 3 });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient({ submitDecision: async () => succeeded }),
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(localEventHandler);

    await act(async () => {
        localEventHandler?.({ kind: "turn_completed", threadId: "wrong-thread", turnId: "wrong-turn", event: { type: "turn.completed" } });
        await flushAsyncWork();
    });
    assert.match(runtime?.error ?? "", /事件归属冲突/);

    await act(async () => {
        localEventHandler?.({
            kind: "final_decision",
            threadId: "thread-1",
            turnId: "turn-1",
            message: "只读核验完成",
            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
        });
        localEventHandler?.({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });
        await flushAsyncWork();
    });

    assert.equal(runtime?.bridgeState.kind, "idle");
    assert.equal(runtime?.error, "");
});

test("后端权威事件刷新工具结果并只向本机 Codex 交付一次", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    let backendHandlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | undefined;
    const deliveries: LocalAgentToolResultInput[] = [];
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
        deliverToolResult: async (input) => {
            deliveries.push(input);
        },
    });
    const waiting = runtimeView({ status: "waiting_tool", stateVersion: 3, lastEventSequence: 2, pendingTool: true });
    const completed = runtimeView({
        status: "running",
        stateVersion: 4,
        lastEventSequence: 3,
        lastToolResult: { toolCallId: "tool-1", actionVersion: 1, succeeded: true, output: { revision: 12 } },
    });
    const localRuntime = fakeLocalRuntimeClient({ submitDecision: async () => waiting });
    const backend = fakeRuntimeClient({
        getRun: async () => completed,
        subscribe: (_runId, _afterSequence, handlers) => {
            backendHandlers = handlers;
            handlers.onOpen?.();
            return () => undefined;
        },
    });
    const authoritativeEvents: AgentRuntimeEvent[] = [];
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: localRuntime,
        runtimeClient: backend,
        onRuntimeEvent: (event) => authoritativeEvents.push(event),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(localEventHandler);
    assert.ok(backendHandlers);

    const toolCall: LocalAgentEvent = {
        protocolVersion: 1,
        kind: "tool_call",
        requestId: "tool-1",
        threadId: "thread-1",
        turnId: "turn-1",
        toolName: "canvas.read",
        arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
        expectedDelivery: { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision" }] },
        createdAt: "2026-09-01T00:00:00.000Z",
    };
    await act(async () => {
        localEventHandler?.(toolCall);
        await flushAsyncWork();
    });
    assert.equal(deliveries.length, 0);

    const runtimeEvent: AgentRuntimeEvent = {
        protocolVersion: 5,
        threadId: "audit-thread-1",
        runId: "run-1",
        sequence: 3,
        createdAt: "2026-09-01T00:00:01.000Z",
        kind: "state.snapshot",
        payload: { status: "running", stateVersion: 4 },
    };
    await act(async () => {
        backendHandlers?.onEvent(runtimeEvent);
        backendHandlers?.onEvent(runtimeEvent);
        await flushAsyncWork();
    });
    assert.deepEqual(authoritativeEvents, [runtimeEvent]);
    assert.equal(deliveries.length, 1);
    assert.deepEqual(deliveries[0]?.output, { revision: 12 });
});

test("连续本机 turn 为新后端 Run 重置事件游标", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    let turnNumber = 0;
    let runNumber = 0;
    const subscriptions: Array<{ runId: string; afterSequence: number }> = [];
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
        startTurn: async () => {
            turnNumber += 1;
            return { threadId: "thread-1", turnId: `turn-${turnNumber}` };
        },
    });
    const localRuntime = fakeLocalRuntimeClient({
        startRun: async () => {
            runNumber += 1;
            return runtimeView({ runId: `run-${runNumber}`, lastEventSequence: runNumber === 1 ? 19 : 3 });
        },
        submitDecision: async () =>
            runtimeView({
                runId: `run-${runNumber}`,
                status: "succeeded",
                stateVersion: 4,
                lastEventSequence: runNumber === 1 ? 20 : 4,
            }),
    });
    const backend = fakeRuntimeClient({
        subscribe: (runId, afterSequence, handlers) => {
            subscriptions.push({ runId, afterSequence });
            handlers.onOpen?.();
            return () => undefined;
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: localRuntime,
        runtimeClient: backend,
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("第一轮", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    await act(async () => {
        await flushAsyncWork();
    });

    await act(async () => {
        localEventHandler?.({
            kind: "final_decision",
            threadId: "thread-1",
            turnId: "turn-1",
            message: "第一轮完成",
            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
        });
        localEventHandler?.({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });
        await flushAsyncWork();
    });

    await act(async () => runtime?.submit("第二轮", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    await act(async () => {
        await flushAsyncWork();
    });

    assert.deepEqual(subscriptions, [
        { runId: "run-1", afterSequence: 19 },
        { runId: "run-2", afterSequence: 3 },
    ]);
});

test("后端权威事件流发生致命错误时取消本机 turn", async () => {
    let backendHandlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | undefined;
    let cancellations = 0;
    const local = fakeLocalClient({
        cancelTurn: async () => {
            cancellations += 1;
        },
    });
    const backend = fakeRuntimeClient({
        subscribe: (_runId, _afterSequence, handlers) => {
            backendHandlers = handlers;
            handlers.onOpen?.();
            return () => undefined;
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient(),
        runtimeClient: backend,
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(backendHandlers);
    await act(async () => {
        backendHandlers?.onError(new Error("后端事件协议损坏"));
        await flushAsyncWork();
    });
    assert.equal(cancellations, 1);
    assert.equal(runtime?.bridgeState.kind, "failed");
    assert.match(runtime?.error ?? "", /后端事件协议损坏/);
});

test("本机事件流断开且取消失败时显式展示取消错误且不产生未处理拒绝", async () => {
    let rejectStream: ((cause: Error) => void) | undefined;
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            onEvent({ kind: "connected" });
            await new Promise<void>((_resolve, reject) => {
                rejectStream = reject;
            });
        },
        cancelTurn: async () => {
            throw new Error("cancel transport failed");
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient(),
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));

    await act(async () => {
        rejectStream?.(new Error("local stream failed"));
        await flushAsyncWork();
    });

    assert.equal(runtime?.connection, "disconnected");
    assert.equal(runtime?.bridgeState.kind, "failed");
    assert.match(runtime?.error ?? "", /cancel transport failed/);
});

test("用户断开连接时取消失败会保留连接并显式报错", async () => {
    const local = fakeLocalClient({
        cancelTurn: async () => {
            throw new Error("cancel transport failed");
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient(),
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));

    let failure: unknown;
    await act(async () => {
        try {
            await runtime?.disconnect();
        } catch (cause) {
            failure = cause;
        }
    });

    assert.match(failure instanceof Error ? failure.message : "", /cancel transport failed/);
    assert.match(runtime?.error ?? "", /cancel transport failed/);
    assert.equal(runtime?.connection, "connected");
});

test("本机审批继续复用网站审批端点并把权威结果交回桥接器", async () => {
    const approved = runtimeView({ stateVersion: 5, lastToolResult: { toolCallId: "tool-1", actionVersion: 1, succeeded: true, output: { revision: 9 } } });
    let approvalCalls = 0;
    const backend = fakeRuntimeClient({
        submitApproval: async () => {
            approvalCalls += 1;
            return approved;
        },
    });
    await mount({ sessionStore: fakeSessionStore(null), createClient: () => fakeLocalClient(), runtimeClient: backend });
    await assert.rejects(() => runtime?.decideApproval({ toolCallId: "tool-1", actionVersion: 1, proposalHash: "hash", decision: "approved" }) ?? Promise.reject(new Error("hook missing")), /当前没有等待审批的本机 Agent Run/);
    assert.equal(approvalCalls, 0);
});

test("批准接口直接返回的权威工具结果会同步通知当前页面", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    const results: LocalAgentAuthoritativeToolResult[] = [];
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
    });
    const waiting = runtimeView({ status: "waiting_approval", stateVersion: 3, pendingApproval: true });
    const approved = runtimeView({
        status: "running",
        stateVersion: 5,
        lastToolResult: { toolCallId: "tool-1", actionVersion: 1, succeeded: true, output: { revision: 9 } },
    });
    const backend = fakeRuntimeClient({ submitApproval: async () => approved });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient({ submitDecision: async () => waiting }),
        runtimeClient: backend,
        onToolResult: (result) => results.push(result),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(localEventHandler);
    await act(async () => {
        localEventHandler?.({
            protocolVersion: 1,
            kind: "tool_call",
            requestId: "tool-1",
            threadId: "thread-1",
            turnId: "turn-1",
            toolName: "canvas.read",
            arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
            expectedDelivery: { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision" }] },
            createdAt: "2026-09-01T00:00:00.000Z",
        });
        await flushAsyncWork();
    });
    assert.equal(runtime?.view?.state.status, "waiting_approval");
    await act(async () => runtime?.decideApproval({ toolCallId: "tool-1", actionVersion: 1, proposalHash: "hash", decision: "approved" }));
    assert.deepEqual(results, [{ toolName: "canvas.read", succeeded: true, output: { revision: 9 } }]);
});

test("本机 turn 已结束后迟到的后端事件只刷新视图而不再交给已释放的桥接器", async () => {
    let localEventHandler: Parameters<LocalAgentClientPort["streamEvents"]>[1] | undefined;
    let backendHandlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | undefined;
    const terminal = runtimeView({ status: "succeeded", stateVersion: 4, lastEventSequence: 2 });
    const lateTerminal = runtimeView({ status: "succeeded", stateVersion: 5, lastEventSequence: 3 });
    const local = fakeLocalClient({
        streamEvents: async (_signal, onEvent) => {
            localEventHandler = onEvent;
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
    });
    const backend = fakeRuntimeClient({
        getRun: async () => lateTerminal,
        subscribe: (_runId, _afterSequence, handlers) => {
            backendHandlers = handlers;
            handlers.onOpen?.();
            return () => undefined;
        },
    });
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () => local,
        localRuntimeClient: fakeLocalRuntimeClient({ submitDecision: async () => terminal }),
        runtimeClient: backend,
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));
    assert.ok(localEventHandler);
    assert.ok(backendHandlers);

    await act(async () => {
        localEventHandler?.({
            kind: "final_decision",
            threadId: "thread-1",
            turnId: "turn-1",
            message: "已完成",
            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
        });
        localEventHandler?.({ kind: "turn_completed", threadId: "thread-1", turnId: "turn-1", event: { type: "turn.completed" } });
        await flushAsyncWork();
    });

    await act(async () => {
        backendHandlers?.onEvent({
            protocolVersion: 5,
            threadId: "audit-thread-1",
            runId: "run-1",
            sequence: 3,
            createdAt: "2026-09-01T00:00:02.000Z",
            kind: "state.snapshot",
            payload: { status: "succeeded", stateVersion: 5 },
        });
        await flushAsyncWork();
    });

    assert.equal(runtime?.error, "");
    assert.equal(runtime?.view?.state.status, "succeeded");
});

test("工作区卸载时同时取消本机 turn 并终止后端审计 Run", async () => {
    let cancellations = 0;
    let interruptions = 0;
    await mount({
        sessionStore: fakeSessionStore(null),
        createClient: () =>
            fakeLocalClient({
                cancelTurn: async () => {
                    cancellations += 1;
                },
            }),
        localRuntimeClient: fakeLocalRuntimeClient({
            interrupt: async () => {
                interruptions += 1;
                return runtimeView({ status: "cancelled", stateVersion: 3 });
            },
        }),
        runtimeClient: fakeRuntimeClient(),
    });
    await act(async () => runtime?.connect({ baseUrl: "http://127.0.0.1:17371", token: "ab".repeat(32) }));
    await act(async () => runtime?.submit("读取画布", { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" }, []));

    await act(async () => {
        root?.unmount();
        root = null;
        await flushAsyncWork();
    });

    assert.equal(cancellations, 1);
    assert.equal(interruptions, 1);
});

type HookOptions = Parameters<typeof useLocalAgentRuntime>[0];
type HarnessOptions = Omit<HookOptions, "canvasId"> & { canvasId?: string };

async function mount(options: HarnessOptions) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(Harness, { options: { ...options, canvasId: options.canvasId ?? "canvas-1" } })));
    await act(async () => {
        await Promise.resolve();
    });
}

function Harness({ options }: { options: HookOptions }) {
    runtime = useLocalAgentRuntime(options);
    return createElement("div", { className: "local-agent-runtime-harness" });
}

function fakeSessionStore(saved: LocalAgentConnection | null): LocalAgentSessionStore {
    let value = saved;
    return {
        load: () => value,
        save: (next) => {
            value = next;
        },
        clear: () => {
            value = null;
        },
    };
}

function fakeLocalClient(patch: Partial<LocalAgentClientPort> = {}): LocalAgentClientPort {
    return {
        health: async () => ({ version: "0.1.0", protocolVersion: 1, ready: true }),
        streamEvents: async (_signal, onEvent) => {
            onEvent({ kind: "connected" });
            await new Promise<void>(() => {});
        },
        startTurn: async () => ({ threadId: "thread-1", turnId: "turn-1" }),
        cancelTurn: async () => undefined,
        deliverToolResult: async () => undefined,
        listThreads: async () => [],
        readThread: async () => ({ threadId: "thread-1", canvasId: "canvas-1", model: "gpt-5", createdAt: "2026-09-01T00:00:00.000Z", updatedAt: "2026-09-01T00:00:00.000Z", turns: [] }),
        archiveThread: async () => undefined,
        ...patch,
    };
}

function fakeRuntimeClient(patch: Partial<AgentRuntimeClient> = {}): AgentRuntimeClient {
    const view = runtimeView();
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

function fakeLocalRuntimeClient(patch: Partial<AgentLocalRuntimeClient> = {}): AgentLocalRuntimeClient {
    const view = runtimeView();
    return {
        startRun: async () => view,
        submitDecision: async () => view,
        interrupt: async () => runtimeView({ status: "cancelled", stateVersion: 3 }),
        ...patch,
    };
}

function runtimeView(
    options: {
        runId?: string;
        status?: AgentRuntimeView["state"]["status"];
        stateVersion?: number;
        lastEventSequence?: number;
        lastToolResult?: AgentRuntimeView["state"]["lastToolResult"];
        pendingTool?: boolean;
        pendingApproval?: boolean;
    } = {},
): AgentRuntimeView {
    const stateVersion = options.stateVersion ?? 2;
    const status = options.status ?? "running";
    const pendingToolCall: NonNullable<AgentRuntimeView["state"]["pendingToolCall"]> = {
        toolCallId: "tool-1",
        toolName: "canvas.read",
        actionVersion: 1,
        arguments: { canvasId: "canvas-1", selectedNodeIds: [], includeViewport: true },
        expectedDelivery: { kind: "canvas_change", targetCanvasId: "canvas-1", completionCriteria: [{ fact: "canvas_revision" }] },
    };
    const view: AgentRuntimeView = {
        run: {
            id: options.runId ?? "run-1",
            threadId: "audit-thread-1",
            reasoningHost: "local_codex",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status,
            lastEventSequence: options.lastEventSequence ?? 1,
            stateVersion,
            stepNumber: 0,
            maxSteps: 8,
            modelRecordId: "",
            modelKey: "",
            toolSchemaVersion: 8,
            runtimeVersion: 5,
            policyVersion: 5,
            createdAt: "2026-09-01T00:00:00.000Z",
            updatedAt: "2026-09-01T00:00:00.000Z",
        },
        state: {
            stateVersion,
            stepNumber: 0,
            maxSteps: 8,
            status,
            clarificationHistory: [],
            userMessage: "读取画布",
            configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" },
            ...(options.pendingTool || options.pendingApproval ? { pendingToolCall, ...(options.pendingTool ? { pendingToolStarted: true } : {}) } : {}),
            ...(options.lastToolResult ? { lastToolResult: options.lastToolResult } : {}),
        },
    };
    if (options.pendingApproval) {
        view.pendingApproval = {
            toolCallId: pendingToolCall.toolCallId,
            toolName: pendingToolCall.toolName,
            actionVersion: 1,
            proposalHash: "hash",
            expiresAt: "2026-09-01T01:00:00.000Z",
            effect: { kind: "canvas_mutation", summary: "读取画布", targetIds: ["canvas-1"] },
        };
    }
    return view;
}

async function flushAsyncWork(): Promise<void> {
    await Promise.resolve();
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    await Promise.resolve();
}
