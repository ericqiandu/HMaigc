import { useCallback, useEffect, useMemo, useRef, useState, type SetStateAction } from "react";
import { Bot, CircleAlert, History, Plus, XCircle } from "lucide-react";
import { App, Button, Tooltip } from "antd";
import { nanoid } from "nanoid";

import { canvasThemes } from "@/lib/canvas-theme";
import { deriveCanvasAgentSelectionDefaults } from "@/lib/canvas/canvas-agent-composer-context";
import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection } from "@/lib/canvas/canvas-agent-draft";
import { agentRuntimeUserErrorMessage } from "@/lib/canvas/canvas-agent-runtime-event";
import { uploadImage } from "@/services/image-storage";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage, AgentRuntimeStartConfiguration, AgentRuntimeState, AgentRuntimeView, AgentThreadHistoryTurn } from "@/services/api/agent-runtime";
import type { PlatformSkill } from "@/services/api/skills";
import { useEffectiveConfig, type AiConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAgentGenerationModels, CanvasAgentLaunchRequest, CanvasAgentSkillSelection, CanvasNodeData } from "@/types/canvas";
import { AgentChatComposer } from "./canvas-agent-chat-ui";
import { AgentApprovalCard } from "./agent-approval-card";
import { AgentRuntimeActivity } from "./agent-runtime-activity";
import { AgentThinkingTrace } from "./agent-thinking-trace";
import { CanvasAgentComposerControls } from "./canvas-agent-composer-controls";
import { buildAgentStartConfiguration, encodeAgentModelSelection } from "./canvas-agent-runtime-configuration";
import { CanvasAgentSelectionSummary } from "./canvas-agent-selection-summary";
import { AgentRuntimeHistoryList } from "./agent-runtime-history-list";
import type { AgentConversationState } from "./agent-conversation-reducer";
import { AgentClarificationHistory, AgentClarificationPanel, AgentClarificationStatus } from "./agent-clarification-panel";
import { agentRuntimeUsesLiveSubscription, useAgentRuntime } from "./use-agent-runtime";
import "./canvas-agent-panel.css";

export type CanvasManagedAgentWorkspaceProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    getSelectedNodes: () => CanvasNodeData[];
    activatedSkills: PlatformSkill[];
    agentLaunchRequest?: CanvasAgentLaunchRequest;
    onAgentLaunchHandled?: (launchRequestId: string) => void;
    onBeforeRun?: () => Promise<void>;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
    runtimeClient?: AgentRuntimeClient;
    runtimeStorage?: AgentRuntimeHandleStorage;
};

export function CanvasManagedAgentWorkspace({
    projectId,
    canvasRevision,
    selectedNodeIds,
    getSelectedNodes,
    activatedSkills,
    agentLaunchRequest,
    onAgentLaunchHandled,
    onBeforeRun,
    onRuntimeEvent,
    runtimeClient,
    runtimeStorage,
}: CanvasManagedAgentWorkspaceProps) {
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
                image: encodeAgentModelSelection(runtime.pendingConfiguration.generationModels.image),
                video: encodeAgentModelSelection(runtime.pendingConfiguration.generationModels.video),
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
                const submitted = await runtime.submit(agentLaunchRequest.prompt, buildAgentStartConfiguration(launchModels, launchSkills, launchAttachments, agentLaunchRequest.executionMode));
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
            configuration = buildAgentStartConfiguration(agentModels, selectedSkills, draft.attachments, executionMode);
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
        <div className="canvas-agent-workspace canvas-agent-managed-workspace">
            <div className="canvas-agent-workspace-toolbar" aria-label="网站 Agent 对话操作">
                <span className="canvas-agent-workspace-host-status">网站 Agent</span>
                <div className="canvas-agent-workspace-toolbar-actions">
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
                </div>
            </div>

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
                        view={runtime.view}
                        runId={runtime.view.run.id}
                        state={runtime.view.state}
                        conversation={runtime.conversation}
                        meaningfulEvents={runtime.meaningfulEvents}
                        events={runtime.events}
                        turns={runtime.turns}
                        connection={runtime.connection}
                        muted={theme.node.muted}
                        config={effectiveConfig}
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
                {runtime.view?.state.status === "waiting_approval" && runtime.view.state.pendingToolCall && runtime.view.pendingApproval ? (
                    <AgentApprovalCard call={runtime.view.state.pendingToolCall} approval={runtime.view.pendingApproval} busy={runtime.busy} muted={theme.node.muted} onDecision={runtime.decideApproval} />
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
                selectionSummary={
                    active ? undefined : <CanvasAgentSelectionSummary config={effectiveConfig} models={agentModels} selectedSkills={selectedSkills} disabled={runtime.busy} onModelsChange={setAgentModels} onSkillsChange={setSelectedSkills} />
                }
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
        </div>
    );
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
    view,
    runId,
    state,
    conversation,
    meaningfulEvents,
    events,
    turns,
    connection,
    muted,
    config,
}: {
    view: AgentRuntimeView;
    runId: string;
    state: AgentRuntimeState;
    conversation: AgentConversationState;
    meaningfulEvents: AgentRuntimeEvent[];
    events: AgentRuntimeEvent[];
    turns: AgentThreadHistoryTurn[];
    connection: string;
    muted: string;
    config: AiConfig;
}) {
    const liveSubscription = agentRuntimeUsesLiveSubscription(state.status);
    const hasVisibleReply = conversation.items.some((item) => item.text.length > 0) || Boolean(state.finalMessage?.trim());
    const submittedModels: CanvasAgentGenerationModels = {
        image: encodeAgentModelSelection(state.configuration.generationModels.image),
        video: encodeAgentModelSelection(state.configuration.generationModels.video),
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
            <AgentRuntimeActivity runId={runId} turns={turns} events={events} view={view} muted={muted} />
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
    return agentRuntimeUserErrorMessage(failureCode) ?? "运行失败，请重试；若问题持续，请联系管理员查看诊断信息。";
}
