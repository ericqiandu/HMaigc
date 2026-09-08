import type { MouseEventHandler, PropsWithChildren } from "react";

import type { CanvasTheme } from "@/lib/canvas-theme";

export type CanvasNodeVisualState = "idle" | "related" | "selected" | "target" | "loading" | "error";

export type CanvasNodeSurfaceProps = PropsWithChildren<{
    state: CanvasNodeVisualState;
    theme: CanvasTheme;
    flushContent?: boolean;
    className?: string;
    onMouseDown?: MouseEventHandler<HTMLDivElement>;
    onDoubleClick?: MouseEventHandler<HTMLDivElement>;
}>;

function outlineColor(state: CanvasNodeVisualState, theme: CanvasTheme) {
    if (state === "error") return theme.accent.danger;
    if (state === "idle") return theme.node.stroke;
    return theme.accent.primary;
}

function outlineWidth(state: CanvasNodeVisualState) {
    return state === "idle" || state === "related" ? 1 : 2;
}

export function CanvasNodeSurface({ state, theme, flushContent = false, className = "", onMouseDown, onDoubleClick, children }: CanvasNodeSurfaceProps) {
    const color = outlineColor(state, theme);
    const width = outlineWidth(state);

    return (
        <div
            data-canvas-node-surface="true"
            data-state={state}
            className={`canvas-node-surface relative size-full overflow-clip rounded-[12px] ${className}`}
            style={{ background: `var(--canvas-node-surface-fill, ${flushContent ? "transparent" : theme.node.fill})` }}
            onMouseDown={onMouseDown}
            onDoubleClick={onDoubleClick}
        >
            <div className="canvas-node-surface-content relative size-full">{children}</div>
            <div aria-hidden="true" className="canvas-node-outline pointer-events-none absolute inset-0 z-50 rounded-[inherit]" style={{ boxShadow: `var(--canvas-node-outline-shadow, inset 0 0 0 ${width}px ${color})` }} />
        </div>
    );
}
