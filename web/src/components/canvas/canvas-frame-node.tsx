import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { ChevronDown, ChevronRight, Video } from "lucide-react";

import { FRAME_HEADER_HEIGHT, FRAME_PADDING } from "@/lib/canvas/canvas-frame";
import { applyCanvasLiveResize, calculateCanvasResizePreview, clearCanvasLiveResize, type CanvasResizeCorner, type CanvasResizePreview } from "@/lib/canvas/canvas-interaction-runtime";
import { canvasThemes, type CanvasTheme } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasNodeType, type CanvasNodeData, type Position } from "@/types/canvas";
import { CanvasNodeSurface, type CanvasNodeVisualState } from "./canvas-node-surface";

type CanvasFrameResizeGesture = {
    active: boolean;
    corner: CanvasResizeCorner;
    startX: number;
    startY: number;
    scale: number;
    nodeId: string;
    initial: CanvasResizePreview;
    childBounds: { left: number; top: number; right: number; bottom: number } | null;
    lastPreview: CanvasResizePreview | null;
    cleanup: (() => void) | null;
};

export const CanvasFrameNode = React.memo(function CanvasFrameNode({
    data,
    dragOffset,
    childNodes,
    scale,
    isSelected,
    isDropTarget,
    onMouseDown,
    onResizeCommit,
    onToggleCollapsed,
    onTitleChange,
    onContextMenu,
    readOnly = false,
    onHoverStart,
    onHoverEnd,
}: {
    data: CanvasNodeData;
    dragOffset?: Position;
    childNodes: CanvasNodeData[];
    scale: number;
    isSelected: boolean;
    isDropTarget: boolean;
    onMouseDown: (event: ReactMouseEvent, nodeId: string) => void;
    onResizeCommit: (nodeId: string, width: number, height: number, position?: Position) => void;
    onToggleCollapsed: (nodeId: string) => void;
    onTitleChange: (nodeId: string, title: string) => void;
    onContextMenu: (event: ReactMouseEvent, nodeId: string) => void;
    readOnly?: boolean;
    onHoverStart?: (nodeId: string) => void;
    onHoverEnd?: (nodeId: string) => void;
}) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const collapsed = Boolean(data.metadata?.frame?.collapsed);
    const [editing, setEditing] = useState(false);
    const [title, setTitle] = useState(data.title);
    const frameElementRef = useRef<HTMLDivElement>(null);
    const resizeRef = useRef<CanvasFrameResizeGesture>({
        active: false,
        corner: "bottom-right",
        startX: 0,
        startY: 0,
        scale: 1,
        nodeId: data.id,
        initial: { x: data.position.x, y: data.position.y, width: data.width, height: data.height },
        childBounds: null as { left: number; top: number; right: number; bottom: number } | null,
        lastPreview: null,
        cleanup: null,
    });

    useEffect(() => setTitle(data.title), [data.title]);

    const commitTitle = () => {
        const next = title.trim() || "未命名背板";
        setTitle(next);
        setEditing(false);
        onTitleChange(data.id, next);
    };

    const childBounds = useMemo(() => {
        if (!childNodes.length) return null;
        const left = Math.min(...childNodes.map((node) => node.position.x));
        const top = Math.min(...childNodes.map((node) => node.position.y));
        const right = Math.max(...childNodes.map((node) => node.position.x + node.width));
        const bottom = Math.max(...childNodes.map((node) => node.position.y + node.height));
        return { left, top, right, bottom };
    }, [childNodes]);

    const handleResizeMove = useCallback((event: MouseEvent) => {
        const state = resizeRef.current;
        if (!state.active) return;
        const horizontalMovement = (event.clientX - state.startX) / state.scale;
        const verticalMovement = (event.clientY - state.startY) / state.scale;
        const fromLeft = state.corner.includes("left");
        const fromTop = state.corner.includes("top");
        const preview = calculateCanvasResizePreview({
            initial: state.initial,
            delta: {
                x: fromLeft ? -horizontalMovement : horizontalMovement,
                y: fromTop ? -verticalMovement : verticalMovement,
            },
            corner: state.corner,
            minWidth: 360,
            minHeight: 240,
        });
        let left = preview.x;
        let top = preview.y;
        let right = preview.x + preview.width;
        let bottom = preview.y + preview.height;

        if (state.childBounds) {
            if (fromLeft) left = Math.min(left, state.childBounds.left - FRAME_PADDING);
            else right = Math.max(right, state.childBounds.right + FRAME_PADDING);
            if (fromTop) top = Math.min(top, state.childBounds.top - FRAME_HEADER_HEIGHT - FRAME_PADDING);
            else bottom = Math.max(bottom, state.childBounds.bottom + FRAME_PADDING);
        }
        state.lastPreview = { x: left, y: top, width: right - left, height: bottom - top };
        if (frameElementRef.current) applyCanvasLiveResize(frameElementRef.current, state.lastPreview);
    }, []);

    const handleResizeUp = useCallback(() => {
        const state = resizeRef.current;
        if (!state.active) return;
        const preview = state.lastPreview;
        state.active = false;
        state.lastPreview = null;
        state.cleanup?.();
        state.cleanup = null;
        if (frameElementRef.current) clearCanvasLiveResize(frameElementRef.current);
        if (preview) onResizeCommit(state.nodeId, preview.width, preview.height, { x: preview.x, y: preview.y });
    }, [onResizeCommit]);

    const handleResizeCancel = useCallback(() => {
        const state = resizeRef.current;
        if (!state.active) return;
        state.active = false;
        state.lastPreview = null;
        state.cleanup?.();
        state.cleanup = null;
        if (frameElementRef.current) clearCanvasLiveResize(frameElementRef.current);
    }, []);

    useEffect(() => () => handleResizeCancel(), [handleResizeCancel]);

    const startResize = (event: ReactMouseEvent, corner: CanvasResizeCorner) => {
        event.preventDefault();
        event.stopPropagation();
        resizeRef.current = {
            active: true,
            corner,
            startX: event.clientX,
            startY: event.clientY,
            scale,
            nodeId: data.id,
            initial: { x: data.position.x, y: data.position.y, width: data.width, height: data.height },
            childBounds,
            lastPreview: null,
            cleanup: null,
        };
        window.addEventListener("mousemove", handleResizeMove);
        window.addEventListener("mouseup", handleResizeUp);
        window.addEventListener("blur", handleResizeCancel);
        window.addEventListener("pointercancel", handleResizeCancel);
        resizeRef.current.cleanup = () => {
            window.removeEventListener("mousemove", handleResizeMove);
            window.removeEventListener("mouseup", handleResizeUp);
            window.removeEventListener("blur", handleResizeCancel);
            window.removeEventListener("pointercancel", handleResizeCancel);
        };
    };

    const active = isSelected || isDropTarget;
    const visualState: CanvasNodeVisualState = isDropTarget ? "target" : isSelected ? "selected" : "idle";

    return (
        <div
            ref={frameElementRef}
            data-node-id={data.id}
            className={`absolute z-0 select-none ${dragOffset ? "cursor-grabbing" : "cursor-default"}`}
            style={
                {
                    transform: `translate(calc(var(--canvas-live-resize-x, ${data.position.x}px) + var(--canvas-live-drag-x, 0px)), calc(var(--canvas-live-resize-y, ${data.position.y}px) + var(--canvas-live-drag-y, 0px)))`,
                    width: `var(--canvas-live-resize-width, ${data.width}px)`,
                    height: `var(--canvas-live-resize-height, ${data.height}px)`,
                    contain: "layout style",
                    "--canvas-node-surface-fill": active ? theme.frame.activeFill : theme.frame.fill,
                    "--canvas-node-outline-shadow": `inset 0 0 0 ${active ? 2 : 1}px ${active ? theme.frame.activeStroke : theme.frame.stroke}`,
                } as React.CSSProperties
            }
            onMouseDown={(event) => onMouseDown(event, data.id)}
            onDoubleClick={(event) => {
                if (!collapsed || (event.target instanceof Element && event.target.closest("button,input"))) return;
                event.stopPropagation();
                onToggleCollapsed(data.id);
            }}
            onContextMenu={(event) => onContextMenu(event, data.id)}
            onMouseEnter={() => onHoverStart?.(data.id)}
            onMouseLeave={() => onHoverEnd?.(data.id)}
        >
            <CanvasNodeSurface className="canvas-frame-shell" state={visualState} theme={theme}>
                <div className="pointer-events-auto absolute inset-x-0 top-0 z-10 flex items-center gap-1.5 px-1.5" style={{ height: FRAME_HEADER_HEIGHT, color: theme.node.text }}>
                    <button
                        type="button"
                        className="grid size-8 shrink-0 place-items-center rounded-md transition-colors hover:bg-black/5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 dark:hover:bg-white/10"
                        style={{ outlineColor: theme.frame.activeStroke }}
                        aria-label={collapsed ? "展开背板" : "折叠背板"}
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => {
                            event.stopPropagation();
                            onToggleCollapsed(data.id);
                        }}
                    >
                        {collapsed ? <ChevronRight className="size-4" /> : <ChevronDown className="size-4" />}
                    </button>
                    {editing ? (
                        <input
                            autoFocus
                            className="canvas-node-title h-7 min-w-0 flex-1 rounded border bg-transparent px-1.5 outline-none"
                            style={{ borderColor: theme.frame.activeStroke, color: theme.node.text }}
                            value={title}
                            onChange={(event) => setTitle(event.target.value)}
                            onBlur={commitTitle}
                            onMouseDown={(event) => event.stopPropagation()}
                            onKeyDown={(event) => {
                                if (event.key === "Enter") commitTitle();
                                if (event.key === "Escape") {
                                    setTitle(data.title);
                                    setEditing(false);
                                }
                            }}
                        />
                    ) : (
                        <button
                            type="button"
                            className="canvas-node-title min-w-0 truncate text-left"
                            title={data.title}
                            onDoubleClick={(event) => {
                                event.stopPropagation();
                                if (readOnly) return;
                                setEditing(true);
                            }}
                        >
                            {data.title}
                        </button>
                    )}
                    <span className="ml-auto shrink-0 pr-1 text-[11px] tabular-nums" style={{ color: theme.node.muted }}>
                        {childNodes.length}
                    </span>
                </div>

                {collapsed ? <FramePreview nodes={childNodes} frame={data} theme={theme} /> : null}
            </CanvasNodeSurface>
            {!readOnly && !collapsed && isSelected && !data.metadata?.locked ? (
                <>
                    <ResizeHandle corner="top-left" scale={scale} theme={theme} onMouseDown={startResize} />
                    <ResizeHandle corner="top-right" scale={scale} theme={theme} onMouseDown={startResize} />
                    <ResizeHandle corner="bottom-left" scale={scale} theme={theme} onMouseDown={startResize} />
                    <ResizeHandle corner="bottom-right" scale={scale} theme={theme} onMouseDown={startResize} />
                </>
            ) : null}
        </div>
    );
});

function FramePreview({ nodes, frame, theme }: { nodes: CanvasNodeData[]; frame: CanvasNodeData; theme: CanvasTheme }) {
    const layout = useMemo(() => {
        if (!nodes.length) return [];
        const previewNodes = nodes.slice(0, 24);
        const left = Math.min(...previewNodes.map((node) => node.position.x));
        const top = Math.min(...previewNodes.map((node) => node.position.y));
        const right = Math.max(...previewNodes.map((node) => node.position.x + node.width));
        const bottom = Math.max(...previewNodes.map((node) => node.position.y + node.height));
        const width = Math.max(right - left, 1);
        const height = Math.max(bottom - top, 1);
        const previewWidth = Math.max(frame.width - 16, 1);
        const previewHeight = Math.max(frame.height - FRAME_HEADER_HEIGHT - 8, 1);
        const scale = Math.min(previewWidth / width, previewHeight / height);
        const offsetX = (previewWidth - width * scale) / 2;
        const offsetY = (previewHeight - height * scale) / 2;
        return previewNodes.map((node) => ({
            node,
            left: offsetX + (node.position.x - left) * scale,
            top: offsetY + (node.position.y - top) * scale,
            width: Math.max(node.width * scale, 12),
            height: Math.max(node.height * scale, 10),
        }));
    }, [frame.height, frame.width, nodes]);

    return (
        <div className="pointer-events-none absolute inset-x-2 bottom-2 overflow-hidden rounded-md" style={{ top: FRAME_HEADER_HEIGHT, background: theme.frame.preview }}>
            {layout.length ? (
                layout.map(({ node, ...style }) => (
                    <div key={node.id} className="absolute overflow-hidden rounded-[3px] border" style={{ ...style, background: theme.node.fill, borderColor: theme.node.stroke }}>
                        {node.type === CanvasNodeType.Image && node.metadata?.content ? <img src={node.metadata.content} alt="" className="h-full w-full object-cover" loading="lazy" decoding="async" draggable={false} /> : null}
                        {node.type === CanvasNodeType.Video && node.metadata?.content ? <video src={node.metadata.content} className="h-full w-full object-cover" muted playsInline preload="metadata" /> : null}
                        {node.type === CanvasNodeType.Video && !node.metadata?.content ? <Video className="m-auto size-4 h-full opacity-40" /> : null}
                        {node.type === CanvasNodeType.Text ? (
                            <div className="line-clamp-3 p-1 text-[7px] leading-[9px]" style={{ color: theme.node.text }}>
                                {node.metadata?.content || node.title}
                            </div>
                        ) : null}
                        {node.type === CanvasNodeType.Script ? (
                            <div className="p-1 text-[7px] leading-[9px]" style={{ color: theme.node.text }}>
                                分镜脚本 · {node.metadata?.storyboard?.rows.length || 0} 镜
                            </div>
                        ) : null}
                    </div>
                ))
            ) : (
                <div className="grid h-full place-items-center text-[11px]" style={{ color: theme.node.faint }}>
                    空背板
                </div>
            )}
        </div>
    );
}

function ResizeHandle({ corner, scale, theme, onMouseDown }: { corner: CanvasResizeCorner; scale: number; theme: CanvasTheme; onMouseDown: (event: ReactMouseEvent, corner: CanvasResizeCorner) => void }) {
    const inverseScale = 1 / Math.max(scale, 0.05);
    const size = 14 * inverseScale;
    const offset = -size / 2;
    const fromTop = corner.includes("top");
    const fromLeft = corner.includes("left");
    const cursor = corner === "top-left" || corner === "bottom-right" ? "nwse-resize" : "nesw-resize";

    return (
        <button
            type="button"
            data-canvas-resize-corner={corner}
            aria-label="调整背板尺寸"
            className="canvas-frame-resize-handle pointer-events-auto absolute z-20 rounded-sm shadow-sm"
            style={{
                top: fromTop ? offset : undefined,
                bottom: fromTop ? undefined : offset,
                left: fromLeft ? offset : undefined,
                right: fromLeft ? undefined : offset,
                width: size,
                height: size,
                cursor,
                background: theme.frame.activeStroke,
                border: `${2 * inverseScale}px solid ${theme.canvas.background}`,
            }}
            onMouseDown={(event) => onMouseDown(event, corner)}
        />
    );
}
