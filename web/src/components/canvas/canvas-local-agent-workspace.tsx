import { useCallback, useEffect, useState, type FormEvent, type SetStateAction } from "react";
import { CircleAlert, History, Link2, Link2Off, Plus } from "lucide-react";
import { App, Button, Input, Tooltip } from "antd";
import { nanoid } from "nanoid";

import { deriveCanvasAgentSelectionDefaults } from "@/lib/canvas/canvas-agent-composer-context";
import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection } from "@/lib/canvas/canvas-agent-draft";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeView } from "@/services/api/agent-runtime";
import { uploadImage } from "@/services/image-storage";
import type { LocalAgentAttachment } from "@/services/local-agent/local-agent-client";
import type { LocalAgentAuthoritativeToolResult } from "@/services/local-agent/local-agent-bridge";
import type { LocalAgentEvent, LocalAgentThread } from "@/services/local-agent/local-agent-contracts";
import { useEffectiveConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import { canvasThemes } from "@/lib/canvas-theme";
import type { CanvasAgentGenerationModels, CanvasAgentSkillSelection, CanvasNodeData } from "@/types/canvas";
import type { PlatformSkill } from "@/services/api/skills";

import { AgentApprovalCard } from "./agent-approval-card";
import { AgentChatComposer } from "./canvas-agent-chat-ui";
import { AgentRuntimeActivity } from "./agent-runtime-activity";
import { CanvasAgentComposerControls } from "./canvas-agent-composer-controls";
import { buildAgentStartConfiguration } from "./canvas-agent-runtime-configuration";
import { CanvasAgentSelectionSummary } from "./canvas-agent-selection-summary";
import { visibleLocalAgentFinalMessages } from "./local-agent-final-message";
import { useLocalAgentRuntime } from "./use-local-agent-runtime";

export type CanvasLocalAgentWorkspaceProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    getSelectedNodes: () => CanvasNodeData[];
    activatedSkills: PlatformSkill[];
    onBeforeRun?: () => Promise<void>;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
    onToolResult?: (result: LocalAgentAuthoritativeToolResult) => void;
    runtimeClient?: AgentRuntimeClient;
};

export function CanvasLocalAgentWorkspace({ projectId, canvasRevision, selectedNodeIds, getSelectedNodes, activatedSkills, onBeforeRun, onRuntimeEvent, onToolResult, runtimeClient }: CanvasLocalAgentWorkspaceProps) {
    const { message } = App.useApp();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const effectiveConfig = useEffectiveConfig();
    const [draft, setDraft] = useState(createEmptyCanvasAgentDraft);
    const [configurationError, setConfigurationError] = useState("");
    const [historyOpen, setHistoryOpen] = useState(false);
    const local = useLocalAgentRuntime({ canvasId: projectId, beforeToolProposal: onBeforeRun, onRuntimeEvent, onToolResult, runtimeClient });
    const [baseUrl, setBaseUrl] = useState(local.savedConnection?.baseUrl ?? "http://127.0.0.1:17371");
    const [token, setToken] = useState(local.savedConnection?.token ?? "");
    const active = local.activeTurn;
    const { prompt, generationModels: agentModels, skillSelections: selectedSkills, executionMode } = draft;
    const setPrompt = useCallback((value: SetStateAction<string>) => setDraft((current) => ({ ...current, prompt: typeof value === "function" ? value(current.prompt) : value })), []);
    const setAgentModels = useCallback((generationModels: CanvasAgentGenerationModels) => setDraft((current) => ({ ...current, generationModels })), []);
    const setSelectedSkills = useCallback((skillSelections: CanvasAgentSkillSelection[]) => setDraft((current) => ({ ...current, skillSelections })), []);

    useEffect(() => {
        if (active) return;
        const defaults = deriveCanvasAgentSelectionDefaults(getSelectedNodes(), activatedSkills);
        setDraft((current) => {
            const generationModels = current.generationModels.video ? current.generationModels : { ...current.generationModels, video: defaults.generationModels.video };
            const skillSelections = current.skillSelections.length ? current.skillSelections : defaults.skillSelections;
            if (generationModels === current.generationModels && skillSelections === current.skillSelections) return current;
            return { ...current, generationModels, skillSelections };
        });
    }, [activatedSkills, active, getSelectedNodes, selectedNodeIds]);

    const connect = async (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        setConfigurationError("");
        try {
            await local.connect({ baseUrl, token });
            setToken("");
        } catch (cause) {
            setConfigurationError(publicError(cause, "本机 Agent 连接失败"));
        }
    };

    const submit = async () => {
        try {
            const configuration = buildAgentStartConfiguration(agentModels, selectedSkills, draft.attachments, executionMode);
            const attachments = draft.attachments.map(toLocalAttachment);
            setConfigurationError("");
            if (!(await local.submit(prompt, configuration, attachments))) return;
            setDraft((current) => ({ ...current, prompt: "", attachments: [] }));
        } catch (cause) {
            setConfigurationError(publicError(cause, "本机 Agent 运行配置无效"));
        }
    };

    const addAttachments = async (files: FileList | File[] | null) => {
        const images = Array.from(files || []).filter((file) => file.type.startsWith("image/"));
        const remaining = 4 - draft.attachments.length;
        if (!images.length) return;
        if (remaining <= 0) {
            message.warning("一次最多添加 4 张参考图片");
            return;
        }
        try {
            const uploaded = await Promise.all(
                images.slice(0, remaining).map(async (file) => {
                    const result = await uploadImage(file);
                    const resourceId = resourceIdFromStorageKey(result.storageKey);
                    if (!resourceId) throw new Error(`参考图片“${file.name}”尚未保存到账号资源`);
                    return { id: nanoid(), resourceId, name: file.name, mimeType: file.type, url: result.url || resourceFileUrl(resourceId) };
                }),
            );
            setDraft((current) => ({ ...current, attachments: [...current.attachments, ...uploaded] }));
            if (images.length > remaining) message.warning("一次最多添加 4 张参考图片");
        } catch (cause) {
            message.error(publicError(cause, "参考图片上传失败"));
        }
    };

    if (local.connection !== "connected") {
        return (
            <div className="canvas-agent-workspace canvas-agent-local-workspace">
                <form className="canvas-agent-local-connect" onSubmit={connect}>
                    <div className="canvas-agent-local-connect-heading">
                        <Link2 className="canvas-agent-local-connect-icon" aria-hidden="true" />
                        <div className="canvas-agent-local-connect-copy">
                            <strong className="canvas-agent-local-connect-title">连接本机 Codex</strong>
                            <span className="canvas-agent-local-connect-description" style={{ color: theme.node.muted }}>
                                启动 canvas-agent 后粘贴回环地址和本机会话令牌。切换模式不会自动连接。
                            </span>
                        </div>
                    </div>
                    <label className="canvas-agent-local-connect-field">
                        <span className="canvas-agent-local-connect-label">地址</span>
                        <Input className="canvas-agent-local-connect-input" value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} autoComplete="off" />
                    </label>
                    <label className="canvas-agent-local-connect-field">
                        <span className="canvas-agent-local-connect-label">令牌</span>
                        <Input.Password className="canvas-agent-local-connect-input" value={token} onChange={(event) => setToken(event.target.value)} autoComplete="off" visibilityToggle={false} />
                    </label>
                    {configurationError || local.error ? <LocalAgentError message={configurationError || local.error} /> : null}
                    <Button className="canvas-agent-local-connect-submit" type="primary" htmlType="submit" loading={local.connection === "connecting"} disabled={!baseUrl.trim() || !token.trim()}>
                        连接
                    </Button>
                </form>
            </div>
        );
    }

    return (
        <div className="canvas-agent-workspace canvas-agent-local-workspace">
            <div className="canvas-agent-workspace-toolbar" aria-label="本机 Codex 对话操作">
                <span className="canvas-agent-workspace-host-status">本机 Codex 已连接</span>
                <div className="canvas-agent-workspace-toolbar-actions">
                    <Tooltip title="本机历史">
                        <Button className="canvas-agent-runtime-icon-button" type="text" icon={<History className="canvas-agent-runtime-button-icon" />} onClick={() => setHistoryOpen((open) => !open)} aria-label="本机历史" aria-pressed={historyOpen} />
                    </Tooltip>
                    <Tooltip title={active ? "当前 turn 完成后才能新建" : "新建本机对话"}>
                        <Button
                            className="canvas-agent-runtime-icon-button"
                            type="text"
                            icon={<Plus className="canvas-agent-runtime-button-icon" />}
                            disabled={active || local.busy}
                            onClick={() => {
                                local.newThread();
                                setHistoryOpen(false);
                            }}
                            aria-label="新建本机对话"
                        />
                    </Tooltip>
                    <Tooltip title="断开本机 Codex">
                        <Button
                            className="canvas-agent-runtime-icon-button"
                            type="text"
                            icon={<Link2Off className="canvas-agent-runtime-button-icon" />}
                            disabled={local.busy}
                            onClick={() => void local.disconnect().catch(() => undefined)}
                            aria-label="断开本机 Codex"
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
                <span className="canvas-agent-runtime-context-state">{local.selectedThread ? "本机对话已选择" : "等待任务"}</span>
            </div>
            <section className="canvas-agent-runtime-content canvas-agent-chat-list thin-scrollbar min-h-0 flex-1">
                {historyOpen ? (
                    <LocalAgentHistory
                        threads={local.threads}
                        loading={local.historyLoading}
                        selectedThreadId={local.selectedThread?.threadId}
                        onSelect={(threadId) =>
                            void local.selectThread(threadId).then(
                                () => setHistoryOpen(false),
                                () => undefined,
                            )
                        }
                        onRetry={() => void local.reloadThreads().catch(() => undefined)}
                    />
                ) : (
                    <div className="canvas-agent-local-runtime-content">
                        <LocalAgentConversation selectedThread={local.selectedThread} events={local.events} bridgeState={local.bridgeState.kind} runtimeStatus={local.view?.state.status} muted={theme.node.muted} onSuggestion={setPrompt} />
                        {local.view ? <AgentRuntimeActivity runId={local.view.run.id} turns={[]} events={local.runtimeEvents} view={local.view} muted={theme.node.muted} /> : null}
                    </div>
                )}
                {configurationError || local.error ? <LocalAgentError message={configurationError || local.error} /> : null}
                {local.view?.state.status === "waiting_approval" && local.view.state.pendingToolCall && local.view.pendingApproval ? (
                    <AgentApprovalCard call={local.view.state.pendingToolCall} approval={local.view.pendingApproval} busy={local.busy} muted={theme.node.muted} onDecision={local.decideApproval} />
                ) : null}
            </section>
            <AgentChatComposer
                prompt={prompt}
                attachments={draft.attachments}
                sending={local.busy}
                running={active}
                disabled={historyOpen}
                placeholder={active ? "本机 Codex 正在处理" : "描述你想让本机 Codex 如何操作画布"}
                theme={theme}
                onPromptChange={setPrompt}
                onSubmit={submit}
                onStop={() => void local.interrupt()}
                onAddFiles={addAttachments}
                onRemoveAttachment={(id) => setDraft((current) => ({ ...current, attachments: current.attachments.filter((attachment) => attachment.id !== id) }))}
                onDeleteBackwardAtStart={() => {
                    const next = removeLastCanvasAgentDraftSelection(draft);
                    if (!next) return false;
                    setDraft(next);
                    return true;
                }}
                submitReady={Boolean(prompt.trim())}
                selectionSummary={active ? undefined : <CanvasAgentSelectionSummary config={effectiveConfig} models={agentModels} selectedSkills={selectedSkills} disabled={local.busy} onModelsChange={setAgentModels} onSkillsChange={setSelectedSkills} />}
                left={
                    <CanvasAgentComposerControls
                        config={effectiveConfig}
                        disabled={active || local.busy}
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

function LocalAgentHistory({ threads, loading, selectedThreadId, onSelect, onRetry }: { threads: LocalAgentThread[]; loading: boolean; selectedThreadId?: string; onSelect: (threadId: string) => void; onRetry: () => void }) {
    return (
        <div className="canvas-agent-local-history">
            <div className="canvas-agent-local-history-heading">
                <strong className="canvas-agent-local-history-title">本机对话</strong>
                <Button className="canvas-agent-local-history-retry" type="text" size="small" loading={loading} onClick={onRetry}>
                    刷新
                </Button>
            </div>
            {threads.length ? (
                threads.map((thread) => (
                    <button key={thread.threadId} type="button" className="canvas-agent-local-history-item" aria-current={selectedThreadId === thread.threadId} onClick={() => onSelect(thread.threadId)}>
                        <span className="canvas-agent-local-history-item-message">{thread.turns.at(-1)?.message ?? "空对话"}</span>
                        <span className="canvas-agent-local-history-item-meta">
                            {thread.model} · {thread.turns.length} 轮
                        </span>
                    </button>
                ))
            ) : (
                <span className="canvas-agent-local-history-empty">暂无本机对话</span>
            )}
        </div>
    );
}

function LocalAgentConversation({
    selectedThread,
    events,
    bridgeState,
    runtimeStatus,
    muted,
    onSuggestion,
}: {
    selectedThread: LocalAgentThread | null;
    events: LocalAgentEvent[];
    bridgeState: string;
    runtimeStatus: AgentRuntimeView["state"]["status"] | undefined;
    muted: string;
    onSuggestion: (value: string) => void;
}) {
    const turns = selectedThread?.turns ?? [];
    const finalMessages = visibleLocalAgentFinalMessages(events, runtimeStatus);
    if (!turns.length && !events.length) {
        return (
            <div className="canvas-agent-runtime-empty">
                <strong className="canvas-agent-runtime-empty-title">本机 Codex</strong>
                <p className="canvas-agent-runtime-empty-description" style={{ color: muted }}>
                    推理由本机完成；画布写入、媒体计费和审批仍由网站执行。
                </p>
                <div className="canvas-agent-runtime-suggestions">
                    {["读取当前画布", "整理节点布局", "检查当前工作流"].map((suggestion) => (
                        <button key={suggestion} type="button" className="canvas-agent-runtime-suggestion" onClick={() => onSuggestion(suggestion)}>
                            {suggestion}
                        </button>
                    ))}
                </div>
            </div>
        );
    }
    return (
        <div className="canvas-agent-local-conversation">
            {turns.map((turn) => (
                <article key={turn.turnId} className="canvas-agent-runtime-user-message" aria-label="本机对话消息">
                    <span className="canvas-agent-runtime-message-label" style={{ color: muted }}>
                        你
                    </span>
                    <p className="canvas-agent-runtime-message-copy">{turn.message}</p>
                </article>
            ))}
            {bridgeState !== "idle" && bridgeState !== "failed" ? <span className="canvas-agent-local-runtime-state">本机 Codex 正在处理 · {localBridgeLabel(bridgeState)}</span> : null}
            {finalMessages.map((event) => (
                <article key={`${event.threadId}:${event.turnId}`} className="canvas-agent-runtime-final" aria-label="本机 Codex 回复">
                    <p className="canvas-agent-runtime-final-copy">{event.message}</p>
                </article>
            ))}
        </div>
    );
}

function LocalAgentError({ message }: { message: string }) {
    return (
        <div className="canvas-agent-runtime-error" role="alert">
            <CircleAlert className="canvas-agent-runtime-error-icon" aria-hidden="true" />
            <span className="canvas-agent-runtime-error-copy">{message}</span>
        </div>
    );
}

function toLocalAttachment(attachment: { name: string; url: string; mimeType?: string }): LocalAgentAttachment {
    if (!attachment.mimeType?.trim()) throw new Error(`参考图片“${attachment.name}”缺少 MIME 类型`);
    const url = new URL(attachment.url, window.location.origin).toString();
    return { kind: "image", name: attachment.name, mimeType: attachment.mimeType, url };
}

function localBridgeLabel(state: string): string {
    if (state === "waiting_approval") return "等待网站审批";
    if (state === "delivering_result") return "回传权威结果";
    return "推理中";
}

function publicError(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
