import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEventHandler, type PointerEventHandler, type SetStateAction } from "react";
import { Bot, CircleAlert, History, PanelRightClose, Plus, ShieldCheck, XCircle } from "lucide-react";
import { App, Button, Tooltip } from "antd";
import { motion } from "motion/react";
import { nanoid } from "nanoid";

import { canvasThemes } from "@/lib/canvas-theme";
import { CANVAS_AGENT_DOCK_MAX_WIDTH, CANVAS_AGENT_DOCK_MIN_WIDTH } from "@/lib/canvas/canvas-agent-dock";
import { deriveCanvasAgentSelectionDefaults } from "@/lib/canvas/canvas-agent-composer-context";
import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection } from "@/lib/canvas/canvas-agent-draft";
import { uploadImage } from "@/services/image-storage";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import type { AgentPendingApproval, AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage, AgentRuntimeStartConfiguration, AgentRuntimeState, AgentThreadHistoryTurn } from "@/services/api/agent-runtime";
import type { AgentProductionClient } from "@/services/api/agent-production";
import type { PlatformSkill } from "@/services/api/skills";
import { decodeChannelModel, useEffectiveConfig, type AiConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAgentGenerationModels, CanvasAgentLaunchRequest, CanvasAgentSkillSelection, CanvasNodeData } from "@/types/canvas";
import { AgentChatComposer } from "./canvas-agent-chat-ui";
import { AgentThinkingTrace } from "./agent-thinking-trace";
import { CanvasAgentComposerControls } from "./canvas-agent-composer-controls";
import { CanvasAgentSelectionSummary } from "./canvas-agent-selection-summary";
import { AgentRuntimeHistoryList } from "./agent-runtime-history-list";
import type { AgentConversationState } from "./agent-conversation-reducer";
import { AgentClarificationHistory, AgentClarificationPanel, AgentClarificationStatus } from "./agent-clarification-panel";
import { AgentApprovalSummary } from "./agent-approval-summary";
import { agentRuntimeUsesLiveSubscription, useAgentRuntime } from "./use-agent-runtime";
import "./canvas-agent-panel.css";

const AgentProductionTimeline = lazy(() => import("./agent-production-card").then((module) => ({ default: module.AgentProductionTimeline })));

export const CANVAS_AGENT_PANEL_MOTION_MS = 240;

type CanvasAssistantPanelProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    getSelectedNodes: () => CanvasNodeData[];
    activatedSkills: PlatformSkill[];
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
    productionClient?: AgentProductionClient;
};

export function CanvasAssistantPanel({
    projectId,
    canvasRevision,
    selectedNodeIds,
    getSelectedNodes,
    activatedSkills,
    closing,
    width,
    onResizeStart,
    onResizeKeyDown,
    onCollapse,
    agentLaunchRequest,
    onAgentLaunchHandled,
    onBeforeRun,
    onRuntimeEvent,
    runtimeClient,
    runtimeStorage,
    productionClient,
}: CanvasAssistantPanelProps) {
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
        if (!runtime.restored || runtime.pendingUserMessage || agentLaunchRequest) return;
        const selectionDefaults = deriveCanvasAgentSelectionDefaults(getSelectedNodes(), activatedSkills);
        setDraft((current) => {
            const generationModels = current.generationModels.video ? current.generationModels : { ...current.generationModels, video: selectionDefaults.generationModels.video };
            const skillSelections = current.skillSelections.length ? current.skillSelections : selectionDefaults.skillSelections;
            if (generationModels === current.generationModels && skillSelections === current.skillSelections) return current;
            return { ...current, generationModels, skillSelections };
        });
    }, [activatedSkills, agentLaunchRequest, getSelectedNodes, runtime.pendingUserMessage, runtime.restored, selectedNodeIds]);

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
                        <AgentRunContent
                            key={runtime.view.run.id}
                            runId={runtime.view.run.id}
                            state={runtime.view.state}
                            conversation={runtime.conversation}
                            meaningfulEvents={runtime.meaningfulEvents}
                            events={runtime.events}
                            turns={runtime.turns}
                            connection={runtime.connection}
                            muted={theme.node.muted}
                            config={effectiveConfig}
                            productionClient={productionClient}
                            onProductionRefresh={runtime.refreshCurrentRun}
                        />
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
                        <AgentApprovalCard state={runtime.view.state} approval={runtime.view.pendingApproval} busy={runtime.busy} muted={theme.node.muted} onDecision={(decision) => void runtime.decideApproval(decision)} />
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
                    running={active}
                    disabled={!runtime.restored}
                    placeholder={active ? "当前任务运行中" : "描述你想让 Agent 如何操作画布"}
                    theme={theme}
                    onPromptChange={setPrompt}
                    onSubmit={submit}
                    onStop={() => void runtime.interrupt()}
                    onAddFiles={addAttachments}
                    onRemoveAttachment={(id) => setDraft((current) => ({ ...current, attachments: current.attachments.filter((attachment) => attachment.id !== id) }))}
                    onDeleteBackwardAtStart={() => {
                        const next = removeLastCanvasAgentDraftSelection(draft);
                        if (!next) return false;
                        setDraft(next);
                        return true;
                    }}
                    submitReady={Boolean(prompt.trim())}
                    selectionSummary={active ? undefined : <CanvasAgentSelectionSummary config={effectiveConfig} models={agentModels} selectedSkills={selectedSkills} disabled={runtime.busy} onModelsChange={setAgentModels} onSkillsChange={setSelectedSkills} />}
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

function AgentRunContent({
    runId,
    state,
    conversation,
    meaningfulEvents,
    events,
    turns,
    connection,
    muted,
    config,
    productionClient,
    onProductionRefresh,
}: {
    runId: string;
    state: AgentRuntimeState;
    conversation: AgentConversationState;
    meaningfulEvents: AgentRuntimeEvent[];
    events: AgentRuntimeEvent[];
    turns: AgentThreadHistoryTurn[];
    connection: string;
    muted: string;
    config: AiConfig;
    productionClient?: AgentProductionClient;
    onProductionRefresh: () => Promise<void>;
}) {
    const liveSubscription = agentRuntimeUsesLiveSubscription(state.status);
    const hasVisibleReply = conversation.items.some((item) => item.text.length > 0) || Boolean(state.finalMessage?.trim());
    const submittedModels: CanvasAgentGenerationModels = {
        image: encodePendingModel(state.configuration.generationModels.image),
        video: encodePendingModel(state.configuration.generationModels.video),
    };
    const submittedSkills = state.configuration.skills.map(({ dir, name, description }) => ({ dir, name, description }));
    return (
        <div className="canvas-agent-runtime-run">
            <article className="canvas-agent-runtime-user-message" aria-label="你的消息">
                <span className="canvas-agent-runtime-message-label" style={{ color: muted }}>
                    你
                </span>
                <CanvasAgentSelectionSummary config={config} models={submittedModels} selectedSkills={submittedSkills} readOnly ariaLabel="本轮已提交配置" />
                <p className="canvas-agent-runtime-message-copy">{state.userMessage}</p>
            </article>
            <AgentThinkingTrace
                status={state.status}
                stepNumber={state.stepNumber}
                maxSteps={state.maxSteps}
                hasVisibleReply={hasVisibleReply}
                deliveryVerified={state.verification?.status === "satisfied"}
                reconnecting={liveSubscription && connection === "reconnecting"}
                events={meaningfulEvents}
            />
            {conversation.items.map((item) => {
                return (
                    <article key={item.id} className="canvas-agent-runtime-final" data-status={item.status} aria-label="Agent 回复">
                        <p className="canvas-agent-runtime-final-copy">{item.text}</p>
                    </article>
                );
            })}
            {conversation.protocolError ? (
                <div className="canvas-agent-runtime-error" role="alert">
                    Agent 流式回复与最终事实冲突，请重新读取本轮运行。
                </div>
            ) : null}
            <Suspense fallback={<div className="agent-production-card-loading" role="status">正在加载阶段产物</div>}>
                <AgentProductionTimeline runId={runId} turns={turns} events={events} client={productionClient} onRefresh={onProductionRefresh} />
            </Suspense>
            {state.lastToolResult ? <ToolResult state={state} muted={muted} /> : null}
            {state.finalMessage && !conversation.items.some((item) => (state.finalMessage ?? "").startsWith(item.text)) ? (
                <article className="canvas-agent-runtime-final" aria-label="Agent 回复">
                    <p className="canvas-agent-runtime-final-copy">{state.finalMessage}</p>
                </article>
            ) : null}
            {state.failureCode ? (
                <div className="canvas-agent-runtime-failure">
                    <XCircle className="canvas-agent-runtime-failure-icon" />
                    <span className="canvas-agent-runtime-failure-copy">{agentFailureMessage(state.failureCode)}</span>
                </div>
            ) : null}
        </div>
    );
}

function agentFailureMessage(failureCode: string): string {
    return knownAgentErrorMessage(failureCode) ?? `运行失败：${failureCode}`;
}

function knownAgentErrorMessage(errorCode: string): string | undefined {
    if (errorCode === "insufficient_credits") return "余额不足";
    if (errorCode === "production_previous_billing_unresolved") return "上一次生成费用仍待确认，请先处理后再重试";
    return undefined;
}

function AgentApprovalCard({ state, approval, busy, muted, onDecision }: { state: AgentRuntimeState; approval?: AgentPendingApproval; busy: boolean; muted: string; onDecision: (decision: "approved" | "rejected") => void }) {
    const call = state.pendingToolCall;
    if (!call || !approval) return null;
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
            <AgentApprovalSummary approval={approval} />
            <section className="canvas-agent-runtime-approval-effect" aria-label="审批影响">
                <strong className="canvas-agent-runtime-approval-effect-summary">{approval.effect.summary}</strong>
                <span className="canvas-agent-runtime-approval-effect-targets" style={{ color: muted }}>
                    {approval.effect.targetIds.join(" · ")}
                </span>
            </section>
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
                {result.errorCode ? ` · ${knownAgentErrorMessage(result.errorCode) ?? result.errorCode}` : ""}
            </span>
        </div>
    );
}
