import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEventHandler, type PointerEventHandler, type SetStateAction } from "react";
import { Bot, CheckCircle2, ChevronDown, CircleAlert, History, PanelRightClose, Plus, ShieldCheck, XCircle } from "lucide-react";
import { App, Button, Tooltip } from "antd";
import { motion } from "motion/react";
import { nanoid } from "nanoid";

import { canvasThemes } from "@/lib/canvas-theme";
import { CANVAS_AGENT_DOCK_MAX_WIDTH, CANVAS_AGENT_DOCK_MIN_WIDTH } from "@/lib/canvas/canvas-agent-dock";
import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection } from "@/lib/canvas/canvas-agent-draft";
import { uploadImage } from "@/services/image-storage";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage, AgentRuntimeStartConfiguration, AgentRuntimeState } from "@/services/api/agent-runtime";
import { decodeChannelModel, useEffectiveConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAgentGenerationModels, CanvasAgentLaunchRequest, CanvasAgentSkillSelection } from "@/types/canvas";
import { AgentChatComposer } from "./canvas-agent-chat-ui";
import { CanvasAgentComposerControls } from "./canvas-agent-composer-controls";
import { CanvasAgentSelectionSummary } from "./canvas-agent-selection-summary";
import { AgentRuntimeHistoryList } from "./agent-runtime-history-list";
import { AgentClarificationHistory, AgentClarificationPanel, AgentClarificationStatus } from "./agent-clarification-panel";
import { agentRuntimeStatusLabel, agentRuntimeUsesLiveSubscription, useAgentRuntime } from "./use-agent-runtime";
import "./canvas-agent-panel.css";

export const CANVAS_AGENT_PANEL_MOTION_MS = 240;

type CanvasAssistantPanelProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    closing: boolean;
    width: number;
    onResizeStart: PointerEventHandler<HTMLDivElement>;
    onResizeKeyDown: KeyboardEventHandler<HTMLDivElement>;
    onCollapse: () => void;
    agentLaunchRequest?: CanvasAgentLaunchRequest;
    onAgentLaunchHandled?: (launchRequestId: string) => void;
    onBeforeRun?: () => Promise<void>;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
    runtimeClient?: AgentRuntimeClient;
    runtimeStorage?: AgentRuntimeHandleStorage;
};

export function CanvasAssistantPanel({ projectId, canvasRevision, selectedNodeIds, closing, width, onResizeStart, onResizeKeyDown, onCollapse, agentLaunchRequest, onAgentLaunchHandled, onBeforeRun, onRuntimeEvent, runtimeClient, runtimeStorage }: CanvasAssistantPanelProps) {
    const { message } = App.useApp();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const effectiveConfig = useEffectiveConfig();
    const [draft, setDraft] = useState(createEmptyCanvasAgentDraft);
    const [configurationError, setConfigurationError] = useState("");
    const [historyOpen, setHistoryOpen] = useState(false);
    const [clarificationHistoryOpen, setClarificationHistoryOpen] = useState(false);
    const launchAttemptRef = useRef("");
    const runtime = useAgentRuntime({ canvasId: projectId, client: runtimeClient, storage: runtimeStorage, onRuntimeEvent });
    const active = Boolean(runtime.view && !runtime.terminal);
    const { prompt, generationModels: agentModels, skillSelections: selectedSkills, executionMode } = draft;
    const setPrompt = useCallback((value: SetStateAction<string>) => setDraft((current) => ({ ...current, prompt: typeof value === "function" ? value(current.prompt) : value })), []);
    const setAgentModels = useCallback((generationModels: CanvasAgentGenerationModels) => setDraft((current) => ({ ...current, generationModels })), []);
    const setSelectedSkills = useCallback((skillSelections: CanvasAgentSkillSelection[]) => setDraft((current) => ({ ...current, skillSelections })), []);

    useEffect(() => {
        if (!runtime.pendingUserMessage) return;
        setPrompt((current) => current || runtime.pendingUserMessage);
        if (runtime.pendingConfiguration) {
            setAgentModels({
                image: encodePendingModel(runtime.pendingConfiguration.generationModels.image),
                video: encodePendingModel(runtime.pendingConfiguration.generationModels.video),
            });
            setSelectedSkills(runtime.pendingConfiguration.skillDirs.map((dir) => ({ dir, name: dir, description: "" })));
            setDraft((current) => ({
                ...current,
                attachments: runtime.pendingConfiguration?.attachments.map((attachment) => ({ id: attachment.resourceId, resourceId: attachment.resourceId, name: attachment.name, url: resourceFileUrl(attachment.resourceId) })) || [],
                executionMode: runtime.pendingConfiguration?.executionMode || current.executionMode,
            }));
        }
    }, [runtime.pendingConfiguration, runtime.pendingUserMessage]);

    useEffect(() => setHistoryOpen(false), [projectId]);

    useEffect(() => {
        if (!agentLaunchRequest || !runtime.restored || launchAttemptRef.current === agentLaunchRequest.id) return;
        launchAttemptRef.current = agentLaunchRequest.id;
        const launchModels = { ...agentLaunchRequest.generationModels };
        const launchSkills = agentLaunchRequest.skillDirs.map((dir) => ({ dir, name: dir, description: "" }));
        const launchAttachments = agentLaunchRequest.attachments.map((attachment) => ({ id: attachment.resourceId, resourceId: attachment.resourceId, name: attachment.name, url: resourceFileUrl(attachment.resourceId) }));
        setDraft({ prompt: agentLaunchRequest.prompt, generationModels: launchModels, skillSelections: launchSkills, attachments: launchAttachments, executionMode: agentLaunchRequest.executionMode });
        void (async () => {
            try {
                await onBeforeRun?.();
                const submitted = await runtime.submit(agentLaunchRequest.prompt, buildStartConfiguration(launchModels, launchSkills, launchAttachments, agentLaunchRequest.executionMode));
                if (!submitted) return;
                setDraft((current) => ({ ...current, prompt: "", attachments: [] }));
                onAgentLaunchHandled?.(agentLaunchRequest.id);
            } catch (cause) {
                launchAttemptRef.current = "";
                setConfigurationError(cause instanceof Error ? cause.message : "当前画布同步失败，Agent 未启动");
            }
        })();
    }, [agentLaunchRequest, onAgentLaunchHandled, onBeforeRun, runtime.restored, runtime.submit]);

    const submit = async () => {
        let configuration: AgentRuntimeStartConfiguration;
        try {
            configuration = buildStartConfiguration(agentModels, selectedSkills, draft.attachments, executionMode);
            setConfigurationError("");
        } catch (cause) {
            setConfigurationError(cause instanceof Error ? cause.message : "Agent 运行配置无效");
            return;
        }
        try {
            await onBeforeRun?.();
        } catch (cause) {
            setConfigurationError(cause instanceof Error ? cause.message : "当前画布同步失败，Agent 未启动");
            return;
        }
        if (!(await runtime.submit(prompt, configuration))) return;
        if (agentLaunchRequest?.prompt === prompt) onAgentLaunchHandled?.(agentLaunchRequest.id);
        setDraft((current) => ({ ...current, prompt: "", attachments: [] }));
    };

    const addAttachments = async (files: FileList | File[] | null) => {
        const images = Array.from(files || []).filter((file) => file.type.startsWith("image/"));
        if (!images.length) return;
        const remaining = 4 - draft.attachments.length;
        if (remaining <= 0) {
            message.warning("一次最多添加 4 张参考图片");
            return;
        }
        try {
            const uploaded = await Promise.all(
                images.slice(0, remaining).map(async (file) => {
                    const result = await uploadImage(file);
                    const resourceId = resourceIdFromStorageKey(result.storageKey);
                    if (!resourceId) throw new Error(`参考图片“${file.name}”尚未保存到账号资源，请检查存储配置后重试`);
                    return { id: nanoid(), resourceId, name: file.name, url: result.url || resourceFileUrl(resourceId) };
                }),
            );
            setDraft((current) => ({ ...current, attachments: [...current.attachments, ...uploaded] }));
            if (images.length > remaining) message.warning("一次最多添加 4 张参考图片");
        } catch (cause) {
            message.error(cause instanceof Error ? cause.message : "参考图片上传失败");
        }
    };

    return (
        <motion.div
            className="canvas-agent-runtime-layout flex shrink-0"
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: closing ? 0 : width, opacity: closing ? 0 : 1 }}
            transition={{ duration: CANVAS_AGENT_PANEL_MOTION_MS / 1000, ease: [0.2, 0.8, 0.2, 1] }}
            style={{ overflow: "clip", pointerEvents: closing ? "none" : undefined, position: "relative" }}
        >
            <div
                className="canvas-agent-resize-handle"
                role="separator"
                aria-label="调整 Agent 面板宽度"
                aria-orientation="vertical"
                aria-valuemin={CANVAS_AGENT_DOCK_MIN_WIDTH}
                aria-valuemax={CANVAS_AGENT_DOCK_MAX_WIDTH}
                aria-valuenow={width}
                tabIndex={0}
                onPointerDown={onResizeStart}
                onKeyDown={onResizeKeyDown}
            />
            <motion.aside
                className="canvas-agent-shell relative flex min-w-0 flex-1 flex-col overflow-hidden border"
                initial={{ x: 48 }}
                animate={{ x: closing ? 28 : 0 }}
                transition={{ duration: CANVAS_AGENT_PANEL_MOTION_MS / 1000, ease: [0.2, 0.8, 0.2, 1] }}
                style={{ background: theme.node.panel, color: theme.node.text, borderColor: theme.node.stroke, boxShadow: `0 24px 72px ${theme.spatial.shadow}` }}
                aria-label="画布 Agent"
            >
                <header className="canvas-agent-runtime-header canvas-agent-header">
                    <strong className="canvas-agent-header-title">Agent 画布助手</strong>
                    <div className="canvas-agent-runtime-header-actions">
                        <Tooltip title="历史对话">
                            <Button
                                className="canvas-agent-runtime-icon-button"
                                type="text"
                                icon={<History className="canvas-agent-runtime-button-icon" />}
                                disabled={!runtime.restored || runtime.busy}
                                onClick={() => setHistoryOpen((open) => !open)}
                                aria-label="历史对话"
                                aria-pressed={historyOpen}
                            />
                        </Tooltip>
                        <Tooltip title={active ? "当前任务完成后才能新建对话" : "新建对话"}>
                            <Button
                                className="canvas-agent-runtime-icon-button"
                                type="text"
                                icon={<Plus className="canvas-agent-runtime-button-icon" />}
                                disabled={active || runtime.busy}
                                onClick={() => {
                                    setHistoryOpen(false);
                                    void runtime.newThread();
                                }}
                                aria-label="新建 Agent 对话"
                            />
                        </Tooltip>
                        <Tooltip title="收起">
                            <Button className="canvas-agent-runtime-icon-button" type="text" icon={<PanelRightClose className="canvas-agent-runtime-button-icon" />} onClick={onCollapse} aria-label="收起 Agent" />
                        </Tooltip>
                    </div>
                </header>

                <div className="canvas-agent-context" style={{ color: theme.node.muted }}>
                    <strong className="canvas-agent-runtime-context-label" style={{ color: theme.node.text }}>
                        将读取
                    </strong>
                    <span className="canvas-agent-runtime-context-copy">
                        当前画布 v{canvasRevision} · 已选 {selectedNodeIds.size} 个节点
                    </span>
                    <span className="canvas-agent-runtime-context-state">{runtime.threadId ? "对话已建立" : "等待任务"}</span>
                </div>

                <section className="canvas-agent-runtime-content canvas-agent-chat-list thin-scrollbar min-h-0 flex-1">
                    {historyOpen ? (
                        <AgentRuntimeHistoryList
                            items={runtime.threads}
                            selectedThreadId={runtime.selectedThreadId}
                            loading={runtime.historyLoading}
                            error={runtime.historyError}
                            onSelect={(item) => {
                                runtime.selectThread(item);
                                setHistoryOpen(false);
                            }}
                            onRetry={() => void runtime.reloadThreads()}
                        />
                    ) : !runtime.view ? (
                        <AgentEmptyState restored={runtime.restored} muted={theme.node.muted} onSuggestion={setPrompt} />
                    ) : (
                        <AgentRunContent state={runtime.view.state} events={runtime.events} connection={runtime.connection} muted={theme.node.muted} />
                    )}
                    {!historyOpen && runtime.view?.state.clarificationHistory.length ? <AgentClarificationHistory history={runtime.view.state.clarificationHistory} open={clarificationHistoryOpen} onOpenChange={setClarificationHistoryOpen} /> : null}
                    {!historyOpen && runtime.view?.state.status === "waiting_input" ? <AgentClarificationStatus /> : null}
                    {(runtime.error && runtime.view?.state.status !== "waiting_input") || configurationError ? (
                        <div className="canvas-agent-runtime-error" role="alert">
                            <CircleAlert className="canvas-agent-runtime-error-icon" />
                            <div className="canvas-agent-runtime-error-content">
                                <span className="canvas-agent-runtime-error-copy">{configurationError || runtime.error}</span>
                            </div>
                        </div>
                    ) : null}
                    {runtime.view?.state.status === "waiting_approval" && runtime.view.state.pendingToolCall ? (
                        <AgentApprovalCard state={runtime.view.state} busy={runtime.busy} muted={theme.node.muted} onDecision={(decision) => void runtime.decideApproval(decision)} />
                    ) : null}
                </section>

                <div className="canvas-agent-runtime-interaction">
                    {!historyOpen && runtime.view?.state.status === "waiting_input" && runtime.view.state.pendingClarification ? (
                        <AgentClarificationPanel pending={runtime.view.state.pendingClarification} history={[]} busy={runtime.busy} error={runtime.error} onRespond={runtime.submitClarificationResponse} />
                    ) : null}
                </div>

                <AgentChatComposer
                    prompt={prompt}
                    attachments={draft.attachments}
                    sending={runtime.busy}
                    disabled={!runtime.restored || active}
                    placeholder={active ? "当前任务运行中" : "描述你想让 Agent 如何操作画布"}
                    theme={theme}
                    onPromptChange={setPrompt}
                    onSubmit={submit}
                    onAddFiles={addAttachments}
                    onRemoveAttachment={(id) => setDraft((current) => ({ ...current, attachments: current.attachments.filter((attachment) => attachment.id !== id) }))}
                    onDeleteBackwardAtStart={() => {
                        const next = removeLastCanvasAgentDraftSelection(draft);
                        if (!next) return false;
                        setDraft(next);
                        return true;
                    }}
                    submitReady={Boolean(prompt.trim())}
                    selectionSummary={<CanvasAgentSelectionSummary config={effectiveConfig} models={agentModels} selectedSkills={selectedSkills} disabled={active || runtime.busy} onModelsChange={setAgentModels} onSkillsChange={setSelectedSkills} />}
                    left={
                        <CanvasAgentComposerControls
                            config={effectiveConfig}
                            disabled={!runtime.restored || active || runtime.busy}
                            models={agentModels}
                            selectedSkills={selectedSkills}
                            executionMode={executionMode}
                            onModelsChange={setAgentModels}
                            onSkillsChange={setSelectedSkills}
                            onExecutionModeChange={(mode) => setDraft((current) => ({ ...current, executionMode: mode }))}
                        />
                    }
                />
            </motion.aside>
        </motion.div>
    );
}

function emptyStartConfiguration(): AgentRuntimeStartConfiguration {
    return { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" };
}

function buildStartConfiguration(
    models: CanvasAgentGenerationModels,
    skills: CanvasAgentSkillSelection[],
    attachments: Array<{ resourceId?: string; name: string }>,
    executionMode: AgentRuntimeStartConfiguration["executionMode"],
): AgentRuntimeStartConfiguration {
    const configuration = emptyStartConfiguration();
    if (models.image) configuration.generationModels.image = requiredModelSelection(models.image, "图片模型");
    if (models.video) configuration.generationModels.video = requiredModelSelection(models.video, "视频模型");
    configuration.skillDirs = skills
        .map((skill) => skill.dir.trim())
        .filter(Boolean)
        .sort();
    configuration.attachments = attachments.map((attachment) => {
        const resourceId = attachment.resourceId?.trim();
        if (!resourceId) throw new Error(`参考图片“${attachment.name}”缺少账号资源事实`);
        return { resourceId, name: attachment.name.trim() || "参考图片" };
    });
    configuration.executionMode = executionMode;
    return configuration;
}

function requiredModelSelection(value: string, label: string) {
    const selected = decodeChannelModel(value);
    if (!selected?.channelId.trim() || !selected.model.trim()) throw new Error(`${label}配置无效，请重新选择`);
    return selected;
}

function encodePendingModel(selection: { channelId: string; model: string } | undefined) {
    return selection ? `${selection.channelId}::${selection.model}` : "";
}

function AgentEmptyState({ restored, muted, onSuggestion }: { restored: boolean; muted: string; onSuggestion: (suggestion: string) => void }) {
    const suggestions = ["搭建短剧工作流", "整理当前画布", "生成镜头分镜", "检查节点连线"];
    return (
        <div className="canvas-agent-runtime-empty">
            <span className="canvas-agent-runtime-empty-avatar">
                <Bot className="canvas-agent-runtime-empty-icon" />
            </span>
            <strong className="canvas-agent-runtime-empty-title">{restored ? "Agent" : "正在恢复运行事实"}</strong>
            {restored ? (
                <div className="canvas-agent-runtime-suggestions">
                    {suggestions.map((suggestion) => (
                        <button key={suggestion} type="button" className="canvas-agent-runtime-suggestion" onClick={() => onSuggestion(suggestion)}>
                            {suggestion}
                        </button>
                    ))}
                </div>
            ) : (
                <p className="canvas-agent-runtime-empty-description" style={{ color: muted }}>
                    正在读取本画布最后一次可恢复运行。
                </p>
            )}
        </div>
    );
}

function AgentRunContent({ state, events, connection, muted }: { state: AgentRuntimeState; events: AgentRuntimeEvent[]; connection: string; muted: string }) {
    const status = agentRuntimeStatusLabel(state.status);
    const lastEvents = useMemo(() => events.slice(-8), [events]);
    const liveSubscription = agentRuntimeUsesLiveSubscription(state.status);
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
                        第 {state.stepNumber} / {state.maxSteps} 步{liveSubscription && connection === "reconnecting" ? " · 正在重连" : ""}
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
                            {agentRuntimeEmptyEventLabel(state.status)}
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

function agentRuntimeEmptyEventLabel(status: AgentRuntimeState["status"]) {
    if (status === "waiting_input") return "等待你的回答";
    if (status === "waiting_approval") return "等待你的确认";
    if (status === "waiting_tool") return "等待工具结果";
    return "等待新的持久化事件";
}
function eventLabel(kind: AgentRuntimeEvent["kind"]) {
    return (
        {
            "run.started": "运行已创建",
            "run.completed": "交付验收通过",
            "run.failed": "运行失败",
            "run.interrupted": "运行已停止",
            "item.started": "执行项已开始",
            "item.delta": "执行项已更新",
            "item.completed": "执行项已完成",
            "item.failed": "执行项失败",
            "approval.requested": "需要用户确认",
            "approval.resolved": "审批已记录",
            "state.snapshot": "运行状态已持久化",
        } satisfies Record<AgentRuntimeEvent["kind"], string>
    )[kind];
}
