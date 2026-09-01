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
    type AgentThreadHistoryTurn,
} from "@/services/api/agent-runtime";
import { initialAgentConversationState, reduceAgentConversation, type AgentConversationState } from "./agent-conversation-reducer";

const terminalStatuses = new Set(["succeeded", "failed", "cancelled"]);
const liveSubscriptionStatuses = new Set<AgentRuntimeView["state"]["status"]>(["queued", "running", "waiting_tool"]);
const agentRuntimeEventWindowLimit = 256;
const hiddenProcessEventKinds = new Set<AgentRuntimeEvent["kind"]>(["item.delta", "item.started", "state.snapshot"]);

type AgentTimelineState = {
    events: AgentRuntimeEvent[];
    conversation: AgentConversationState;
    meaningfulEvents: AgentRuntimeEvent[];
    lastSequence: number;
};

function initialAgentTimelineState(): AgentTimelineState {
    return { events: [], conversation: initialAgentConversationState(), meaningfulEvents: [], lastSequence: 0 };
}

function appendAgentTimelineEvent(state: AgentTimelineState, event: AgentRuntimeEvent): AgentTimelineState {
    if (event.sequence <= state.lastSequence) return state;
    const events = [...state.events, event].slice(-agentRuntimeEventWindowLimit);
    const meaningfulEvents = hiddenProcessEventKinds.has(event.kind) ? state.meaningfulEvents : [...state.meaningfulEvents, event].slice(-4);
    return { events, conversation: reduceAgentConversation(state.conversation, event), meaningfulEvents, lastSequence: event.sequence };
}

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

export function agentRuntimeUsesLiveSubscription(status: AgentRuntimeView["state"]["status"]) {
    return liveSubscriptionStatuses.has(status);
}

type UseAgentRuntimeInput = {
    canvasId: string;
    client?: AgentRuntimeClient;
    storage?: AgentRuntimeHandleStorage;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
};

export function useAgentRuntime({ canvasId, client = agentRuntimeClient, storage = agentRuntimeHandleStorage, onRuntimeEvent }: UseAgentRuntimeInput) {
    const [threadId, setThreadId] = useState("");
    const [view, setView] = useState<AgentRuntimeView | null>(null);
    const [timeline, setTimeline] = useState(initialAgentTimelineState);
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
    const runRefreshRequestRef = useRef(0);
    const historyReloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const runRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const stoppingRunIDRef = useRef("");
    const onRuntimeEventRef = useRef(onRuntimeEvent);

    useEffect(() => {
        onRuntimeEventRef.current = onRuntimeEvent;
    }, [onRuntimeEvent]);

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
        async (item: AgentThreadHistoryItem) => {
            const latestTurn = item.turns.at(-1);
            if (runRefreshTimerRef.current) {
                clearTimeout(runRefreshTimerRef.current);
                runRefreshTimerRef.current = null;
            }
            threadIdRef.current = item.thread.id;
            stoppingRunIDRef.current = "";
            pendingRunRef.current = undefined;
            cursorRef.current = latestTurn?.run.lastEventSequence ?? 0;
            runRefreshRequestRef.current += 1;
            setThreadId(item.thread.id);
            setView(null);
            setTimeline(initialAgentTimelineState());
            setError("");
            setConnection("idle");
            setPendingUserMessage("");
            setPendingConfiguration(null);
            if (!latestTurn) {
                await persist(null, item.thread.id);
                return;
            }
            const requestID = runRefreshRequestRef.current;
            try {
                const restoredView = await client.getRun(latestTurn.run.id);
                if (threadIdRef.current !== item.thread.id || runRefreshRequestRef.current !== requestID) return;
                adoptView(restoredView);
            } catch (cause) {
                if (threadIdRef.current === item.thread.id && runRefreshRequestRef.current === requestID) setError(errorMessage(cause, "Agent 会话恢复失败"));
            }
        },
        [adoptView, client, persist],
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

    const refreshCurrentRun = useCallback(async () => {
        if (!view) throw new Error("当前没有可刷新的 Agent 运行");
        const expectedRunID = view.run.id;
        const expectedThreadID = view.run.threadId;
        const next = await client.getRun(expectedRunID);
        if (next.run.id !== expectedRunID || next.run.threadId !== expectedThreadID || threadIdRef.current !== expectedThreadID) {
            throw new Error("Agent 阶段审核后的运行归属与当前会话不一致");
        }
        adoptView(next);
        await reloadThreads();
    }, [adoptView, client, reloadThreads, view]);

    const scheduleThreadReload = useCallback(() => {
        if (historyReloadTimerRef.current) clearTimeout(historyReloadTimerRef.current);
        historyReloadTimerRef.current = setTimeout(() => {
            historyReloadTimerRef.current = null;
            void reloadThreads();
        }, 120);
    }, [reloadThreads]);

    const scheduleRunRefresh = useCallback(
        (runId: string, expectedThreadId: string) => {
            if (runRefreshTimerRef.current) clearTimeout(runRefreshTimerRef.current);
            runRefreshTimerRef.current = setTimeout(() => {
                runRefreshTimerRef.current = null;
                const refreshRequestID = ++runRefreshRequestRef.current;
                void client
                    .getRun(runId)
                    .then((next) => {
                        if (runRefreshRequestRef.current !== refreshRequestID || next.run.id !== runId || threadIdRef.current !== expectedThreadId) return;
                        adoptView(next);
                    })
                    .catch((cause: unknown) => {
                        if (runRefreshRequestRef.current === refreshRequestID && threadIdRef.current === expectedThreadId) {
                            setError(errorMessage(cause, "Agent 实时状态刷新失败"));
                        }
                    });
            }, 50);
        },
        [adoptView, client],
    );

    useEffect(
        () => () => {
            if (historyReloadTimerRef.current) clearTimeout(historyReloadTimerRef.current);
            if (runRefreshTimerRef.current) clearTimeout(runRefreshTimerRef.current);
        },
        [],
    );

    useEffect(() => {
        let cancelled = false;
        const historyRequestID = ++historyRequestRef.current;
        threadIdRef.current = "";
        stoppingRunIDRef.current = "";
        if (historyReloadTimerRef.current) {
            clearTimeout(historyReloadTimerRef.current);
            historyReloadTimerRef.current = null;
        }
        if (runRefreshTimerRef.current) {
            clearTimeout(runRefreshTimerRef.current);
            runRefreshTimerRef.current = null;
        }
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        setThreadId("");
        setView(null);
        setTimeline(initialAgentTimelineState());
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
                if (selected) await selectThread(selected);
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
    const liveSubscription = Boolean(view && agentRuntimeUsesLiveSubscription(view.state.status));
    useEffect(() => {
        if (!restored || !runId || !liveSubscription) {
            setConnection("idle");
            return;
        }
        setConnection("connecting");
        let closeSubscription: () => void = () => undefined;
        // UI text is reconstructed from the durable event log after a refresh. The
        // saved cursor still fences business side effects, so replay never repeats
        // canvas mutations, notifications, or persisted recovery progress.
        closeSubscription = client.subscribe(runId, 0, {
            onOpen: () => {
                if (stoppingRunIDRef.current !== runId) setConnection("connected");
            },
            onError: (cause) => {
                if (stoppingRunIDRef.current === runId) return;
                if (cause) {
                    setError(cause.message);
                    setConnection("idle");
                } else setConnection("reconnecting");
            },
            onEvent: (event) => {
                if (stoppingRunIDRef.current === runId) return;
                if (event.runId !== runId || event.threadId !== threadIdRef.current) {
                    closeSubscription();
                    setConnection("idle");
                    setError("Agent 实时事件与当前会话归属冲突");
                    return;
                }
                setTimeline((current) => appendAgentTimelineEvent(current, event));
                if (event.sequence <= cursorRef.current) return;
                cursorRef.current = event.sequence;
                void storage.save(canvasId, { threadId: event.threadId, activeRunId: runId, lastSequence: event.sequence }).catch((cause: unknown) => setError(errorMessage(cause, "Agent 事件游标保存失败")));
                onRuntimeEventRef.current?.(event);
                scheduleThreadReload();
                scheduleRunRefresh(runId, event.threadId);
            },
        });
        return () => closeSubscription();
    }, [canvasId, client, liveSubscription, restored, runId, scheduleRunRefresh, scheduleThreadReload, storage]);

    const sendOrSteer = useCallback(
        async (userMessage: string, configuration: AgentRuntimeStartConfiguration) => {
            const message = userMessage.trim();
            if (!message || busy) return false;
            setBusy(true);
            setError("");
            try {
                if (view && !terminalStatuses.has(view.state.status)) {
                    const steered = await client.steer(view.run.id, { clientRequestId: nanoid(), message, expectedStateVersion: view.state.stateVersion });
                    adoptView(steered);
                    scheduleThreadReload();
                    return true;
                }
                stoppingRunIDRef.current = "";
                setTimeline(initialAgentTimelineState());
                cursorRef.current = 0;
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
                if (view && cause instanceof AgentRuntimeRequestError && cause.status === 409) {
                    try {
                        adoptView(await client.getRun(view.run.id));
                        scheduleThreadReload();
                        setError("Agent 运行状态已更新，请核对后重试追加指令");
                    } catch (refreshCause) {
                        setError(errorMessage(refreshCause, "Agent 追加冲突后状态刷新失败"));
                    }
                    return false;
                }
                setError(errorMessage(cause, view ? "Agent 追加指令失败" : "Agent 运行启动失败"));
                return false;
            } finally {
                setBusy(false);
            }
        },
        [adoptView, busy, canvasId, client, persist, reloadThreads, scheduleThreadReload, view],
    );

    const interrupt = useCallback(async () => {
        if (!view || terminalStatuses.has(view.state.status) || busy) return false;
        const interruptedRunID = view.run.id;
        stoppingRunIDRef.current = interruptedRunID;
        if (runRefreshTimerRef.current) {
            clearTimeout(runRefreshTimerRef.current);
            runRefreshTimerRef.current = null;
        }
        runRefreshRequestRef.current += 1;
        setBusy(true);
        setError("");
        try {
            adoptView(await client.interrupt(interruptedRunID, { expectedStateVersion: view.state.stateVersion }));
            scheduleThreadReload();
            return true;
        } catch (cause) {
            if (stoppingRunIDRef.current === interruptedRunID) stoppingRunIDRef.current = "";
            if (cause instanceof AgentRuntimeRequestError && cause.status === 409) {
                try {
                    adoptView(await client.getRun(interruptedRunID));
                    await reloadThreads();
                    setError("Agent 运行状态已更新，停止请求未执行");
                } catch (refreshCause) {
                    setError(errorMessage(refreshCause, "Agent 停止冲突后状态刷新失败"));
                }
                return false;
            }
            setError(errorMessage(cause, "Agent 停止失败"));
            return false;
        } finally {
            setBusy(false);
        }
    }, [adoptView, busy, client, reloadThreads, scheduleThreadReload, view]);

    const decideApproval = useCallback(
        async (decision: "approved" | "rejected") => {
            const call = view?.state.status === "waiting_approval" ? view.state.pendingToolCall : undefined;
            const approval = view?.state.status === "waiting_approval" ? view.pendingApproval : undefined;
            if (!call || !approval || !view || busy) return;
            if (approval.toolCallId !== call.toolCallId || approval.toolName !== call.toolName || approval.actionVersion !== call.actionVersion) {
                setError("审批提案与当前工具调用不一致，请刷新后重试");
                return;
            }
            const rejectedRunID = decision === "rejected" ? view.run.id : "";
            if (rejectedRunID) {
                stoppingRunIDRef.current = rejectedRunID;
                if (runRefreshTimerRef.current) {
                    clearTimeout(runRefreshTimerRef.current);
                    runRefreshTimerRef.current = null;
                }
                runRefreshRequestRef.current += 1;
            }
            setBusy(true);
            setError("");
            try {
                adoptView(await client.submitApproval(view.run.id, {
                    toolCallId: call.toolCallId,
                    actionVersion: call.actionVersion,
                    decision,
                    proposalHash: approval.proposalHash,
                }));
            } catch (cause) {
                if (rejectedRunID && stoppingRunIDRef.current === rejectedRunID) stoppingRunIDRef.current = "";
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
        if (runRefreshTimerRef.current) {
            clearTimeout(runRefreshTimerRef.current);
            runRefreshTimerRef.current = null;
        }
        runRefreshRequestRef.current += 1;
        threadIdRef.current = "";
        stoppingRunIDRef.current = "";
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        setThreadId("");
        setView(null);
        setTimeline(initialAgentTimelineState());
        setError("");
        setPendingUserMessage("");
        setPendingConfiguration(null);
    }, [canvasId, storage, view]);

    const turns = useMemo<AgentThreadHistoryTurn[]>(() => threads.find((item) => item.thread.id === threadId)?.turns ?? [], [threadId, threads]);

    return useMemo(
        () => ({
            threadId,
            selectedThreadId: threadId,
            threads,
            historyLoading,
            historyError,
            view,
            turns,
            lastSequence: cursorRef.current,
            events: timeline.events,
            conversation: timeline.conversation,
            meaningfulEvents: timeline.meaningfulEvents,
            busy,
            error,
            connection,
            restored,
            terminal,
            pendingUserMessage,
            pendingConfiguration,
            submit: sendOrSteer,
            sendOrSteer,
            interrupt,
            submitClarificationResponse,
            decideApproval,
            newThread,
            selectThread,
            reloadThreads,
            refreshCurrentRun,
        }),
        [
            busy,
            connection,
            decideApproval,
            error,
            historyError,
            historyLoading,
            interrupt,
            newThread,
            pendingConfiguration,
            pendingUserMessage,
            reloadThreads,
            refreshCurrentRun,
            restored,
            selectThread,
            sendOrSteer,
            submitClarificationResponse,
            terminal,
            threadId,
            threads,
            timeline,
            turns,
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
