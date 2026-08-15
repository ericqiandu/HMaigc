import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { nanoid } from "nanoid";

import { agentRuntimeClient, agentRuntimeHandleStorage, type AgentRuntimeClient, type AgentRuntimeEvent, type AgentRuntimeHandle, type AgentRuntimeHandleStorage, type AgentRuntimeView } from "@/services/api/agent-runtime";

const terminalStatuses = new Set(["succeeded", "failed", "cancelled"]);

type UseAgentRuntimeInput = {
    canvasId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    client?: AgentRuntimeClient;
    storage?: AgentRuntimeHandleStorage;
};

export function useAgentRuntime({ canvasId, canvasRevision, selectedNodeIds, client = agentRuntimeClient, storage = agentRuntimeHandleStorage }: UseAgentRuntimeInput) {
    const [threadId, setThreadId] = useState("");
    const [view, setView] = useState<AgentRuntimeView | null>(null);
    const [events, setEvents] = useState<AgentRuntimeEvent[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [connection, setConnection] = useState<"idle" | "connecting" | "connected" | "reconnecting">("idle");
    const [restored, setRestored] = useState(false);
    const [pendingUserMessage, setPendingUserMessage] = useState("");
    const [selectionRetry, setSelectionRetry] = useState(0);
    const cursorRef = useRef(0);
    const threadIdRef = useRef("");
    const pendingRunRef = useRef<AgentRuntimeHandle["pendingRun"]>(undefined);
    const submittedSelectionRef = useRef(new Set<string>());
    const selectedNodeIdsRef = useRef(selectedNodeIds);
    const canvasRevisionRef = useRef(canvasRevision);

    useEffect(() => {
        selectedNodeIdsRef.current = selectedNodeIds;
    }, [selectedNodeIds]);
    useEffect(() => {
        canvasRevisionRef.current = canvasRevision;
    }, [canvasRevision]);

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

    useEffect(() => {
        let cancelled = false;
        threadIdRef.current = "";
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        submittedSelectionRef.current.clear();
        setThreadId("");
        setView(null);
        setEvents([]);
        setError("");
        setConnection("idle");
        setPendingUserMessage("");
        setRestored(false);
        void storage
            .load(canvasId)
            .then(async (handle) => {
                if (cancelled || !handle) return;
                threadIdRef.current = handle.threadId;
                pendingRunRef.current = handle.pendingRun;
                cursorRef.current = handle.lastSequence;
                setThreadId(handle.threadId);
                if (handle.activeRunId) {
                    const resumed = await client.getRun(handle.activeRunId);
                    if (!cancelled) {
                        pendingRunRef.current = undefined;
                        adoptView(resumed);
                    }
                } else if (handle.pendingRun) {
                    setPendingUserMessage(handle.pendingRun.userMessage);
                    const resumed = await client.startRun(handle.threadId, { ...handle.pendingRun, maxSteps: 8 });
                    if (!cancelled) {
                        pendingRunRef.current = undefined;
                        setPendingUserMessage("");
                        adoptView(resumed);
                    }
                }
            })
            .catch((cause: unknown) => {
                if (!cancelled) setError(errorMessage(cause, "Agent 运行恢复失败"));
            })
            .finally(() => {
                if (!cancelled) setRestored(true);
            });
        return () => {
            cancelled = true;
        };
    }, [adoptView, canvasId, client, storage]);

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

    const pendingSelection = view?.state.status === "waiting_tool" && view.state.pendingToolCall?.toolName === "canvas.read_selection" ? view.state.pendingToolCall : null;
    useEffect(() => {
        if (!pendingSelection || !view) return;
        const identity = `${view.run.id}:${pendingSelection.toolCallId}:${pendingSelection.actionVersion}`;
        if (submittedSelectionRef.current.has(identity)) return;
        submittedSelectionRef.current.add(identity);
        setBusy(true);
        void client
            .submitSelection(view.run.id, {
                toolCallId: pendingSelection.toolCallId,
                actionVersion: pendingSelection.actionVersion,
                selection: { revision: canvasRevisionRef.current, nodeIds: [...selectedNodeIdsRef.current].sort() },
            })
            .then(adoptView)
            .catch((cause: unknown) => setError(errorMessage(cause, "选区事实提交失败")))
            .finally(() => setBusy(false));
    }, [adoptView, client, pendingSelection, selectionRetry, view]);

    const retrySelection = useCallback(() => {
        if (!pendingSelection || !view) return;
        submittedSelectionRef.current.delete(`${view.run.id}:${pendingSelection.toolCallId}:${pendingSelection.actionVersion}`);
        setError("");
        setSelectionRetry((value) => value + 1);
    }, [pendingSelection, view]);

    const submit = useCallback(
        async (userMessage: string) => {
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
                if (pending && pending.userMessage !== message) throw new Error("上次 Agent 启动结果尚未确认，请保留原指令重试");
                const request = pending || { clientRequestId: nanoid(), userMessage: message };
                pendingRunRef.current = request;
                setPendingUserMessage(request.userMessage);
                await persist(null, activeThreadId);
                const started = await client.startRun(activeThreadId, { ...request, maxSteps: 8 });
                pendingRunRef.current = undefined;
                setPendingUserMessage("");
                adoptView(started);
                return true;
            } catch (cause) {
                setError(errorMessage(cause, "Agent 运行启动失败"));
                return false;
            } finally {
                setBusy(false);
            }
        },
        [adoptView, busy, canvasId, client, view],
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

    const newThread = useCallback(async () => {
        if (view && !terminalStatuses.has(view.state.status)) return;
        await storage.clear(canvasId);
        threadIdRef.current = "";
        pendingRunRef.current = undefined;
        cursorRef.current = 0;
        submittedSelectionRef.current.clear();
        setThreadId("");
        setView(null);
        setEvents([]);
        setError("");
        setPendingUserMessage("");
    }, [canvasId, storage, view]);

    return useMemo(
        () => ({ threadId, view, events, busy, error, connection, restored, terminal, pendingUserMessage, canRetrySelection: Boolean(pendingSelection && error), submit, decideApproval, retrySelection, newThread }),
        [busy, connection, decideApproval, error, events, newThread, pendingSelection, pendingUserMessage, restored, retrySelection, submit, terminal, threadId, view],
    );
}

function errorMessage(cause: unknown, fallback: string) {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
