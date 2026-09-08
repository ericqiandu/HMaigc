import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { BookOpenCheck, ChevronRight, Clock3, FileText, Image as ImageIcon, LoaderCircle, Lock, Maximize2, RefreshCw, Replace, Square, Star, TriangleAlert } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import { useSharedSecondNow } from "@/hooks/use-shared-second-clock";
import { resourceStorageLabel, resourceStorageLocation, resourceStorageTitle } from "@/lib/canvas/resource-storage-status";
import { canvasRichTextHTML } from "@/lib/canvas/canvas-rich-text";
import { applyCanvasLiveResize, calculateCanvasResizePreview, clearCanvasLiveResize, type CanvasResizeCorner, type CanvasResizePreview } from "@/lib/canvas/canvas-interaction-runtime";
import type { CanvasMediaVisibilityTier } from "@/lib/canvas/canvas-render-index";
import { formatBytes } from "@/lib/image-utils";
import { AGENT_MEDIA_SETTLEMENT_PENDING_ERROR_CODE, CONTENT_MODERATION_ERROR_CODE, isContentModerationError } from "@/lib/generation-error";
import { canvasResourceDisplayUrl } from "@/lib/canvas-media-playback";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasTextDraftEditor, type CanvasTextDraftEditorHandle } from "./canvas-text-draft-editor";
import { CanvasMediaNodeContent } from "./canvas-media-node-content";
import { CanvasNodeSurface, type CanvasNodeVisualState } from "./canvas-node-surface";
import { CanvasNodeAction, CanvasNodeEmptyState, CanvasNodeStatusLayout } from "./canvas-node-ui";
import { storyboardMinNodeHeight } from "./canvas-script-node";
import { CanvasNodeType, type CanvasMediaIntrinsicSize, type CanvasNodeData, type Position } from "@/types/canvas";
import type { CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import "./canvas-audio-node.css";

type CanvasTheme = (typeof canvasThemes)[keyof typeof canvasThemes];

type CanvasResizeGesture = {
    active: boolean;
    corner: CanvasResizeCorner;
    startX: number;
    startY: number;
    scale: number;
    nodeId: string;
    initial: CanvasResizePreview;
    minWidth: number;
    minHeight: number;
    aspectRatio?: number;
    lastPreview: CanvasResizePreview | null;
    cleanup: (() => void) | null;
};

type CanvasNodeProps = {
    data: CanvasNodeData;
    dragOffset?: Position;
    scale: number;
    isSelected: boolean;
    isRelated: boolean;
    isFocusRelated: boolean;
    isConnectionTarget: boolean;
    isConnecting: boolean;
    showImageInfo: boolean;
    mediaVisibilityTier: CanvasMediaVisibilityTier;
    reduceMediaEffects?: boolean;
    readOnly?: boolean;
    resourceLabel?: CanvasResourceReference;
    mentionReferences?: CanvasResourceReference[];
    renderNodeContent?: (node: CanvasNodeData) => ReactNode;
    batchCount?: number;
    batchExpanded?: boolean;
    batchClosing?: boolean;
    batchOpening?: boolean;
    batchRecovering?: boolean;
    batchMotion?: { x: number; y: number; index: number };
    onMouseDown: (event: React.MouseEvent, nodeId: string) => void;
    onHoverStart: (nodeId: string) => void;
    onHoverEnd: (nodeId: string) => void;
    onConnectStart: (event: React.PointerEvent, nodeId: string, handleType: "source" | "target", handleId?: string) => void;
    onResizeCommit: (nodeId: string, width: number, height: number, position?: Position) => void;
    onContentChange: (nodeId: string, content: string) => void;
    onVideoIntrinsicSize?: (nodeId: string, size: CanvasMediaIntrinsicSize) => void;
    onToggleBatch?: (nodeId: string) => void;
    onSetBatchPrimary?: (node: CanvasNodeData) => void;
    onRetry?: (node: CanvasNodeData) => void;
    onCancelTask?: (node: CanvasNodeData) => void;
    onOpenTaskDetails?: (node: CanvasNodeData) => void;
    onOpenVersions?: (node: CanvasNodeData) => void;
    onViewImage?: (node: CanvasNodeData) => void;
    onReplaceMedia?: (node: CanvasNodeData) => void;
    onOpenTextEditor?: (node: CanvasNodeData) => void;
    onOpenDirector?: (node: CanvasNodeData) => void;
    onContextMenu: (event: React.MouseEvent, nodeId: string) => void;
};

type NodeContentRendererProps = {
    node: CanvasNodeData;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    isEditingContent: boolean;
    textareaRef: React.RefObject<CanvasTextDraftEditorHandle | null>;
    isBatchRoot: boolean;
    batchCount: number;
    batchExpanded: boolean;
    batchOpening: boolean;
    batchRecovering: boolean;
    renderNodeContent?: (node: CanvasNodeData) => ReactNode;
    onContentChange: (nodeId: string, content: string) => void;
    onVideoIntrinsicSize?: (nodeId: string, size: CanvasMediaIntrinsicSize) => void;
    onStopEditing: () => void;
    mentionReferences: CanvasResourceReference[];
    onRetry?: (node: CanvasNodeData) => void;
    onCancelTask?: (node: CanvasNodeData) => void;
    onOpenTaskDetails?: (node: CanvasNodeData) => void;
    onToggleBatch?: () => void;
    mediaVisibilityTier: CanvasMediaVisibilityTier;
    reduceMediaEffects?: boolean;
};

export const CanvasNode = React.memo(function CanvasNode({
    data,
    dragOffset,
    scale,
    isSelected,
    isRelated,
    isFocusRelated,
    isConnectionTarget,
    isConnecting,
    showImageInfo,
    mediaVisibilityTier,
    reduceMediaEffects = false,
    readOnly = false,
    resourceLabel,
    mentionReferences = [],
    renderNodeContent,
    batchCount = 0,
    batchExpanded = false,
    batchClosing = false,
    batchOpening = false,
    batchRecovering = false,
    batchMotion,
    onMouseDown,
    onHoverStart,
    onHoverEnd,
    onConnectStart,
    onResizeCommit,
    onContentChange,
    onVideoIntrinsicSize,
    onToggleBatch,
    onSetBatchPrimary,
    onRetry,
    onCancelTask,
    onOpenTaskDetails,
    onOpenVersions,
    onViewImage,
    onReplaceMedia,
    onOpenTextEditor,
    onOpenDirector,
    onContextMenu,
}: CanvasNodeProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [hovered, setHovered] = useState(false);
    const [isEditingContent, setIsEditingContent] = useState(false);
    const isVideoComposition = data.type === CanvasNodeType.Video && data.metadata?.videoEditOperation === "concat";
    const hasImageContent = data.type === CanvasNodeType.Image && Boolean(data.metadata?.content);
    const hasVideoContent = data.type === CanvasNodeType.Video && !isVideoComposition && Boolean(data.metadata?.content);
    const hasAudioContent = data.type === CanvasNodeType.Audio && Boolean(data.metadata?.content);
    const hasMediaContent = hasImageContent || hasVideoContent || hasAudioContent;
    const isGeneratingNode = data.type !== CanvasNodeType.Frame && data.metadata?.status === "loading";
    const isBatchRoot = data.type === CanvasNodeType.Image && Boolean(data.metadata?.isBatchRoot) && batchCount > 1;
    const isBatchChild = data.type === CanvasNodeType.Image && Boolean(data.metadata?.batchRootId);
    const showStatusTrack = Boolean(data.metadata?.locked || (!isVideoComposition && (resourceLabel || isBatchRoot || (isBatchChild && !readOnly) || (hasMediaContent && !readOnly))));
    const isActive = isConnectionTarget || isSelected || isFocusRelated;
    const flushMediaContent = hasImageContent || hasVideoContent;
    const visualState: CanvasNodeVisualState = data.metadata?.status === "error" ? "error" : data.metadata?.status === "loading" ? "loading" : isConnectionTarget ? "target" : isActive ? "selected" : isRelated && !isBatchChild ? "related" : "idle";
    const assetTags = data.metadata?.assetTags?.filter((tag) => tag.trim()) || [];
    const scriptMinHeight = data.type === CanvasNodeType.Script ? storyboardMinNodeHeight(data.metadata?.storyboardComposerHeight) : null;
    const textareaRef = useRef<CanvasTextDraftEditorHandle>(null);
    const nodeElementRef = useRef<HTMLDivElement>(null);
    const resizeRef = useRef<CanvasResizeGesture>({
        active: false,
        corner: "bottom-right",
        startX: 0,
        startY: 0,
        scale: 1,
        nodeId: data.id,
        initial: { x: data.position.x, y: data.position.y, width: data.width, height: data.height },
        minWidth: 220,
        minHeight: 160,
        lastPreview: null,
        cleanup: null,
    });

    useEffect(() => {
        const textarea = textareaRef.current?.element;
        if (!textarea) return;

        const handleWheel = (event: WheelEvent) => event.stopPropagation();
        textarea.addEventListener("wheel", handleWheel, { passive: false });
        return () => textarea.removeEventListener("wheel", handleWheel);
    }, [data.type, isEditingContent]);

    useEffect(() => {
        if (!isEditingContent) return;
        const textarea = textareaRef.current?.element;
        textarea?.focus();
        textarea?.setSelectionRange(textarea.value.length, textarea.value.length);
    }, [isEditingContent]);

    useEffect(() => {
        if (!isEditingContent) return;

        const handleOutsidePointerDown = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (isEditingContent && textareaRef.current?.element?.contains(target)) return;

            textareaRef.current?.commit();
            setIsEditingContent(false);
        };

        window.addEventListener("pointerdown", handleOutsidePointerDown, true);
        return () => window.removeEventListener("pointerdown", handleOutsidePointerDown, true);
    }, [isEditingContent]);

    const handleResizeMove = useCallback((event: MouseEvent) => {
        const state = resizeRef.current;
        if (!state.active) return;

        const horizontalMovement = (event.clientX - state.startX) / state.scale;
        const verticalMovement = (event.clientY - state.startY) / state.scale;
        const preview = calculateCanvasResizePreview({
            initial: state.initial,
            delta: {
                x: state.corner.includes("left") ? -horizontalMovement : horizontalMovement,
                y: state.corner.includes("top") ? -verticalMovement : verticalMovement,
            },
            corner: state.corner,
            minWidth: state.minWidth,
            minHeight: state.minHeight,
            aspectRatio: state.aspectRatio,
        });
        state.lastPreview = preview;
        if (nodeElementRef.current) applyCanvasLiveResize(nodeElementRef.current, preview);
    }, []);

    const handleResizeUp = useCallback(() => {
        const state = resizeRef.current;
        if (!state.active) return;
        const preview = state.lastPreview;
        state.active = false;
        state.lastPreview = null;
        state.cleanup?.();
        state.cleanup = null;
        if (nodeElementRef.current) clearCanvasLiveResize(nodeElementRef.current);
        if (preview) onResizeCommit(state.nodeId, preview.width, preview.height, { x: preview.x, y: preview.y });
    }, [onResizeCommit]);

    const handleResizeCancel = useCallback(() => {
        const state = resizeRef.current;
        if (!state.active) return;
        state.active = false;
        state.lastPreview = null;
        state.cleanup?.();
        state.cleanup = null;
        if (nodeElementRef.current) clearCanvasLiveResize(nodeElementRef.current);
    }, []);

    const handleResizeMouseDown = (event: React.MouseEvent, corner: CanvasResizeCorner) => {
        event.stopPropagation();
        event.preventDefault();
        const minWidth = data.type === CanvasNodeType.Script ? 800 : data.type === CanvasNodeType.Audio ? 300 : 220;
        const minHeight = scriptMinHeight || (data.type === CanvasNodeType.Audio ? 220 : 160);
        resizeRef.current = {
            active: true,
            corner,
            startX: event.clientX,
            startY: event.clientY,
            scale,
            nodeId: data.id,
            initial: { x: data.position.x, y: data.position.y, width: data.width, height: data.height },
            minWidth,
            minHeight,
            aspectRatio: (data.type === CanvasNodeType.Image && !data.metadata?.freeResize) || data.type === CanvasNodeType.Video ? (data.metadata?.naturalWidth || data.width) / (data.metadata?.naturalHeight || data.height || 1) : undefined,
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

    useEffect(() => () => handleResizeCancel(), [handleResizeCancel]);

    return (
        <div
            ref={nodeElementRef}
            data-node-id={data.id}
            className={`node-element absolute flex select-none flex-col ${dragOffset ? "cursor-grabbing" : "cursor-default"} ${isSelected ? "z-50" : "z-10"}`}
            style={{
                transform: `translate(calc(var(--canvas-live-resize-x, ${data.position.x}px) + var(--canvas-live-drag-x, 0px)), calc(var(--canvas-live-resize-y, ${data.position.y}px) + var(--canvas-live-drag-y, 0px)))`,
                width: `var(--canvas-live-resize-width, ${data.width}px)`,
                height: `var(--canvas-live-resize-height, ${data.height}px)`,
                contain: "layout style",
            }}
            onMouseEnter={() => {
                setHovered(true);
                onHoverStart(data.id);
            }}
            onMouseLeave={() => {
                setHovered(false);
                onHoverEnd(data.id);
            }}
            onContextMenu={(event) => onContextMenu(event, data.id)}
        >
            <CanvasNodeSurface
                className={`canvas-node-shell ${isGeneratingNode ? "canvas-node-shell-generating" : ""}`}
                state={visualState}
                theme={theme}
                flushContent={flushMediaContent}
                onMouseDown={(event) => onMouseDown(event, data.id)}
                onDoubleClick={(event) => {
                    if (isBatchRoot) {
                        event.stopPropagation();
                        onToggleBatch?.(data.id);
                        return;
                    }
                    if (data.type === CanvasNodeType.Image && hasImageContent) {
                        event.stopPropagation();
                        onViewImage?.(data);
                        return;
                    }
                    if (data.metadata?.directorSceneId) {
                        event.stopPropagation();
                        onOpenDirector?.(data);
                        return;
                    }
                    if (!readOnly && data.type === CanvasNodeType.Text && data.metadata?.workflowKind === "character" && data.metadata.characterAssetId) {
                        event.stopPropagation();
                        onOpenTextEditor?.(data);
                        return;
                    }
                    if (readOnly || data.type !== CanvasNodeType.Text) return;
                    event.stopPropagation();
                    setIsEditingContent(true);
                }}
            >
                <div
                    className={`relative flex h-full w-full items-center justify-center rounded-[inherit] ${isBatchRoot || data.type === CanvasNodeType.Script ? "overflow-visible" : "overflow-hidden"}`}
                    style={
                        {
                            background: hasImageContent || hasVideoContent ? "transparent" : theme.node.fill,
                            "--batch-from-x": `${batchMotion?.x || 0}px`,
                            "--batch-from-y": `${batchMotion?.y || 0}px`,
                            "--batch-from-rotate": `${6 + (batchMotion?.index || 0) * 4}deg`,
                            animation: data.metadata?.batchRootId ? (batchClosing ? "canvas-batch-child-out 260ms cubic-bezier(.4,0,.2,1) both" : "canvas-batch-child-in 340ms cubic-bezier(.2,.85,.18,1) both") : undefined,
                            animationDelay: data.metadata?.batchRootId ? `${batchClosing ? 0 : 45 + (batchMotion?.index || 0) * 24}ms` : undefined,
                        } as React.CSSProperties
                    }
                >
                    <NodeContent
                        node={data}
                        theme={theme}
                        isEditingContent={isEditingContent}
                        textareaRef={textareaRef}
                        isBatchRoot={isBatchRoot}
                        batchCount={batchCount}
                        batchExpanded={batchExpanded}
                        batchOpening={batchOpening}
                        batchRecovering={batchRecovering}
                        renderNodeContent={renderNodeContent}
                        mentionReferences={mentionReferences}
                        onContentChange={onContentChange}
                        onVideoIntrinsicSize={onVideoIntrinsicSize}
                        onStopEditing={() => setIsEditingContent(false)}
                        onRetry={onRetry}
                        onCancelTask={onCancelTask}
                        onOpenTaskDetails={onOpenTaskDetails}
                        onToggleBatch={() => onToggleBatch?.(data.id)}
                        mediaVisibilityTier={mediaVisibilityTier}
                        reduceMediaEffects={reduceMediaEffects}
                    />
                </div>

                {(hasImageContent || hasVideoContent) && !readOnly ? (
                    <div
                        className={`absolute bottom-[10%] left-1/2 z-40 -translate-x-1/2 motion-safe:transition motion-safe:duration-200 ${hovered || isSelected ? "translate-y-0 opacity-100" : "pointer-events-none translate-y-3 opacity-0"}`}
                        onMouseDown={(event) => event.stopPropagation()}
                        onPointerDown={(event) => event.stopPropagation()}
                    >
                        <CanvasNodeAction icon={<Replace />} label="替换媒体" onClick={() => onReplaceMedia?.(data)} />
                    </div>
                ) : null}

                {data.type === CanvasNodeType.Text && data.metadata?.workflowKind !== "character" && !readOnly ? (
                    <div
                        className={`absolute bottom-[10%] left-1/2 z-40 -translate-x-1/2 motion-safe:transition motion-safe:duration-200 ${hovered || isSelected ? "translate-y-0 opacity-100" : "pointer-events-none translate-y-3 opacity-0"}`}
                        onMouseDown={(event) => event.stopPropagation()}
                        onPointerDown={(event) => event.stopPropagation()}
                    >
                        <CanvasNodeAction icon={<Maximize2 />} label="放大编辑文本" onClick={() => onOpenTextEditor?.(data)} />
                    </div>
                ) : null}

                {data.metadata?.versionLabel ? (
                    <button
                        type="button"
                        className="absolute left-3 top-3 z-40 inline-flex h-7 items-center gap-1 rounded-md border px-2 text-[10px] font-semibold backdrop-blur transition hover:brightness-110"
                        style={{ background: theme.toolbar.panel, borderColor: data.metadata.versionPrimary ? theme.node.activeStroke : theme.toolbar.border, color: data.metadata.versionPrimary ? theme.node.activeStroke : theme.node.text }}
                        title="查看版本对比"
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => {
                            event.stopPropagation();
                            onOpenVersions?.(data);
                        }}
                    >
                        <Star className={`size-3 ${data.metadata.versionPrimary ? "fill-current" : ""}`} />
                        {data.metadata.versionLabel}
                    </button>
                ) : null}
                {showStatusTrack ? (
                    <div className={`absolute right-3 top-3 z-40 flex min-w-0 items-center justify-end gap-1 ${data.metadata?.versionLabel ? "max-w-[calc(100%-104px)]" : "max-w-[calc(100%-24px)]"}`}>
                        {resourceLabel ? <ResourceLabelBadge reference={resourceLabel} theme={theme} /> : null}
                        {hasMediaContent && !readOnly ? <ResourceStorageBadge storageKey={data.metadata?.storageKey} active={isActive} theme={theme} /> : null}
                        {isBatchRoot ? <BatchToggleBadge count={batchCount} expanded={batchExpanded} theme={theme} onToggle={() => onToggleBatch?.(data.id)} /> : null}
                        {isBatchChild && !readOnly ? <BatchPrimaryBadge visible={hovered || isSelected} theme={theme} onSelect={() => onSetBatchPrimary?.(data)} /> : null}
                        {data.metadata?.locked ? <NodeLockBadge theme={theme} /> : null}
                    </div>
                ) : null}
                {assetTags.length || (showImageInfo && hasImageContent) ? (
                    <div className="pointer-events-none absolute inset-x-3 bottom-3 z-40 flex items-end justify-between gap-2">
                        {assetTags.length ? <AssetTagBadges tags={assetTags} theme={theme} /> : null}
                        {showImageInfo && hasImageContent ? <ImageInfoBar node={data} /> : null}
                    </div>
                ) : null}

                {!hasImageContent && !hasVideoContent && !hasAudioContent ? <div className="pointer-events-none absolute inset-x-0 bottom-0 h-12" style={{ background: `linear-gradient(to top, ${theme.canvas.background}66, transparent)` }} /> : null}
            </CanvasNodeSurface>

            {!readOnly && !data.metadata?.locked ? (
                <div className="canvas-node-handles pointer-events-none absolute inset-0 z-50 overflow-visible">
                    <ResizeHandle corner="top-left" onMouseDown={handleResizeMouseDown} />
                    <ResizeHandle corner="top-right" onMouseDown={handleResizeMouseDown} />
                    <ResizeHandle corner="bottom-left" onMouseDown={handleResizeMouseDown} />
                    <ResizeHandle corner="bottom-right" onMouseDown={handleResizeMouseDown} />
                </div>
            ) : null}

            {!readOnly && data.type !== CanvasNodeType.Script ? <ConnectionHandleDot side="left" scale={scale} visible={hovered || isSelected || isConnecting} theme={theme} onPointerDown={(event) => onConnectStart(event, data.id, "target")} /> : null}
            {!readOnly && data.type !== CanvasNodeType.Script ? (
                <ConnectionHandleDot side="right" scale={scale} visible={data.type !== CanvasNodeType.Config && (hovered || isSelected || isConnecting)} theme={theme} onPointerDown={(event) => onConnectStart(event, data.id, "source")} />
            ) : null}
        </div>
    );
});

function NodeContent(props: NodeContentRendererProps) {
    const hasCustomContent =
        props.node.type === CanvasNodeType.Config ||
        props.node.type === CanvasNodeType.Script ||
        props.node.metadata?.videoEditOperation === "concat" ||
        (props.node.metadata?.workflowKind === "character" && Boolean(props.node.metadata.characterAssetId)) ||
        (props.node.metadata?.workflowKind === "story_input" && !props.isEditingContent) ||
        (props.node.metadata?.workflowKind === "styleboard" && !props.node.metadata.content);
    if (hasCustomContent && props.renderNodeContent) return props.renderNodeContent(props.node);
    if (props.isBatchRoot) return <ImageNodeContent {...props} />;
    if (props.node.metadata?.status === "loading") return <LoadingContent node={props.node} theme={props.theme} onCancelTask={props.onCancelTask} onOpenTaskDetails={props.onOpenTaskDetails} />;
    if (props.node.metadata?.status === "error") return <ErrorContent node={props.node} theme={props.theme} onRetry={props.onRetry} />;

    const Renderer = nodeContentRenderers[props.node.type];
    return Renderer ? <Renderer {...props} /> : <UnknownNodeContent theme={props.theme} />;
}

const nodeContentRenderers = {
    [CanvasNodeType.Text]: TextContent,
    [CanvasNodeType.Script]: UnknownNodeContent,
    [CanvasNodeType.Skill]: SkillContent,
    [CanvasNodeType.Image]: ImageNodeContent,
    [CanvasNodeType.Config]: EmptyImageContent,
    [CanvasNodeType.Video]: VideoNodeContent,
    [CanvasNodeType.Audio]: AudioNodeContent,
    [CanvasNodeType.Frame]: UnknownNodeContent,
} satisfies Record<CanvasNodeType, (props: NodeContentRendererProps) => ReactNode>;

function LoadingContent({ node, theme, onCancelTask, onOpenTaskDetails }: Pick<NodeContentRendererProps, "node" | "theme" | "onCancelTask" | "onOpenTaskDetails">) {
    const taskId = node.metadata?.taskId;
    const progress = typeof node.metadata?.taskProgress === "number" ? Math.max(0, Math.min(100, Math.round(node.metadata.taskProgress))) : null;
    const statusLabel = taskStatusLabel(node.metadata?.taskStatus);
    const elapsed = useTaskElapsed(node.metadata?.taskCreatedAt);
    return (
        <CanvasNodeStatusLayout
            icon={<LoaderCircle className="size-5 animate-spin" />}
            title={node.metadata?.taskStage || (taskId ? "任务处理中" : "正在创建任务")}
            detail={taskId ? `${statusLabel}${progress !== null ? ` · ${progress}%` : ""}` : undefined}
            progress={progress ?? undefined}
            meta={
                taskId ? (
                    <>
                        <Clock3 className="mr-1 inline size-3" />
                        {elapsed} · {shortTaskId(taskId)}
                    </>
                ) : undefined
            }
            actions={
                taskId ? (
                    <>
                        <CanvasNodeAction icon={<FileText />} label="详情" onClick={() => onOpenTaskDetails?.(node)} />
                        <CanvasNodeAction icon={<Square className="fill-current" />} label="取消" tone="danger" onClick={() => onCancelTask?.(node)} />
                    </>
                ) : undefined
            }
            tone="progress"
            theme={theme}
        />
    );
}

function useTaskElapsed(createdAt?: string) {
    const now = useSharedSecondNow(Boolean(createdAt));
    if (!createdAt) return "刚刚";
    const seconds = Math.max(0, Math.floor((now - new Date(createdAt).getTime()) / 1000));
    if (seconds < 60) return `${seconds}秒`;
    const minutes = Math.floor(seconds / 60);
    return minutes < 60 ? `${minutes}分${seconds % 60}秒` : `${Math.floor(minutes / 60)}时${minutes % 60}分`;
}

function taskStatusLabel(status?: string) {
    if (status === "queued") return "排队中";
    if (status === "running") return "生成中";
    if (status === "succeeded") return "任务已完成";
    if (status === "failed") return "任务失败";
    if (status === "cancelled") return "任务已取消";
    return status ? "未知任务状态" : "等待任务状态";
}

function shortTaskId(id: string) {
    if (id.length <= 20) return id;
    return `${id.slice(0, 14)}...${id.slice(-4)}`;
}

function ErrorContent({ node, theme, onRetry }: Pick<NodeContentRendererProps, "node" | "theme" | "onRetry">) {
    const moderationFailure = node.metadata?.generationErrorCode === CONTENT_MODERATION_ERROR_CODE || isContentModerationError(node.metadata?.errorDetails);
    const settlementPending = node.metadata?.generationErrorCode === AGENT_MEDIA_SETTLEMENT_PENDING_ERROR_CODE;
    return (
        <CanvasNodeStatusLayout
            icon={<TriangleAlert className="size-5" />}
            title={settlementPending ? "费用待核对" : "生成失败"}
            detail={node.metadata?.errorDetails || "生成失败"}
            meta={moderationFailure ? "修改节点提示词后，可重新点击生成。" : settlementPending ? "核对完成后系统会自动更新节点。" : undefined}
            actions={!moderationFailure && !settlementPending ? <CanvasNodeAction icon={<RefreshCw />} label="重试" onClick={() => onRetry?.(node)} /> : undefined}
            tone={settlementPending ? "neutral" : "danger"}
            theme={theme}
        />
    );
}

function UnknownNodeContent({ theme }: Pick<NodeContentRendererProps, "theme">) {
    return (
        <div className="flex h-full w-full items-center justify-center text-sm" style={{ color: theme.node.placeholder }}>
            未知节点
        </div>
    );
}

function TextContent({ node, theme, isEditingContent, textareaRef, mentionReferences, onContentChange, onStopEditing }: NodeContentRendererProps) {
    const fontSize = node.metadata?.fontSize || 14;
    const textStyle = { fontSize: `${fontSize}px`, lineHeight: `${Math.round(fontSize * 1.65)}px`, color: theme.node.text, boxSizing: "border-box" } as React.CSSProperties;
    const richTextHTML = useMemo(() => canvasRichTextHTML(node.metadata?.richText), [node.metadata?.richText]);

    return (
        <div className="flex h-full w-full flex-col overflow-hidden pt-10">
            {isEditingContent ? (
                <CanvasTextDraftEditor
                    ref={textareaRef}
                    nodeId={node.id}
                    className="thin-scrollbar block h-full w-full resize-none overflow-y-auto whitespace-pre-wrap break-words border-none bg-transparent px-4 pt-0 pb-4 m-0 font-mono outline-none select-text appearance-none"
                    style={textStyle}
                    value={node.metadata?.content || ""}
                    references={mentionReferences}
                    highlightLabels={false}
                    onCommit={onContentChange}
                    onStopEditing={onStopEditing}
                    onMouseDown={(event) => event.stopPropagation()}
                    onPointerDown={(event) => event.stopPropagation()}
                    onWheel={(event) => event.stopPropagation()}
                />
            ) : richTextHTML ? (
                <div
                    className="thin-scrollbar block h-full w-full overflow-y-auto break-words bg-transparent px-4 pb-4 font-mono [&_a]:underline [&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:pl-3 [&_blockquote]:opacity-70 [&_code]:rounded [&_code]:bg-black/6 [&_code]:px-1 dark:[&_code]:bg-white/8 [&_h1]:my-2 [&_h1]:text-[1.55em] [&_h1]:font-semibold [&_h2]:my-2 [&_h2]:text-[1.3em] [&_h2]:font-semibold [&_h3]:my-1.5 [&_h3]:text-[1.12em] [&_h3]:font-semibold [&_hr]:my-3 [&_li]:my-0.5 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-5 [&_p]:my-1 [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-black/90 [&_pre]:p-2 [&_pre]:text-white [&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-5"
                    style={textStyle}
                    onWheel={(event) => event.stopPropagation()}
                    dangerouslySetInnerHTML={{ __html: richTextHTML }}
                />
            ) : (
                <div className="thin-scrollbar block h-full w-full overflow-y-auto whitespace-pre-wrap break-words bg-transparent px-4 pt-0 pb-4 font-mono" style={textStyle} onWheel={(event) => event.stopPropagation()}>
                    {node.metadata?.content || <span style={{ color: theme.node.placeholder }}>双击编辑文字</span>}
                </div>
            )}
        </div>
    );
}

function SkillContent({ node, theme }: NodeContentRendererProps) {
    const skill = node.metadata?.skillSnapshot;
    const tags = skill?.tags?.slice(0, 4) || [];
    const template = skill?.template || node.metadata?.content || "";

    return (
        <div className="flex h-full w-full flex-col overflow-hidden p-4" style={{ color: theme.node.text }}>
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2">
                        <span className="grid size-8 shrink-0 place-items-center rounded-xl" style={{ background: `${theme.node.activeStroke}18`, color: theme.node.activeStroke }}>
                            <BookOpenCheck className="size-4" />
                        </span>
                        <div className="min-w-0">
                            <div className="truncate text-sm font-semibold">{skill?.name || node.title || "技能"}</div>
                            <div className="mt-0.5 flex items-center gap-1.5 text-[11px]" style={{ color: theme.node.muted }}>
                                <span>{skillCategoryLabel(skill?.category)}</span>
                                <span>·</span>
                                <span>{skillOutputModeLabel(skill?.outputMode)}</span>
                                {skill?.version ? (
                                    <>
                                        <span>·</span>
                                        <span>v{skill.version}</span>
                                    </>
                                ) : null}
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            {skill?.description ? (
                <div className="mt-3 line-clamp-2 text-xs leading-5" style={{ color: theme.node.muted }}>
                    {skill.description}
                </div>
            ) : null}

            <div className="thin-scrollbar mt-3 min-h-0 flex-1 overflow-hidden px-0 py-2 text-xs leading-5" style={{ color: theme.node.text }}>
                <div className="mb-1 font-semibold opacity-55">模板</div>
                <div className="line-clamp-4 whitespace-pre-wrap break-words">{template || "未配置技能模板"}</div>
            </div>

            <div className="mt-3 flex flex-wrap gap-1.5">
                {tags.length ? (
                    tags.map((tag) => (
                        <span key={tag} className="rounded-md border px-1.5 py-0.5 text-[10px]" style={{ borderColor: theme.node.stroke, color: theme.node.muted }}>
                            {tag}
                        </span>
                    ))
                ) : (
                    <span className="text-[11px]" style={{ color: theme.node.muted }}>
                        连接到生成配置节点后生效
                    </span>
                )}
            </div>
        </div>
    );
}

function skillCategoryLabel(category?: string) {
    if (category === "writing") return "剧情";
    if (category === "storyboard") return "分镜";
    if (category === "image") return "生图";
    if (category === "video") return "视频";
    return "通用";
}

function skillOutputModeLabel(mode?: string) {
    if (mode === "json") return "JSON";
    if (mode === "image_prompt") return "生图提示词";
    if (mode === "workflow") return "工作流";
    return "文本";
}

function ResourceLabelBadge({ reference, theme }: { reference: CanvasResourceReference; theme: CanvasTheme }) {
    return (
        <span
            className="pointer-events-none min-w-0 max-w-28 truncate rounded-md px-1.5 py-1 text-[10px] font-medium leading-none text-white shadow-sm"
            style={{ background: reference.active ? theme.accent.primary : "rgba(0,0,0,.35)", opacity: reference.active ? 1 : 0.75 }}
            title={reference.title || reference.label}
        >
            {reference.label}
        </span>
    );
}

function ResourceStorageBadge({ storageKey, active, theme }: { storageKey?: string; active: boolean; theme: CanvasTheme }) {
    const location = resourceStorageLocation(storageKey);
    const background = active ? (location === "local" ? "rgba(245,158,11,.9)" : theme.accent.primary) : "rgba(0,0,0,.35)";
    return (
        <span className="pointer-events-auto shrink-0 rounded-md px-1.5 py-1 text-[10px] font-medium leading-none text-white shadow-sm" style={{ background, opacity: active ? 1 : 0.75 }} title={resourceStorageTitle(storageKey)}>
            {resourceStorageLabel(storageKey)}
        </span>
    );
}

function NodeLockBadge({ theme }: { theme: CanvasTheme }) {
    return (
        <span className="pointer-events-none grid size-7 shrink-0 place-items-center rounded-md border backdrop-blur" style={{ background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.muted }} title="节点已锁定">
            <Lock className="size-3.5" />
        </span>
    );
}

function BatchToggleBadge({ count, expanded, theme, onToggle }: { count: number; expanded: boolean; theme: CanvasTheme; onToggle: () => void }) {
    return (
        <button
            type="button"
            className="canvas-node-tool-button inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 text-[10px] font-semibold backdrop-blur-md"
            style={{ background: `${theme.toolbar.panel}d9`, borderColor: `${theme.toolbar.border}cc`, color: theme.node.text }}
            aria-label={expanded ? "图片组已展开" : "图片组已收起"}
            onClick={(event) => {
                event.stopPropagation();
                onToggle();
            }}
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
        >
            <span className="leading-none" style={{ color: theme.accent.primary }}>
                {count}
            </span>
            <ChevronRight className={`size-3 opacity-55 transition-transform ${expanded ? "rotate-90" : ""}`} />
        </button>
    );
}

function BatchPrimaryBadge({ visible, theme, onSelect }: { visible: boolean; theme: CanvasTheme; onSelect: () => void }) {
    return (
        <button
            type="button"
            className={`canvas-node-tool-button inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 text-[10px] font-medium backdrop-blur-md transition-opacity ${visible ? "opacity-100" : "pointer-events-none opacity-0"}`}
            style={{ background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
            onClick={(event) => {
                event.stopPropagation();
                onSelect();
            }}
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
        >
            <Star className="size-3" style={{ color: theme.accent.primary }} />
            主图
        </button>
    );
}

function AssetTagBadges({ tags, theme }: { tags: string[]; theme: (typeof canvasThemes)[keyof typeof canvasThemes] }) {
    return (
        <div className="flex min-w-0 flex-1 flex-wrap items-end gap-1">
            {tags.map((tag, index) => (
                <span
                    key={`${tag}-${index}`}
                    className="max-w-full truncate rounded-md border px-1.5 py-1 text-[10px] font-medium leading-none backdrop-blur-sm"
                    style={{ background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
                >
                    {tag.trim()}
                </span>
            ))}
        </div>
    );
}

function ImageNodeContent(props: NodeContentRendererProps) {
    if (!props.node.metadata?.content && props.isBatchRoot) {
        const content =
            props.node.metadata?.status === "loading" ? (
                <LoadingContent node={props.node} theme={props.theme} />
            ) : props.node.metadata?.status === "error" ? (
                <ErrorContent node={props.node} theme={props.theme} onRetry={props.onRetry} />
            ) : (
                <EmptyImageContent {...props} isBatchRoot={false} />
            );
        return (
            <BatchFrame batchCount={props.batchCount} batchExpanded={props.batchExpanded} batchOpening={props.batchOpening} batchRecovering={props.batchRecovering} theme={props.theme} onToggleBatch={props.onToggleBatch}>
                {content}
            </BatchFrame>
        );
    }
    if (!props.node.metadata?.content) return <EmptyImageContent {...props} />;

    return (
        <ImageContent
            node={props.node}
            theme={props.theme}
            isBatchRoot={props.isBatchRoot}
            batchCount={props.batchCount}
            batchExpanded={props.batchExpanded}
            batchOpening={props.batchOpening}
            batchRecovering={props.batchRecovering}
            onToggleBatch={props.onToggleBatch}
        />
    );
}

function EmptyImageContent({ node, theme, isBatchRoot, batchCount, batchExpanded, batchOpening, batchRecovering, onToggleBatch }: NodeContentRendererProps) {
    const isCharacterReference = node.metadata?.workflowKind === "character" && node.metadata?.characterView === "multi";
    const content = (
        <CanvasNodeEmptyState icon={<ImageIcon className="size-5" />} title={isCharacterReference ? node.metadata?.characterName || node.title : "空图片节点"} description={isCharacterReference ? "多视角参考 · 待生成" : undefined} theme={theme} />
    );
    if (isBatchRoot)
        return (
            <BatchFrame batchCount={batchCount} batchExpanded={batchExpanded} batchOpening={batchOpening} batchRecovering={batchRecovering} theme={theme} onToggleBatch={onToggleBatch}>
                {content}
            </BatchFrame>
        );
    return content;
}

function VideoNodeContent({ node, theme, reduceMediaEffects, mediaVisibilityTier, onVideoIntrinsicSize }: NodeContentRendererProps) {
    return <CanvasMediaNodeContent node={node} theme={theme} reduceMediaEffects={Boolean(reduceMediaEffects)} mediaVisibilityTier={mediaVisibilityTier} onVideoIntrinsicSize={onVideoIntrinsicSize} />;
}

function AudioNodeContent({ node, theme, mediaVisibilityTier }: NodeContentRendererProps) {
    return <CanvasMediaNodeContent node={node} theme={theme} reduceMediaEffects={false} mediaVisibilityTier={mediaVisibilityTier} />;
}

function ImageContent({
    node,
    theme,
    isBatchRoot,
    batchCount,
    batchExpanded,
    batchOpening,
    batchRecovering,
    onToggleBatch,
}: {
    node: CanvasNodeData;
    theme: CanvasTheme;
    isBatchRoot: boolean;
    batchCount: number;
    batchExpanded: boolean;
    batchOpening: boolean;
    batchRecovering: boolean;
    onToggleBatch?: () => void;
}) {
    const url = canvasResourceDisplayUrl(node.metadata?.storageKey, node.metadata?.content || "");

    return (
        <BatchFrame batchCount={isBatchRoot ? batchCount : 0} batchExpanded={batchExpanded} batchOpening={batchOpening} batchRecovering={batchRecovering} theme={theme} onToggleBatch={onToggleBatch}>
            <div className="h-full w-full overflow-hidden">
                {url ? (
                    <img
                        src={url}
                        alt={node.title}
                        loading="lazy"
                        decoding="async"
                        draggable={false}
                        onDragStart={(event) => event.preventDefault()}
                        className={`pointer-events-none block h-full w-full select-none ${node.metadata?.freeResize ? "object-fill" : "object-contain"}`}
                    />
                ) : (
                    <div className="grid size-full place-items-center" style={{ color: theme.node.muted }}>
                        <ImageIcon className="size-5 opacity-45" />
                    </div>
                )}
            </div>
        </BatchFrame>
    );
}

function ImageInfoBar({ node }: { node: CanvasNodeData }) {
    const width = Math.round(node.metadata?.naturalWidth || node.width);
    const height = Math.round(node.metadata?.naturalHeight || node.height);
    const size = formatBytes(node.metadata?.bytes || 0);
    return (
        <span className="ml-auto max-w-full shrink-0 truncate rounded-md bg-black/55 px-2 py-1 text-[11px] font-medium leading-none text-white backdrop-blur-sm">
            {width} x {height}
            {size ? ` · ${size}` : ""}
        </span>
    );
}

function BatchFrame({
    batchCount,
    batchExpanded,
    batchOpening,
    batchRecovering,
    theme,
    onToggleBatch,
    children,
}: {
    batchCount: number;
    batchExpanded: boolean;
    batchOpening: boolean;
    batchRecovering: boolean;
    theme: CanvasTheme;
    onToggleBatch?: () => void;
    children: ReactNode;
}) {
    const isBatchRoot = batchCount > 1;
    return (
        <div
            className="group/batch relative h-full w-full overflow-visible"
            onDoubleClick={
                isBatchRoot
                    ? (event) => {
                          event.stopPropagation();
                          onToggleBatch?.();
                      }
                    : undefined
            }
        >
            {isBatchRoot ? (
                <div className="pointer-events-none absolute inset-0 overflow-visible">
                    {Array.from({ length: Math.min(batchCount - 1, 5) }).map((_, index) => (
                        <div
                            key={index}
                            className="absolute rounded-[inherit] border shadow-[0_14px_34px_rgba(68,64,60,.16)] transition-all duration-300 group-hover/batch:translate-x-2"
                            style={{
                                inset: 0,
                                background: `linear-gradient(135deg, ${theme.node.panel}, ${theme.node.fill})`,
                                borderColor: theme.node.stroke,
                                opacity: batchExpanded && !batchOpening ? 0.34 : 1,
                                transform:
                                    batchOpening || batchRecovering ? `translate(${54 + index * 22}px, ${20 + index * 12}px) rotate(${8 + index * 5}deg) scale(.98)` : `translate(${34 + index * 18}px, ${14 + index * 10}px) rotate(${6 + index * 4}deg)`,
                                zIndex: -index - 1,
                            }}
                        />
                    ))}
                </div>
            ) : null}
            {children}
        </div>
    );
}
function ResizeHandle({ corner, onMouseDown }: { corner: CanvasResizeCorner; onMouseDown: (event: React.MouseEvent, corner: CanvasResizeCorner) => void }) {
    const positionClass = {
        "top-left": "-left-[14px] -top-[14px] cursor-nwse-resize",
        "top-right": "-right-[14px] -top-[14px] cursor-nesw-resize",
        "bottom-left": "-bottom-[14px] -left-[14px] cursor-nesw-resize",
        "bottom-right": "-bottom-[14px] -right-[14px] cursor-nwse-resize",
    }[corner];

    return <div data-canvas-resize-corner={corner} className={`canvas-node-resize-handle pointer-events-auto absolute z-50 size-7 ${positionClass}`} onMouseDown={(event) => onMouseDown(event, corner)} />;
}

function ConnectionHandleDot({ side, scale, visible, theme, onPointerDown }: { side: "left" | "right"; scale: number; visible: boolean; theme: CanvasTheme; onPointerDown: (event: React.PointerEvent) => void }) {
    const inverseScale = 1 / Math.max(scale, 0.05);

    return (
        <div
            className={`canvas-connection-handle absolute top-1/2 z-30 flex -translate-y-1/2 cursor-pointer items-center justify-center transition-opacity duration-150 ${
                side === "left" ? "left-0 -translate-x-1/2" : "right-0 translate-x-1/2"
            } ${visible ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0"}`}
            style={{ width: 40 * inverseScale, height: 40 * inverseScale }}
            onPointerDown={onPointerDown}
        >
            <div className="canvas-node-tool-button grid place-items-center rounded-full border" style={{ width: 16 * inverseScale, height: 16 * inverseScale, borderWidth: inverseScale, background: theme.node.panel, borderColor: theme.accent.primary }}>
                <span className="block rounded-full" style={{ width: 6 * inverseScale, height: 6 * inverseScale, background: theme.accent.primary }} />
            </div>
        </div>
    );
}
