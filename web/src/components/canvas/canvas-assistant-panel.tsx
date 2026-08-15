import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, CheckCircle2, ChevronDown, CircleAlert, PanelRightClose, Plus, Send, ShieldCheck, XCircle } from "lucide-react";
import { Button, Tooltip } from "antd";
import { motion } from "motion/react";

import { canvasThemes } from "@/lib/canvas-theme";
import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage, AgentRuntimeState } from "@/services/api/agent-runtime";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAgentLaunchRequest } from "@/types/canvas";
import { useAgentRuntime } from "./use-agent-runtime";
import "./canvas-agent-panel.css";

export const CANVAS_AGENT_PANEL_MOTION_MS = 240;

type CanvasAssistantPanelProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    closing: boolean;
    onCollapse: () => void;
    agentLaunchRequest?: CanvasAgentLaunchRequest;
    onAgentLaunchHandled?: (launchRequestId: string) => void;
    runtimeClient?: AgentRuntimeClient;
    runtimeStorage?: AgentRuntimeHandleStorage;
};

export function CanvasAssistantPanel({ projectId, canvasRevision, selectedNodeIds, closing, onCollapse, agentLaunchRequest, onAgentLaunchHandled, runtimeClient, runtimeStorage }: CanvasAssistantPanelProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [prompt, setPrompt] = useState("");
    const launchAttemptRef = useRef("");
    const runtime = useAgentRuntime({ canvasId: projectId, canvasRevision, selectedNodeIds, client: runtimeClient, storage: runtimeStorage });
    const active = Boolean(runtime.view && !runtime.terminal);

    useEffect(() => {
        if (!runtime.pendingUserMessage) return;
        setPrompt((current) => current || runtime.pendingUserMessage);
    }, [runtime.pendingUserMessage]);

    useEffect(() => {
        if (!agentLaunchRequest || !runtime.restored || launchAttemptRef.current === agentLaunchRequest.id) return;
        launchAttemptRef.current = agentLaunchRequest.id;
        setPrompt(agentLaunchRequest.prompt);
        void runtime.submit(agentLaunchRequest.prompt).then((submitted) => {
            if (!submitted) return;
            setPrompt("");
            onAgentLaunchHandled?.(agentLaunchRequest.id);
        });
    }, [agentLaunchRequest, onAgentLaunchHandled, runtime.restored, runtime.submit]);

    const submit = async () => {
        if (!(await runtime.submit(prompt))) return;
        if (agentLaunchRequest?.prompt === prompt) onAgentLaunchHandled?.(agentLaunchRequest.id);
        setPrompt("");
    };

    return (
        <motion.aside
            className="canvas-agent-runtime-panel"
            initial={{ x: 28, opacity: 0 }}
            animate={{ x: closing ? 28 : 0, opacity: closing ? 0 : 1 }}
            transition={{ duration: CANVAS_AGENT_PANEL_MOTION_MS / 1000, ease: [0.2, 0.8, 0.2, 1] }}
            style={{ background: theme.toolbar.panel, color: theme.node.text, borderColor: theme.node.stroke }}
            aria-label="画布 Agent"
        >
            <header className="canvas-agent-runtime-header">
                <div className="canvas-agent-runtime-heading">
                    <span className="canvas-agent-runtime-avatar" style={{ background: theme.accent.primarySoft, color: theme.accent.primary }}>
                        <Bot className="canvas-agent-runtime-avatar-icon" />
                    </span>
                    <div className="canvas-agent-runtime-title-copy">
                        <strong className="canvas-agent-runtime-title">Agent</strong>
                        <span className="canvas-agent-runtime-subtitle" style={{ color: theme.node.muted }}>
                            单一运行内核 · {runtime.threadId ? "对话已建立" : "等待任务"}
                        </span>
                    </div>
                </div>
                <div className="canvas-agent-runtime-header-actions">
                    <Tooltip title={active ? "当前任务完成后才能新建对话" : "新建对话"}>
                        <Button
                            className="canvas-agent-runtime-icon-button"
                            type="text"
                            icon={<Plus className="canvas-agent-runtime-button-icon" />}
                            disabled={active || runtime.busy}
                            onClick={() => void runtime.newThread()}
                            aria-label="新建 Agent 对话"
                        />
                    </Tooltip>
                    <Tooltip title="收起">
                        <Button className="canvas-agent-runtime-icon-button" type="text" icon={<PanelRightClose className="canvas-agent-runtime-button-icon" />} onClick={onCollapse} aria-label="收起 Agent" />
                    </Tooltip>
                </div>
            </header>

            <section className="canvas-agent-runtime-content thin-scrollbar">
                {!runtime.view ? <AgentEmptyState restored={runtime.restored} muted={theme.node.muted} /> : <AgentRunContent state={runtime.view.state} events={runtime.events} connection={runtime.connection} muted={theme.node.muted} />}
                {runtime.error ? (
                    <div className="canvas-agent-runtime-error" role="alert">
                        <CircleAlert className="canvas-agent-runtime-error-icon" />
                        <div className="canvas-agent-runtime-error-content">
                            <span className="canvas-agent-runtime-error-copy">{runtime.error}</span>
                            {runtime.canRetrySelection ? (
                                <Button className="canvas-agent-runtime-error-retry" size="small" onClick={runtime.retrySelection}>
                                    重试选区提交
                                </Button>
                            ) : null}
                        </div>
                    </div>
                ) : null}
                {runtime.view?.state.status === "waiting_approval" && runtime.view.state.pendingToolCall ? (
                    <AgentApprovalCard state={runtime.view.state} busy={runtime.busy} muted={theme.node.muted} onDecision={(decision) => void runtime.decideApproval(decision)} />
                ) : null}
            </section>

            <footer className="canvas-agent-runtime-composer" style={{ background: theme.node.fill, borderColor: theme.node.stroke }}>
                <div className="canvas-agent-runtime-selection" style={{ color: theme.node.muted }}>
                    当前画布 v{canvasRevision} · 已选 {selectedNodeIds.size} 个节点
                </div>
                <textarea
                    className="canvas-agent-runtime-textarea thin-scrollbar"
                    value={prompt}
                    disabled={!runtime.restored || active || runtime.busy}
                    onChange={(event) => setPrompt(event.target.value)}
                    onKeyDown={(event) => {
                        if (event.key !== "Enter" || event.shiftKey || event.ctrlKey || event.metaKey) return;
                        event.preventDefault();
                        void submit();
                    }}
                    placeholder={active ? "当前任务运行中" : "告诉 Agent 你要完成什么"}
                    style={{ color: theme.node.text }}
                    rows={3}
                />
                <div className="canvas-agent-runtime-composer-footer">
                    <span className="canvas-agent-runtime-composer-hint" style={{ color: theme.node.muted }}>
                        Enter 发送 · Shift + Enter 换行
                    </span>
                    <Button
                        className="canvas-agent-runtime-submit"
                        type="primary"
                        icon={<Send className="canvas-agent-runtime-submit-icon" />}
                        disabled={!prompt.trim() || !runtime.restored || active || runtime.busy}
                        loading={runtime.busy}
                        onClick={() => void submit()}
                    >
                        发送
                    </Button>
                </div>
            </footer>
        </motion.aside>
    );
}

function AgentEmptyState({ restored, muted }: { restored: boolean; muted: string }) {
    return (
        <div className="canvas-agent-runtime-empty">
            <Bot className="canvas-agent-runtime-empty-icon" />
            <strong className="canvas-agent-runtime-empty-title">{restored ? "从目标开始" : "正在恢复运行事实"}</strong>
            <p className="canvas-agent-runtime-empty-description" style={{ color: muted }}>
                {restored ? "Agent 会读取真实画布、按需调用工具，并在交付验收通过后结束。" : "正在读取本画布最后一次可恢复运行。"}
            </p>
        </div>
    );
}

function AgentRunContent({ state, events, connection, muted }: { state: AgentRuntimeState; events: AgentRuntimeEvent[]; connection: string; muted: string }) {
    const status = runtimeStatus(state.status);
    const lastEvents = useMemo(() => events.slice(-8), [events]);
    return (
        <div className="canvas-agent-runtime-run">
            <div className="canvas-agent-runtime-user-message">
                <span className="canvas-agent-runtime-message-label" style={{ color: muted }}>
                    你
                </span>
                <p className="canvas-agent-runtime-message-copy">{state.userMessage}</p>
            </div>
            <details className="canvas-agent-runtime-process" open={!isTerminal(state.status)}>
                <summary className="canvas-agent-runtime-process-summary">
                    <span className="canvas-agent-runtime-process-main">
                        <ChevronDown className="canvas-agent-runtime-chevron" />
                        <span className="canvas-agent-runtime-status-dot" data-status={state.status} />
                        <strong className="canvas-agent-runtime-status-label">{status}</strong>
                    </span>
                    <span className="canvas-agent-runtime-step" style={{ color: muted }}>
                        第 {state.stepNumber} / {state.maxSteps} 步{connection === "reconnecting" ? " · 正在重连" : ""}
                    </span>
                </summary>
                <div className="canvas-agent-runtime-event-list">
                    {lastEvents.length ? (
                        lastEvents.map((event) => (
                            <div key={event.sequence} className="canvas-agent-runtime-event">
                                <span className="canvas-agent-runtime-event-sequence">{event.sequence}</span>
                                <span className="canvas-agent-runtime-event-label">{eventLabel(event.kind)}</span>
                            </div>
                        ))
                    ) : (
                        <span className="canvas-agent-runtime-event-empty" style={{ color: muted }}>
                            等待新的持久化事件
                        </span>
                    )}
                </div>
            </details>
            {state.lastToolResult ? <ToolResult state={state} muted={muted} /> : null}
            {state.finalMessage ? (
                <div className="canvas-agent-runtime-final">
                    <div className="canvas-agent-runtime-final-heading">
                        <CheckCircle2 className="canvas-agent-runtime-final-icon" />
                        <strong className="canvas-agent-runtime-final-title">{state.verification?.status === "satisfied" ? "交付已验收" : "Agent 回复"}</strong>
                    </div>
                    <p className="canvas-agent-runtime-final-copy">{state.finalMessage}</p>
                </div>
            ) : null}
            {state.failureCode ? (
                <div className="canvas-agent-runtime-failure">
                    <XCircle className="canvas-agent-runtime-failure-icon" />
                    <span className="canvas-agent-runtime-failure-copy">运行失败：{state.failureCode}</span>
                </div>
            ) : null}
        </div>
    );
}

function AgentApprovalCard({ state, busy, muted, onDecision }: { state: AgentRuntimeState; busy: boolean; muted: string; onDecision: (decision: "approved" | "rejected") => void }) {
    const call = state.pendingToolCall;
    if (!call) return null;
    return (
        <div className="canvas-agent-runtime-approval">
            <div className="canvas-agent-runtime-approval-heading">
                <ShieldCheck className="canvas-agent-runtime-approval-icon" />
                <div className="canvas-agent-runtime-approval-copy">
                    <strong className="canvas-agent-runtime-approval-title">等待确认</strong>
                    <span className="canvas-agent-runtime-approval-tool" style={{ color: muted }}>
                        {call.toolName} · 版本 {call.actionVersion}
                    </span>
                </div>
            </div>
            <details className="canvas-agent-runtime-arguments">
                <summary className="canvas-agent-runtime-arguments-summary">查看确定性参数</summary>
                <pre className="canvas-agent-runtime-arguments-code">{JSON.stringify(call.arguments, null, 2)}</pre>
            </details>
            <div className="canvas-agent-runtime-approval-actions">
                <Button className="canvas-agent-runtime-reject" disabled={busy} onClick={() => onDecision("rejected")}>
                    拒绝执行
                </Button>
                <Button className="canvas-agent-runtime-approve" type="primary" disabled={busy} onClick={() => onDecision("approved")}>
                    批准执行
                </Button>
            </div>
        </div>
    );
}

function ToolResult({ state, muted }: { state: AgentRuntimeState; muted: string }) {
    const result = state.lastToolResult;
    if (!result) return null;
    return (
        <div className="canvas-agent-runtime-tool-result">
            <span className="canvas-agent-runtime-tool-result-state" data-success={result.succeeded}>
                {result.succeeded ? "工具已完成" : "工具失败"}
            </span>
            <span className="canvas-agent-runtime-tool-result-id" style={{ color: muted }}>
                {result.toolCallId}
                {result.errorCode ? ` · ${result.errorCode}` : ""}
            </span>
        </div>
    );
}

function isTerminal(status: AgentRuntimeState["status"]) {
    return status === "succeeded" || status === "failed" || status === "cancelled";
}
function runtimeStatus(status: AgentRuntimeState["status"]) {
    return ({ queued: "已排队", running: "正在执行", waiting_approval: "等待确认", waiting_tool: "正在调用工具", succeeded: "已完成", failed: "已失败", cancelled: "已取消" } satisfies Record<AgentRuntimeState["status"], string>)[status];
}
function eventLabel(kind: AgentRuntimeEvent["kind"]) {
    return (
        {
            "run.created": "运行已创建",
            "run.status_changed": "运行状态更新",
            "model.delta": "模型输出已持久化",
            "tool.call": "工具调用已冻结",
            "approval.required": "需要用户确认",
            "approval.decided": "审批已记录",
            "tool.started": "工具开始执行",
            "tool.result": "工具结果已记录",
            "checkpoint.saved": "恢复点已保存",
            "run.completed": "交付验收通过",
            "run.failed": "运行失败",
        } satisfies Record<AgentRuntimeEvent["kind"], string>
    )[kind];
}
