import { useCallback, useEffect, useRef, useState } from "react";
import { nanoid } from "nanoid";

import {
    agentLocalRuntimeClient,
    agentRuntimeClient,
    type AgentApprovalSubmission,
    type AgentLocalRuntimeClient,
    type AgentRuntimeClient,
    type AgentRuntimeEvent,
    type AgentRuntimeStartConfiguration,
    type AgentRuntimeView,
} from "@/services/api/agent-runtime";
import { LocalAgentBridge, type LocalAgentAuthoritativeToolResult, type LocalAgentBridgeState } from "@/services/local-agent/local-agent-bridge";
import { LocalAgentHttpClient, type LocalAgentAttachment, type LocalAgentClientPort } from "@/services/local-agent/local-agent-client";
import type { LocalAgentEvent, LocalAgentThread } from "@/services/local-agent/local-agent-contracts";
import { createLocalAgentSessionStore, type LocalAgentConnection, type LocalAgentSessionStore } from "@/services/local-agent/local-agent-session";
import { AGENT_RUNTIME_DECISION_BUDGET } from "./canvas-agent-runtime-configuration";

type UseLocalAgentRuntimeInput = {
    canvasId: string;
    sessionStore?: LocalAgentSessionStore;
    createClient?: (connection: LocalAgentConnection) => LocalAgentClientPort;
    localRuntimeClient?: AgentLocalRuntimeClient;
    runtimeClient?: Pick<AgentRuntimeClient, "submitApproval" | "interrupt" | "getRun" | "subscribe">;
    beforeToolProposal?: () => Promise<void>;
    onRuntimeView?: (view: AgentRuntimeView) => void;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
    onToolResult?: (result: LocalAgentAuthoritativeToolResult) => void;
    connectionTimeoutMs?: number;
};

export function useLocalAgentRuntime({
    canvasId,
    sessionStore = createLocalAgentSessionStore(window.sessionStorage),
    createClient = (connection) => new LocalAgentHttpClient(connection),
    localRuntimeClient = agentLocalRuntimeClient,
    runtimeClient = agentRuntimeClient,
    beforeToolProposal = async () => undefined,
    onRuntimeView,
    onRuntimeEvent,
    onToolResult,
    connectionTimeoutMs = 10_000,
}: UseLocalAgentRuntimeInput) {
    const [savedConnection, setSavedConnection] = useState<LocalAgentConnection | null>(() => sessionStore.load());
    const [connection, setConnection] = useState<"disconnected" | "connecting" | "connected">("disconnected");
    const [bridgeState, setBridgeState] = useState<LocalAgentBridgeState>({ kind: "idle" });
    const [view, setView] = useState<AgentRuntimeView | null>(null);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const [events, setEvents] = useState<LocalAgentEvent[]>([]);
    const [threads, setThreads] = useState<LocalAgentThread[]>([]);
    const [selectedThread, setSelectedThread] = useState<LocalAgentThread | null>(null);
    const [historyLoading, setHistoryLoading] = useState(false);
    const [runtimeEvents, setRuntimeEvents] = useState<AgentRuntimeEvent[]>([]);
    const [backendConnection, setBackendConnection] = useState<"idle" | "connecting" | "connected" | "reconnecting">("idle");
    const clientRef = useRef<LocalAgentClientPort | undefined>(undefined);
    const bridgeRef = useRef<LocalAgentBridge | undefined>(undefined);
    const streamAbortRef = useRef<AbortController | undefined>(undefined);
    const onRuntimeViewRef = useRef(onRuntimeView);
    const onRuntimeEventRef = useRef(onRuntimeEvent);
    const onToolResultRef = useRef(onToolResult);
    const runtimeRunIdRef = useRef("");
    const runtimeCursorRef = useRef(0);
    const refreshQueueRef = useRef<Promise<void>>(Promise.resolve());

    useEffect(() => {
        onRuntimeViewRef.current = onRuntimeView;
    }, [onRuntimeView]);
    useEffect(() => {
        onRuntimeEventRef.current = onRuntimeEvent;
    }, [onRuntimeEvent]);
    useEffect(() => {
        onToolResultRef.current = onToolResult;
    }, [onToolResult]);
    useEffect(
        () => () => {
            const bridge = bridgeRef.current;
            if (bridge?.hasActiveTurn) {
                void bridge.dispose("本机 Agent 工作区已卸载").catch((cause: unknown) => {
                    console.error(
                        JSON.stringify({
                            event: "local_agent_unmount_disconnect_failed",
                            error: cause instanceof Error ? cause.message : "本机 Agent 卸载终止返回未知错误",
                        }),
                    );
                });
            }
            streamAbortRef.current?.abort();
        },
        [],
    );

    const adoptView = useCallback((next: AgentRuntimeView) => {
        if (runtimeRunIdRef.current === next.run.id) {
            runtimeCursorRef.current = Math.max(runtimeCursorRef.current, next.run.lastEventSequence);
        } else {
            runtimeRunIdRef.current = next.run.id;
            runtimeCursorRef.current = next.run.lastEventSequence;
            setRuntimeEvents([]);
        }
        setView(next);
        onRuntimeViewRef.current?.(next);
    }, []);

    const adoptBridgeState = useCallback((next: LocalAgentBridgeState) => {
        setBridgeState(next);
        setError(next.kind === "failed" ? next.message : "");
    }, []);

    const loadThreads = useCallback(
        async (client: LocalAgentClientPort) => {
            setHistoryLoading(true);
            try {
                const next = await client.listThreads(canvasId);
                setThreads(next);
                return next;
            } finally {
                setHistoryLoading(false);
            }
        },
        [canvasId],
    );

    const failConnectedStream = useCallback(async (cause: unknown) => {
        const message = contextualError("本机 Agent 事件流已断开", cause);
        setConnection("disconnected");
        setError(message);
        const bridge = bridgeRef.current;
        if (!bridge?.hasActiveTurn) return;
        try {
            await bridge.disconnect(message);
        } catch (disconnectCause) {
            setError(publicError(disconnectCause, "本机 Agent 致命断流后的取消失败"));
        }
    }, []);

    const connect = useCallback(
        async (nextConnection: LocalAgentConnection) => {
            if (connection !== "disconnected") return;
            setConnection("connecting");
            setError("");
            const client = createClient(nextConnection);
            const abort = new AbortController();
            const timeoutError = new Error(`本机 Agent 连接超时（${connectionTimeoutMs}ms）`);
            let timeoutId: number | undefined;
            try {
                const establish = async () => {
                    await client.health(abort.signal);
                    streamAbortRef.current = abort;
                    clientRef.current = client;
                    await new Promise<void>((resolve, reject) => {
                        let opened = false;
                        void client
                            .streamEvents(abort.signal, (event) => {
                                if (event.kind === "connected" && !opened) {
                                    opened = true;
                                    resolve();
                                    return;
                                }
                                setEvents((current) => [...current, event]);
                                const bridge = bridgeRef.current;
                                if (bridge) void bridge.handleEvent(event).catch((cause: unknown) => setError(publicError(cause, "本机 Agent 事件处理失败")));
                            })
                            .then(
                                () => {
                                    if (abort.signal.aborted) return;
                                    const cause = new Error("本机 Agent 事件流已断开");
                                    if (!opened) reject(cause);
                                    else void failConnectedStream(cause);
                                },
                                (cause: unknown) => {
                                    if (abort.signal.aborted) return;
                                    if (!opened) reject(cause);
                                    else void failConnectedStream(cause);
                                },
                            );
                    });
                };
                const timeout = new Promise<never>((_resolve, reject) => {
                    timeoutId = window.setTimeout(() => {
                        abort.abort();
                        reject(timeoutError);
                    }, connectionTimeoutMs);
                });
                await Promise.race([establish(), timeout]);
                const history = await loadThreads(client);
                sessionStore.save(nextConnection);
                setSavedConnection(nextConnection);
                setSelectedThread((current) => (current ? (history.find((thread) => thread.threadId === current.threadId) ?? null) : null));
                setConnection("connected");
            } catch (cause) {
                abort.abort();
                streamAbortRef.current = undefined;
                clientRef.current = undefined;
                setConnection("disconnected");
                setError(publicError(cause, "本机 Agent 连接失败"));
                throw cause;
            } finally {
                if (timeoutId !== undefined) window.clearTimeout(timeoutId);
            }
        },
        [connection, connectionTimeoutMs, createClient, failConnectedStream, loadThreads, sessionStore],
    );

    const disconnect = useCallback(async () => {
        const bridge = bridgeRef.current;
        if (bridge) {
            try {
                await bridge.disconnect("用户已断开本机 Agent");
            } catch (cause) {
                setError(publicError(cause, "本机 Agent 断开失败"));
                throw cause;
            }
        }
        streamAbortRef.current?.abort();
        streamAbortRef.current = undefined;
        clientRef.current = undefined;
        bridgeRef.current = undefined;
        sessionStore.clear();
        setSavedConnection(null);
        setConnection("disconnected");
        setBridgeState({ kind: "idle" });
        setView(null);
        setEvents([]);
        setThreads([]);
        setSelectedThread(null);
        setRuntimeEvents([]);
        setBackendConnection("idle");
        runtimeRunIdRef.current = "";
        runtimeCursorRef.current = 0;
        setError("");
    }, [sessionStore]);

    const reloadThreads = useCallback(async () => {
        const client = clientRef.current;
        if (!client || connection !== "connected") throw new Error("请先连接本机 Agent");
        try {
            const next = await loadThreads(client);
            setSelectedThread((current) => (current ? (next.find((thread) => thread.threadId === current.threadId) ?? null) : null));
        } catch (cause) {
            setError(publicError(cause, "本机 Agent 历史读取失败"));
            throw cause;
        }
    }, [connection, loadThreads]);

    const selectThread = useCallback(
        async (threadId: string) => {
            const client = clientRef.current;
            if (!client || connection !== "connected") throw new Error("请先连接本机 Agent");
            setHistoryLoading(true);
            setError("");
            try {
                setSelectedThread(await client.readThread(canvasId, threadId));
                setEvents([]);
                setRuntimeEvents([]);
                runtimeRunIdRef.current = "";
                runtimeCursorRef.current = 0;
                setView(null);
                setBridgeState({ kind: "idle" });
            } catch (cause) {
                setError(publicError(cause, "本机 Agent 对话读取失败"));
                throw cause;
            } finally {
                setHistoryLoading(false);
            }
        },
        [canvasId, connection],
    );

    const newThread = useCallback(() => {
        if (bridgeRef.current?.hasActiveTurn) throw new Error("本机 Agent 当前 turn 尚未结束");
        bridgeRef.current = undefined;
        setSelectedThread(null);
        setEvents([]);
        setRuntimeEvents([]);
        runtimeRunIdRef.current = "";
        runtimeCursorRef.current = 0;
        setView(null);
        setBridgeState({ kind: "idle" });
        setError("");
    }, []);

    const submit = useCallback(
        async (message: string, configuration: AgentRuntimeStartConfiguration, attachments: LocalAgentAttachment[]) => {
            const client = clientRef.current;
            if (!client || connection !== "connected") throw new Error("请先连接本机 Agent");
            if (!message.trim()) return false;
            if (bridgeRef.current?.hasActiveTurn) throw new Error("本机 Agent 当前 turn 尚未结束");
            setBusy(true);
            setError("");
            try {
                const bridge = new LocalAgentBridge({
                    canvasId,
                    localClient: client,
                    runtimeClient: localRuntimeClient,
                    configuration,
                    maxSteps: AGENT_RUNTIME_DECISION_BUDGET,
                    beforeToolProposal: async () => beforeToolProposal(),
                    onRuntimeView: adoptView,
                    onToolResult: (result) => onToolResultRef.current?.(result),
                    onState: adoptBridgeState,
                });
                bridgeRef.current = bridge;
                await bridge.start({
                    requestId: nanoid(),
                    message: message.trim(),
                    attachments,
                    ...(selectedThread ? { threadId: selectedThread.threadId } : {}),
                });
                return true;
            } catch (cause) {
                setError(publicError(cause, "本机 Agent 启动失败"));
                return false;
            } finally {
                setBusy(false);
            }
        },
        [adoptBridgeState, adoptView, beforeToolProposal, canvasId, connection, localRuntimeClient, selectedThread],
    );

    const decideApproval = useCallback(
        async (submission: AgentApprovalSubmission) => {
            const current = view;
            const bridge = bridgeRef.current;
            if (!current || current.state.status !== "waiting_approval" || !bridge) throw new Error("当前没有等待审批的本机 Agent Run");
            setBusy(true);
            setError("");
            try {
                const next = await runtimeClient.submitApproval(current.run.id, submission);
                adoptView(next);
                await bridge.acceptRuntimeView(next);
            } catch (cause) {
                setError(publicError(cause, "本机 Agent 审批失败"));
                throw cause;
            } finally {
                setBusy(false);
            }
        },
        [adoptView, runtimeClient, view],
    );

    const interrupt = useCallback(async () => {
        const current = view;
        const bridge = bridgeRef.current;
        if (!current || !bridge) return false;
        setBusy(true);
        setError("");
        try {
            const interrupted = await runtimeClient.interrupt(current.run.id, { expectedStateVersion: current.state.stateVersion });
            adoptView(interrupted);
            await bridge.acceptRuntimeView(interrupted);
            await bridge.disconnect("用户已停止本机 Agent turn");
            return true;
        } catch (cause) {
            setError(publicError(cause, "本机 Agent 停止失败"));
            return false;
        } finally {
            setBusy(false);
        }
    }, [adoptView, runtimeClient, view]);

    const runId = view?.run.id ?? "";
    const liveBackendRun = Boolean(view && !isTerminalStatus(view.state.status));
    useEffect(() => {
        if (!runId || !liveBackendRun) {
            setBackendConnection("idle");
            return;
        }
        setBackendConnection("connecting");
        const close = runtimeClient.subscribe(runId, runtimeCursorRef.current, {
            onOpen: () => setBackendConnection("connected"),
            onError: (cause) => {
                if (cause) {
                    setError(cause.message);
                    setBackendConnection("idle");
                    const bridge = bridgeRef.current;
                    if (bridge?.hasActiveTurn) {
                        void bridge.disconnect(cause.message).catch((disconnectCause: unknown) => {
                            setError(publicError(disconnectCause, "本机 Agent 致命断流后的取消失败"));
                        });
                    }
                } else {
                    setBackendConnection("reconnecting");
                }
            },
            onEvent: (event) => {
                if (event.runId !== runId || event.sequence <= runtimeCursorRef.current) return;
                runtimeCursorRef.current = event.sequence;
                setRuntimeEvents((current) => [...current, event]);
                onRuntimeEventRef.current?.(event);
                refreshQueueRef.current = refreshQueueRef.current
                    .then(async () => {
                        const next = await runtimeClient.getRun(runId);
                        if (next.run.id !== runId || next.run.reasoningHost !== "local_codex") throw new Error("本机 Agent 后端事件归属冲突");
                        adoptView(next);
                        const bridge = bridgeRef.current;
                        if (bridge?.hasActiveTurn) await bridge.acceptRuntimeView(next);
                    })
                    .catch((cause: unknown) => {
                        setError(publicError(cause, "本机 Agent 权威结果刷新失败"));
                    });
            },
        });
        return close;
    }, [adoptView, liveBackendRun, runId, runtimeClient]);

    return {
        savedConnection,
        connection,
        bridgeState,
        view,
        error,
        busy,
        events,
        threads,
        selectedThread,
        historyLoading,
        runtimeEvents,
        backendConnection,
        activeTurn: Boolean(bridgeRef.current?.hasActiveTurn),
        connect,
        disconnect,
        submit,
        decideApproval,
        interrupt,
        reloadThreads,
        selectThread,
        newThread,
    };
}

function publicError(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}

function contextualError(context: string, cause: unknown): string {
    const detail = publicError(cause, context);
    return detail === context ? context : `${context}：${detail}`;
}

function isTerminalStatus(status: AgentRuntimeView["state"]["status"]): boolean {
    return status === "succeeded" || status === "failed" || status === "cancelled";
}
