import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "antd";
import { CheckCircle2, CircleAlert, Plus, UserRound, Wrench, X, XCircle } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { canvasThemes } from "@/lib/canvas-theme";
import type { CanvasAgentOperationImpact } from "@/lib/canvas/canvas-agent-ops";
import { staticAssetURL } from "@/lib/static-assets";
import type { LocalUser } from "@/stores/use-user-store";
import { CanvasAgentTooltip } from "./canvas-agent-tooltip";
import { CanvasSubmitButton } from "./canvas-submit-button";
import "./canvas-agent-panel.css";

export type CanvasAgentChatAttachment = { id: string; name: string; url: string };
export type CanvasAgentChatMessage = {
    id: string;
    role: "user" | "assistant" | "system" | "tool" | "error";
    title?: string;
    text: string;
    meta?: string;
    detail?: unknown;
    attachments?: CanvasAgentChatAttachment[];
};

const WORKING_TEXT = "正在推演...";

export function AgentChatMessage({
    item,
    theme,
    user,
    onRejectTool,
    onApproveTool,
}: {
    item: CanvasAgentChatMessage;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    user: LocalUser | null;
    onRejectTool?: (id: string) => void;
    onApproveTool?: (id: string) => void;
}) {
    const isUser = item.role === "user";
    const isSystem = item.role === "system";
    const color = item.role === "error" ? "#dc2626" : item.role === "tool" ? "#2563eb" : theme.node.text;
    if (isSystem) {
        return (
            <div className="canvas-agent-system-message flex justify-center text-xs">
                <div className="canvas-agent-system-message-content max-w-[88%] px-3 py-1.5 text-center" style={{ color: theme.node.muted }}>
                    {item.text}
                    {item.meta ? <span className="canvas-agent-system-message-meta ml-2 opacity-60">{item.meta}</span> : null}
                </div>
            </div>
        );
    }
    if (item.role === "tool") {
        if (objectField(item.detail, "status") === "pending") return <AgentPendingToolCard summary={item.text} detail={item.detail} theme={theme} onReject={() => onRejectTool?.(item.id)} onApprove={() => onApproveTool?.(item.id)} />;
        return (
            <div className="canvas-agent-message-row flex items-start">
                <AgentAvatar theme={theme} />
                <AgentToolCard title={item.title || "工具调用"} text={item.text} detail={item.detail} theme={theme} />
            </div>
        );
    }
    return (
        <div className={`canvas-agent-message-row flex items-start ${isUser ? "justify-end" : "justify-start"}`}>
            {!isUser ? <AgentAvatar theme={theme} /> : null}
            <div className={`min-w-0 ${isUser ? "canvas-agent-user-message text-right" : "canvas-agent-assistant-message text-left"}`} style={{ color }}>
                {isUser ? <div className="canvas-agent-message-text whitespace-pre-wrap break-words text-left">{item.text}</div> : <AgentMarkdownContent text={item.text} />}
                {item.attachments?.length ? <AgentMessageAttachments attachments={item.attachments} /> : null}
                {item.meta ? <div className="canvas-agent-message-meta mt-1 text-[11px] opacity-45">{item.meta}</div> : null}
            </div>
            {isUser ? <AgentUserAvatar user={user} theme={theme} /> : null}
        </div>
    );
}

export function AgentPendingToolCard({ summary, detail, theme, onReject, onApprove }: { summary: string; detail?: unknown; theme: (typeof canvasThemes)[keyof typeof canvasThemes]; onReject?: () => void; onApprove?: () => void }) {
    const impact = agentImpactFromDetail(detail);
    const cinematicProposal = objectField(detail, "kind") === "cinematic-proposal";
    return (
        <div className="canvas-agent-message-row flex items-start">
            <AgentAvatar theme={theme} />
            <div className="canvas-agent-proposal-card min-w-0 flex-1" style={{ color: theme.node.text }}>
                <div className="canvas-agent-proposal-heading flex items-start gap-3">
                    <span className="canvas-agent-tool-icon mt-0.5 grid shrink-0 place-items-center" style={{ color: "#d97706", background: "rgba(217,119,6,.08)" }}>
                        <CircleAlert className="size-4" />
                    </span>
                    <div className="canvas-agent-proposal-copy min-w-0 flex-1">
                        <div className="canvas-agent-proposal-title-row flex flex-wrap items-center gap-2 text-sm font-semibold leading-5">
                            <span className="agent-pending-tool-card-title">{cinematicProposal ? "确认影视方案写回" : "确认工具调用"}</span>
                            <span className="canvas-agent-status-badge" style={{ color: "#d97706", background: "rgba(217,119,6,.08)" }}>
                                等待确认
                            </span>
                        </div>
                        <div className="canvas-agent-proposal-summary mt-2 text-sm leading-6" style={{ color: theme.node.text }}>
                            {summary}
                        </div>
                    </div>
                </div>
                {impact?.operationCount ? (
                    <div className="canvas-agent-proposal-impact mt-3 border-t pt-3" style={{ borderColor: theme.node.stroke }}>
                        <div className="canvas-agent-impact-grid">
                            <ImpactMetric label="操作" value={impact.operationCount} theme={theme} />
                            <ImpactMetric label="涉及节点" value={impact.affectedNodeCount} theme={theme} />
                            <ImpactMetric label="删除" value={impact.destructiveCount} attention={impact.destructiveCount > 0} theme={theme} />
                            <ImpactMetric label="生成" value={impact.generationCount} attention={impact.generationCount > 0} theme={theme} />
                        </div>
                        {impact.items.length ? (
                            <div className="canvas-agent-impact-items mt-3 space-y-1.5">
                                {impact.items.map((item, index) => (
                                    <div key={`${item}-${index}`} className="canvas-agent-impact-item flex gap-2 text-xs leading-5" style={{ color: theme.node.muted }}>
                                        <span className="canvas-agent-impact-bullet mt-2 size-1 shrink-0 rounded-full bg-current" />
                                        <span className="canvas-agent-impact-copy">{item}</span>
                                    </div>
                                ))}
                            </div>
                        ) : null}
                        {impact.warning ? <div className="canvas-agent-impact-warning mt-3 border-l-2 border-amber-500/70 bg-amber-500/[.05] px-2.5 py-2 text-xs leading-5 text-amber-700 dark:text-amber-300">{impact.warning}</div> : null}
                    </div>
                ) : null}
                {detail ? (
                    <details className="canvas-agent-proposal-detail mt-3 border-t pt-2" style={{ borderColor: theme.node.stroke }}>
                        <summary className="canvas-agent-proposal-detail-summary cursor-pointer text-xs" style={{ color: theme.node.muted }}>
                            技术详情
                        </summary>
                        <AgentDetailBlock detail={detail} theme={theme} />
                    </details>
                ) : null}
                {onReject || onApprove ? (
                    <div className="canvas-agent-proposal-actions">
                        <Button className="canvas-agent-proposal-secondary" icon={<XCircle className="size-3.5" />} onClick={() => onReject?.()}>
                            {cinematicProposal ? "暂不写入" : "拒绝执行"}
                        </Button>
                        <Button type="primary" className="canvas-agent-proposal-primary" icon={<CheckCircle2 className="size-3.5" />} onClick={() => onApprove?.()}>
                            {cinematicProposal ? "确认写入并执行" : "批准执行"}
                        </Button>
                    </div>
                ) : null}
            </div>
        </div>
    );
}

function ImpactMetric({ label, value, attention = false, theme }: { label: string; value: number; attention?: boolean; theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    return (
        <div className="canvas-agent-impact-metric">
            <div className="canvas-agent-impact-label text-[10px]" style={{ color: theme.node.muted }}>
                {label}
            </div>
            <div className="canvas-agent-impact-value mt-0.5 text-sm font-semibold tabular-nums" style={{ color: attention ? "#d97706" : theme.node.text }}>
                {value}
            </div>
        </div>
    );
}

function agentImpactFromDetail(detail: unknown) {
    const impact = objectField(detail, "impact");
    if (!impact || typeof impact !== "object") return null;
    const value = impact as Partial<CanvasAgentOperationImpact>;
    return {
        operationCount: Number(value.operationCount) || 0,
        affectedNodeCount: Number(value.affectedNodeCount) || 0,
        destructiveCount: Number(value.destructiveCount) || 0,
        generationCount: Number(value.generationCount) || 0,
        items: Array.isArray(value.items) ? value.items.filter((item): item is string => typeof item === "string") : [],
        warning: typeof value.warning === "string" ? value.warning : "",
    } satisfies CanvasAgentOperationImpact;
}

export function AgentToolCard({ title, text, detail, theme }: { title: string; text: string; detail?: unknown; theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    const state = toolCardState(title, text, detail);
    return (
        <details className="canvas-agent-tool-card min-w-0 flex-1 text-left" style={{ color: theme.node.text }}>
            <summary className="canvas-agent-tool-summary cursor-pointer list-none">
                <div className="canvas-agent-tool-heading flex items-start gap-3">
                    <span className="canvas-agent-tool-icon mt-0.5 grid shrink-0 place-items-center" style={{ color: state.color, background: state.softBg }}>
                        {state.icon}
                    </span>
                    <div className="canvas-agent-tool-copy min-w-0 flex-1">
                        <div className="canvas-agent-tool-title-row flex flex-wrap items-center gap-2 text-sm font-semibold leading-5">
                            <span className="canvas-agent-tool-title min-w-0 truncate">{title}</span>
                            <span className="canvas-agent-status-badge" style={{ color: state.color, background: state.softBg }}>
                                {state.label}
                            </span>
                            {detail ? (
                                <span className="canvas-agent-tool-detail-label ml-auto text-xs font-normal" style={{ color: theme.node.muted }}>
                                    详情
                                </span>
                            ) : null}
                        </div>
                        <div className="canvas-agent-tool-text mt-2 text-sm leading-6" style={{ color: state.isError ? state.color : theme.node.muted }}>
                            {text}
                        </div>
                    </div>
                </div>
            </summary>
            {detail ? <AgentDetailBlock detail={detail} theme={theme} /> : null}
        </details>
    );
}

export function AgentWorkingMessage({ theme }: { theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    const [length, setLength] = useState(1);
    useEffect(() => {
        const timer = window.setInterval(() => setLength((value) => (value >= WORKING_TEXT.length + 4 ? 1 : value + 1)), 120);
        return () => window.clearInterval(timer);
    }, [setLength]);
    return (
        <div className="canvas-agent-working-message flex items-start gap-2.5">
            <AgentAvatar theme={theme} />
            <div className="canvas-agent-working-copy min-w-0 max-w-[82%]">
                <div className="canvas-agent-working-text font-mono text-sm" style={{ color: theme.node.muted }} aria-label={WORKING_TEXT}>
                    <span className="canvas-agent-working-text-inner inline-block w-[96px]">{WORKING_TEXT.slice(0, Math.min(length, WORKING_TEXT.length))}</span>
                </div>
            </div>
        </div>
    );
}

export function AgentChatComposer({
    prompt,
    attachments = [],
    disabled,
    sending,
    placeholder,
    theme,
    onPromptChange,
    onSubmit,
    onAddFiles,
    onRemoveAttachment,
    onDeleteBackwardAtStart,
    selectionSummary,
    left,
    submitReady,
}: {
    prompt: string;
    attachments?: CanvasAgentChatAttachment[];
    disabled?: boolean;
    sending?: boolean;
    placeholder: string;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    onPromptChange: (value: string) => void;
    onSubmit: () => void;
    onAddFiles?: (files: FileList | File[] | null) => void | Promise<void>;
    onRemoveAttachment?: (id: string) => void;
    onDeleteBackwardAtStart?: () => boolean;
    selectionSummary?: ReactNode;
    left?: ReactNode;
    submitReady?: boolean;
}) {
    const fileInputRef = useRef<HTMLInputElement>(null);
    const canSubmit = !disabled && !sending && (submitReady ?? Boolean(prompt.trim() || attachments.length));
    return (
        <div className="canvas-agent-composer-wrap" onWheelCapture={(event) => event.stopPropagation()}>
            <div className="canvas-agent-composer border" style={{ background: theme.node.fill, borderColor: theme.node.stroke }}>
                {attachments.length ? (
                    <div className="canvas-agent-composer-attachments thin-scrollbar mb-2 flex gap-2 overflow-x-auto pb-1">
                        {attachments.map((item) => (
                            <div key={item.id} className="canvas-agent-composer-attachment group relative size-14 shrink-0 overflow-hidden rounded-md" title={item.name}>
                                <img src={item.url} alt={item.name} className="canvas-agent-composer-attachment-image size-full object-cover" />
                                {onRemoveAttachment ? (
                                    <button
                                        type="button"
                                        className="canvas-agent-composer-attachment-remove absolute right-1 top-1 grid size-5 place-items-center rounded-md opacity-0 transition group-hover:opacity-100"
                                        style={{ background: theme.toolbar.panel, color: theme.node.text }}
                                        onClick={() => onRemoveAttachment(item.id)}
                                        aria-label="移除图片"
                                    >
                                        <X className="size-3" />
                                    </button>
                                ) : null}
                            </div>
                        ))}
                    </div>
                ) : null}
                <div className="canvas-agent-composer-input-flow">
                    {selectionSummary}
                    <textarea
                        value={prompt}
                        aria-label="输入 Agent 指令"
                        onInput={(event) => onPromptChange(event.currentTarget.value)}
                        onPaste={(event) => {
                            if (!onAddFiles) return;
                            const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith("image/"));
                            if (!images.length) return;
                            event.preventDefault();
                            void onAddFiles(images);
                        }}
                        onKeyDown={(event) => {
                            if (event.key === "Backspace" && !prompt.length && onDeleteBackwardAtStart?.()) {
                                event.preventDefault();
                                return;
                            }
                            if (event.key !== "Enter" || event.shiftKey || event.ctrlKey || event.metaKey) return;
                            event.preventDefault();
                            void onSubmit();
                        }}
                        className="canvas-agent-composer-textarea thin-scrollbar resize-none border-0 bg-transparent outline-none placeholder:opacity-45"
                        style={{ color: theme.node.text }}
                        placeholder={placeholder}
                    />
                </div>
                <div className="canvas-agent-composer-footer">
                    <div className="canvas-agent-composer-left flex min-w-0 items-center gap-1">
                        {onAddFiles ? (
                            <>
                                <input
                                    ref={fileInputRef}
                                    hidden
                                    type="file"
                                    accept="image/*"
                                    multiple
                                    onChange={(event) => {
                                        void onAddFiles(event.target.files);
                                        event.target.value = "";
                                    }}
                                />
                                <CanvasAgentTooltip title="添加图片">
                                    <Button
                                        type="text"
                                        className="canvas-agent-composer-tool"
                                        disabled={disabled || sending}
                                        style={{ color: theme.node.muted }}
                                        icon={<Plus className="canvas-agent-composer-glyph" strokeWidth={1.8} />}
                                        onClick={() => fileInputRef.current?.click()}
                                        aria-label="添加图片"
                                    />
                                </CanvasAgentTooltip>
                            </>
                        ) : null}
                        {left}
                    </div>
                    <CanvasSubmitButton state={sending ? "loading" : "ready"} disabled={!canSubmit} onClick={() => void onSubmit()} ariaLabel={sending ? "正在发送" : "发送"} />
                </div>
            </div>
        </div>
    );
}

function AgentMarkdownContent({ text }: { text: string }) {
    return (
        <div className="canvas-agent-markdown">
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                    a: ({ href, children }) => (
                        <a className="canvas-agent-markdown-link" href={href} target="_blank" rel="noreferrer">
                            {children}
                        </a>
                    ),
                    blockquote: ({ children }) => <blockquote className="canvas-agent-markdown-quote">{children}</blockquote>,
                    code: ({ children, className }) => <code className={`canvas-agent-markdown-code ${className || ""}`}>{children}</code>,
                    h1: ({ children }) => <h1 className="canvas-agent-markdown-heading">{children}</h1>,
                    h2: ({ children }) => <h2 className="canvas-agent-markdown-heading">{children}</h2>,
                    h3: ({ children }) => <h3 className="canvas-agent-markdown-heading">{children}</h3>,
                    li: ({ children }) => <li className="canvas-agent-markdown-list-item">{children}</li>,
                    ol: ({ children }) => <ol className="canvas-agent-markdown-list canvas-agent-markdown-list--ordered">{children}</ol>,
                    p: ({ children }) => <p className="canvas-agent-markdown-paragraph">{children}</p>,
                    pre: ({ children }) => <pre className="canvas-agent-markdown-pre">{children}</pre>,
                    strong: ({ children }) => <strong className="canvas-agent-markdown-strong">{children}</strong>,
                    table: ({ children }) => (
                        <div className="canvas-agent-markdown-table-wrap">
                            <table className="canvas-agent-markdown-table">{children}</table>
                        </div>
                    ),
                    tbody: ({ children }) => <tbody className="canvas-agent-markdown-table-body">{children}</tbody>,
                    td: ({ children }) => <td className="canvas-agent-markdown-table-cell">{children}</td>,
                    th: ({ children }) => <th className="canvas-agent-markdown-table-head">{children}</th>,
                    thead: ({ children }) => <thead className="canvas-agent-markdown-table-header">{children}</thead>,
                    tr: ({ children }) => <tr className="canvas-agent-markdown-table-row">{children}</tr>,
                    ul: ({ children }) => <ul className="canvas-agent-markdown-list">{children}</ul>,
                }}
            >
                {text}
            </ReactMarkdown>
        </div>
    );
}

function AgentDetailBlock({ detail, theme }: { detail: unknown; theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    return (
        <pre className="canvas-agent-detail thin-scrollbar" style={{ color: theme.node.muted }}>
            {JSON.stringify(detail, null, 2)}
        </pre>
    );
}

function AgentAvatar({ theme }: { theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    return (
        <span className="canvas-agent-avatar grid shrink-0 place-items-center" role="img" aria-label="OpenAI">
            <span
                className="canvas-agent-avatar-mark size-5 opacity-80"
                style={{ background: theme.node.text, WebkitMask: `url(${staticAssetURL("/icons/openai.svg")}) center / contain no-repeat`, mask: `url(${staticAssetURL("/icons/openai.svg")}) center / contain no-repeat` }}
            />
        </span>
    );
}

function AgentUserAvatar({ user, theme }: { user: LocalUser | null; theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    const avatarUrl = user?.avatarUrl?.trim();
    return (
        <span className="canvas-agent-avatar grid shrink-0 place-items-center overflow-hidden rounded-full" style={{ color: theme.node.text }}>
            {avatarUrl ? <img src={avatarUrl} alt="" className="canvas-agent-user-avatar-image size-full object-cover" referrerPolicy="no-referrer" /> : <UserRound className="size-4" />}
        </span>
    );
}

function AgentMessageAttachments({ attachments }: { attachments: CanvasAgentChatAttachment[] }) {
    return (
        <div className="canvas-agent-message-attachments mt-2 grid grid-cols-3 gap-1.5">
            {attachments.map((item) => (
                <img key={item.id} src={item.url} alt={item.name} className="canvas-agent-message-attachment aspect-square w-full rounded-md object-cover" />
            ))}
        </div>
    );
}

function toolCardState(title: string, text: string, detail?: unknown) {
    const raw = `${title} ${text} ${normalizeText(objectField(detail, "error"))}`;
    const lower = raw.toLowerCase();
    const tool = String(objectField(detail, "name") || objectField(detail, "tool") || "");
    if (objectField(detail, "status") === "noop" || /未生效|无需|没有找到|没有.*可|已存在/.test(raw))
        return { label: "未生效", color: "#d97706", softBorder: "rgba(217,119,6,.22)", softBg: "rgba(217,119,6,.04)", icon: <CircleAlert className="size-4" />, isError: false };
    if (/拒绝|取消/.test(raw) || lower.includes("rejected")) return { label: "拒绝执行", color: "#dc2626", softBorder: "rgba(220,38,38,.20)", softBg: "rgba(220,38,38,.04)", icon: <XCircle className="size-4" />, isError: true };
    if (/失败|错误/.test(raw) || lower.includes("failed") || lower.includes("error")) return { label: "执行失败", color: "#dc2626", softBorder: "rgba(220,38,38,.20)", softBg: "rgba(220,38,38,.04)", icon: <XCircle className="size-4" />, isError: true };
    if (/完成|成功/.test(raw) || lower.includes("completed") || lower.includes("succeeded"))
        return { label: tool === "canvas_apply_ops" || /画布操作/.test(title) ? "已批准执行" : "执行完成", color: "#16a34a", softBorder: "rgba(22,163,74,.20)", softBg: "rgba(22,163,74,.04)", icon: <CheckCircle2 className="size-4" />, isError: false };
    return { label: "工具调用", color: "#2563eb", softBorder: "rgba(37,99,235,.20)", softBg: "rgba(37,99,235,.04)", icon: <Wrench className="size-4" />, isError: false };
}

function normalizeText(value: unknown) {
    if (typeof value === "string") return value.trim();
    if (value instanceof Error) return value.message;
    if (value == null) return "";
    return JSON.stringify(value, null, 2);
}

function objectField(value: unknown, key: string) {
    return value && typeof value === "object" ? (value as Record<string, unknown>)[key] : undefined;
}
