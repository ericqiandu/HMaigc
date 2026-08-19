import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { nanoid } from "nanoid";

import {
    agentRuntimeClient,
    agentRuntimeHandleStorage,
    AgentRuntimeRequestError,
    type AgentClarificationAnswerInput,
    type AgentRuntimeClient,
    type AgentRuntimeEvent,
    type AgentRuntimeHandle,
    type AgentRuntimeHandleStorage,
    type AgentRuntimeStartConfiguration,
    type AgentRuntimeView,
    type AgentThreadHistoryItem,
} from "@/services/api/agent-runtime";

const terminalStatuses = new Set(["succeeded", "failed", "cancelled"]);

const statusLabels: Record<AgentRuntimeView["state"]["status"], string> = {
    queued: "准备中",
    running: "思考中",
    waiting_input: "询问中",
    waiting_approval: "等待确认",
    waiting_tool: "执行中",
    succeeded: "已完成",
    failed: "已失败",
    cancelled: "已取消",
};

export function agentRuntimeStatusLabel(status: AgentRuntimeView["state"]["status"]) {
    return statusLabels[status];
}

type UseAgentRuntimeInput = {
    canvasId: string;
    client?: AgentRuntimeClient;
    storage?: AgentRuntimeHandleStorage;
};

export function useAgentRuntime({ canvasId, client = agentRuntimeClient, storage = agentRuntimeHandleStorage }: UseAgentRuntimeInput) {
    const [threadId, setThreadId] = useState("");
    const [view, setView] = useState<AgentRuntimeView | null>(null);
    const [events, setEvents] = useState<AgentRuntimeEvent[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [connection, setConnection] = useState<"idle" | "connecting" | "connected" | "reconnecting">("idle");
    const [restored, setRestored] = useState(false);
    const [pendingUserMessage, setPendingUserMessage] = useState("");
    const [pendingConfiguration, setPendingConfiguration] = useState<AgentRuntimeStartConfiguration | null>(null);
    const [threads, setThreads] = useState<AgentThreadHistoryItem[]>([]);
    const [historyLoading, setHistoryLoading] = useState(true);
    const [historyError, setHistoryError] = useState("");
    const cursorRef = useRef(0);
    const threadIdRef = useRef("");
    const pendingRunRef = useRef<AgentRuntimeHandle["pendingRun"]>(undefined);
    const historyRequestRef = useRef(0);

    const persist = useCallback(
        async (nextView: AgentRuntimeView | null, nextThreadId = threadIdRef.current) => {
            if (!nextThreadId) return;
            const active = nextView && !terminalStatuses.has(nextView.state.status) ? nextView.run.id : undefined;
            const handle: AgentRuntimeHandle = { threadId: nextThreadId, lastSequence: active ? cursorRef.current : 0 };
            if (active) handle.activeRunId = active;
            else if (pendingRunRef.current) handle.pendingRun = pendingRunRef.current;
            await storage.save(canvasId, handle);
        },
        [canvasId, storage],
    );

    const adoptView = useCallback(
        (nextView: AgentRuntimeView) => {
            setView(nextView);
            setError("");
            void persist(nextView).catch((cause: unknown) => setError(errorMessage(cause, "Agent 恢复句柄保存失败")));
        },
        [persist],
    );

    const selectThread = useCallback(
        (item: AgentThreadHistoryItem) => {
            threadIdRef.current = item.thread.id;
            pendingRunRef.current = undefined;
            cursorRef.current = item.latestRun && !terminalStatuses.has(item.latestRun.state.status) ? item.latestRun.run.lastEventSequence : 0;
            setThreadId(item.thread.id);
            setView(item.latestRun);
            setEvents([]);
            setError("");
            setConnection("idle");
            setPendingUserMessage("");
            setPendingConfiguration(null);
            void persist(item.latestRun, item.thread.id).catch((cause: unknown) => setError(errorMessage(cause, "Agent 恢复句柄保存失败")));
        },
        [persist],
    );

    const reloadThreads = useCallback(async () => {
        const requestID = ++historyRequestRef.current;
        setHistoryLoading(true);
        setHistoryError("");
        try {
            const history = await client.listThreads(canvasId, 20);
            if (historyRequestRef.current === requestID) setThreads(history.items);
        } catch (cause) {
            if (historyRequestRef.current === requestID) setHistoryError(errorMessage(cause, "Agent 历史加载失败"));
        } finally {
            if (historyRequestRef.current === requestID) setHistoryLoading(false);
        }
    }, [canvasId, client]);

    useEffect(() => {
        let cancelled = false;
        const historyRequestID = ++historyRequestRef.current;
        threadIdRef.current = "";
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        setThreadId("");
        setView(null);
        setEvents([]);
        setError("");
        setConnection("idle");
        setPendingUserMessage("");
        setPendingConfiguration(null);
        setRestored(false);
        setThreads([]);
        setHistoryLoading(true);
        setHistoryError("");
        void Promise.allSettled([storage.load(canvasId), client.listThreads(canvasId, 20)])
            .then(async ([handleResult, historyResult]) => {
                if (cancelled || historyRequestRef.current !== historyRequestID) return;
                const handle = handleResult.status === "fulfilled" ? handleResult.value : null;
                const historyItems = historyResult.status === "fulfilled" ? historyResult.value.items : [];
                const handleLoadError = handleResult.status === "rejected" ? errorMessage(handleResult.reason, "Agent 本地恢复句柄读取失败") : "";
                setThreads(historyItems);
                setHistoryLoading(false);
                if (historyResult.status === "rejected") setHistoryError(errorMessage(historyResult.reason, "Agent 历史加载失败"));

                if (handle?.pendingRun) {
                    threadIdRef.current = handle.threadId;
                    pendingRunRef.current = handle.pendingRun;
                    cursorRef.current = handle.lastSequence;
                    setThreadId(handle.threadId);
                    setPendingUserMessage(handle.pendingRun.userMessage);
                    setPendingConfiguration(handle.pendingRun.configuration);
                    const resumed = await client.startRun(handle.threadId, { ...handle.pendingRun, maxSteps: 8 });
                    if (cancelled || historyRequestRef.current !== historyRequestID) return;
                    pendingRunRef.current = undefined;
                    setPendingUserMessage("");
                    setPendingConfiguration(null);
                    adoptView(resumed);
                    return;
                }
                if (handle?.activeRunId) {
                    threadIdRef.current = handle.threadId;
                    cursorRef.current = handle.lastSequence;
                    setThreadId(handle.threadId);
                    const resumed = await client.getRun(handle.activeRunId);
                    if (cancelled || historyRequestRef.current !== historyRequestID) return;
                    adoptView(resumed);
                    return;
                }
                const selected = (handle ? historyItems.find((item) => item.thread.id === handle.threadId) : undefined) ?? historyItems[0];
                if (selected) selectThread(selected);
                if (handleLoadError) setError(handleLoadError);
            })
            .catch((cause: unknown) => {
                if (!cancelled && historyRequestRef.current === historyRequestID) setError(errorMessage(cause, "Agent 运行恢复失败"));
            })
            .finally(() => {
                if (!cancelled && historyRequestRef.current === historyRequestID) {
                    setHistoryLoading(false);
                    setRestored(true);
                }
            });
        return () => {
            cancelled = true;
        };
    }, [adoptView, canvasId, client, selectThread, storage]);

    const runId = view?.run.id || "";
    const terminal = Boolean(view && terminalStatuses.has(view.state.status));
    useEffect(() => {
        if (!restored || !runId || terminal) {
            setConnection("idle");
            return;
        }
        setConnection("connecting");
        return client.subscribe(runId, cursorRef.current, {
            onOpen: () => setConnection("connected"),
            onError: (cause) => {
                if (cause) {
                    setError(cause.message);
                    setConnection("idle");
                } else setConnection("reconnecting");
            },
            onEvent: (event) => {
                if (event.sequence <= cursorRef.current) return;
                cursorRef.current = event.sequence;
                setEvents((current) => [...current, event].slice(-30));
                setView((current) => {
                    if (!current || current.run.id !== runId) return current;
                    const next = { ...current, run: { ...current.run, status: event.payload.status, lastEventSequence: event.sequence, stateVersion: event.payload.stateVersion, stepNumber: event.payload.stepNumber }, state: event.payload };
                    void persist(next).catch((cause: unknown) => setError(errorMessage(cause, "Agent 事件游标保存失败")));
                    return next;
                });
            },
        });
    }, [client, persist, restored, runId, terminal]);

    const submit = useCallback(
        async (userMessage: string, configuration: AgentRuntimeStartConfiguration) => {
            const message = userMessage.trim();
            if (!message || busy || (view && !terminalStatuses.has(view.state.status))) return false;
            setBusy(true);
            setError("");
            setEvents([]);
            cursorRef.current = 0;
            try {
                let activeThreadId = threadIdRef.current;
                if (!activeThreadId) {
                    const thread = await client.createThread(canvasId);
                    activeThreadId = thread.id;
                    threadIdRef.current = thread.id;
                    setThreadId(thread.id);
                    await persist(null, thread.id);
                }
                const pending = pendingRunRef.current;
                if (pending && (pending.userMessage !== message || !sameStartConfiguration(pending.configuration, configuration))) {
                    throw new Error("上次 Agent 启动结果尚未确认，请保留原指令、模型与 Skills 重试");
                }
                const request = pending || { clientRequestId: nanoid(), userMessage: message, configuration };
                pendingRunRef.current = request;
                setPendingUserMessage(request.userMessage);
                setPendingConfiguration(request.configuration);
                await persist(null, activeThreadId);
                const started = await client.startRun(activeThreadId, { ...request, maxSteps: 8 });
                pendingRunRef.current = undefined;
                setPendingUserMessage("");
                setPendingConfiguration(null);
                adoptView(started);
                void reloadThreads();
                return true;
            } catch (cause) {
                setError(errorMessage(cause, "Agent 运行启动失败"));
                return false;
            } finally {
                setBusy(false);
            }
        },
        [adoptView, busy, canvasId, client, persist, reloadThreads, view],
    );

    const decideApproval = useCallback(
        async (decision: "approved" | "rejected") => {
            const call = view?.state.status === "waiting_approval" ? view.state.pendingToolCall : undefined;
            if (!call || !view || busy) return;
            setBusy(true);
            setError("");
            try {
                adoptView(await client.submitApproval(view.run.id, { toolCallId: call.toolCallId, actionVersion: call.actionVersion, decision }));
            } catch (cause) {
                setError(errorMessage(cause, "审批提交失败"));
            } finally {
                setBusy(false);
            }
        },
        [adoptView, busy, client, view],
    );

    const submitClarificationResponse = useCallback(
        async (input: { requestId: string; questionId: string; answer: AgentClarificationAnswerInput; complete: boolean }) => {
            const pending = view?.state.status === "waiting_input" ? view.state.pendingClarification : undefined;
            if (!view || !pending) throw new Error("当前 Agent 运行未处于询问状态");
            if (pending.request.requestId !== input.requestId) throw new Error("当前问题身份已更新，请核对后重试");
            if (busy) return false;
            setBusy(true);
            setError("");
            try {
                const next = await client.submitClarificationResponse(view.run.id, pending.request.requestId, {
                    expectedStateVersion: view.state.stateVersion,
                    questionId: input.questionId,
                    answer: input.answer,
                    complete: input.complete,
                });
                adoptView(next);
                return true;
            } catch (cause) {
                if (cause instanceof AgentRuntimeRequestError && cause.status === 409) {
                    try {
                        adoptView(await client.getRun(view.run.id));
                        setError("问题已在其他页面更新，请核对后重试");
                    } catch (refreshCause) {
                        setError(errorMessage(refreshCause, "追问状态刷新失败，请重新打开当前会话"));
                    }
                    return false;
                }
                setError(errorMessage(cause, "追问回答提交失败"));
                return false;
            } finally {
                setBusy(false);
            }
        },
        [adoptView, busy, client, view],
    );

    const newThread = useCallback(async () => {
        if (view && !terminalStatuses.has(view.state.status)) return;
        await storage.clear(canvasId);
        threadIdRef.current = "";
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        setThreadId("");
        setView(null);
        setEvents([]);
        setError("");
        setPendingUserMessage("");
        setPendingConfiguration(null);
    }, [canvasId, storage, view]);

    return useMemo(
        () => ({
            threadId,
            selectedThreadId: threadId,
            threads,
            historyLoading,
            historyError,
            view,
            events,
            busy,
            error,
            connection,
            restored,
            terminal,
            pendingUserMessage,
            pendingConfiguration,
            submit,
            submitClarificationResponse,
            decideApproval,
            newThread,
            selectThread,
            reloadThreads,
        }),
        [
            busy,
            connection,
            decideApproval,
            error,
            events,
            historyError,
            historyLoading,
            newThread,
            pendingConfiguration,
            pendingUserMessage,
            reloadThreads,
            restored,
            selectThread,
            submit,
            submitClarificationResponse,
            terminal,
            threadId,
            threads,
            view,
        ],
    );
}

function errorMessage(cause: unknown, fallback: string) {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}

function sameStartConfiguration(left: AgentRuntimeStartConfiguration, right: AgentRuntimeStartConfiguration) {
    return (
        left.generationModels.image?.channelId === right.generationModels.image?.channelId &&
        left.generationModels.image?.model === right.generationModels.image?.model &&
        left.generationModels.video?.channelId === right.generationModels.video?.channelId &&
        left.generationModels.video?.model === right.generationModels.video?.model &&
        left.skillDirs.length === right.skillDirs.length &&
        left.skillDirs.every((dir, index) => dir === right.skillDirs[index]) &&
        left.executionMode === right.executionMode &&
        left.attachments.length === right.attachments.length &&
        left.attachments.every((attachment, index) => attachment.resourceId === right.attachments[index]?.resourceId && attachment.name === right.attachments[index]?.name)
    );
}
