import type { ReactNode } from "react";
import type { CanvasTheme } from "@/lib/canvas-theme";

export type CanvasNodeStatusTone = "neutral" | "progress" | "danger";

type CanvasNodeActionProps = {
    icon: ReactNode;
    label: string;
    tone?: "neutral" | "danger";
    onClick: () => void;
};

type CanvasNodeEmptyStateProps = {
    icon: ReactNode;
    title: string;
    description?: string;
    theme: CanvasTheme;
};

type CanvasNodeStatusLayoutProps = {
    icon: ReactNode;
    title: string;
    detail?: string;
    progress?: number;
    meta?: ReactNode;
    actions?: ReactNode;
    tone: CanvasNodeStatusTone;
    theme: CanvasTheme;
};

export function CanvasNodeAction({ icon, label, tone = "neutral", onClick }: CanvasNodeActionProps) {
    return (
        <button
            type="button"
            className={`canvas-node-action canvas-node-action-${tone}`}
            aria-label={label}
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={(event) => {
                event.stopPropagation();
                onClick();
            }}
        >
            <span className="canvas-node-action-icon" aria-hidden="true">
                {icon}
            </span>
            <span className="canvas-node-action-label">{label}</span>
        </button>
    );
}

export function CanvasNodeEmptyState({ icon, title, description, theme }: CanvasNodeEmptyStateProps) {
    return (
        <div className="canvas-node-state canvas-node-state-empty" style={{ color: theme.node.placeholder }}>
            <span className="canvas-node-state-icon" style={{ background: theme.toolbar.itemHover }} aria-hidden="true">
                {icon}
            </span>
            <span className="canvas-node-state-title" style={{ color: theme.node.muted }}>
                {title}
            </span>
            {description ? (
                <span className="canvas-node-state-description" style={{ color: theme.node.faint }}>
                    {description}
                </span>
            ) : null}
        </div>
    );
}

export function CanvasNodeStatusLayout({ icon, title, detail, progress, meta, actions, tone, theme }: CanvasNodeStatusLayoutProps) {
    const safeProgress = typeof progress === "number" ? Math.max(0, Math.min(100, Math.round(progress))) : undefined;
    const toneColor = tone === "danger" ? theme.accent.danger : tone === "progress" ? theme.accent.primary : theme.node.muted;
    return (
        <div className={`canvas-node-state canvas-node-state-${tone}`} role="status" style={{ color: toneColor }}>
            <span className="canvas-node-state-icon" style={{ background: tone === "progress" ? theme.accent.primarySoft : theme.toolbar.itemHover }} aria-hidden="true">
                {icon}
            </span>
            <span className="canvas-node-state-title">{title}</span>
            {detail ? (
                <span className="canvas-node-state-detail" style={{ color: theme.node.text }}>
                    {detail}
                </span>
            ) : null}
            {safeProgress !== undefined ? (
                <span className="canvas-node-state-progress" role="progressbar" aria-label="任务进度" aria-valuemin={0} aria-valuemax={100} aria-valuenow={safeProgress} style={{ background: theme.node.stroke }}>
                    <span className="canvas-node-state-progress-value" style={{ width: `${safeProgress}%`, background: theme.accent.primary }} />
                </span>
            ) : null}
            {meta ? (
                <span className="canvas-node-state-meta" style={{ color: theme.node.muted }}>
                    {meta}
                </span>
            ) : null}
            {actions ? <span className="canvas-node-state-actions">{actions}</span> : null}
        </div>
    );
}
