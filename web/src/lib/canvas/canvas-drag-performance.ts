import { FRAME_HEADER_HEIGHT, canFrameContain, isFrameNode } from "@/lib/canvas/canvas-frame";
import type { CanvasNodeData, Position } from "@/types/canvas";

const DRAG_PREVIEW_SYNC_INTERVAL_MS = 50;

export type FrameDropContext = {
    draggedNodes: Array<{ centerX: number; centerY: number }>;
    frames: Array<{ id: string; left: number; top: number; right: number; bottom: number }>;
};

export function createFrameDropContext(nodes: CanvasNodeData[], draggedNodeIds: Set<string>): FrameDropContext | null {
    const draggedNodes = nodes.filter((node) => draggedNodeIds.has(node.id) && canFrameContain(node)).map((node) => ({ centerX: node.position.x + node.width / 2, centerY: node.position.y + node.height / 2 }));
    if (!draggedNodes.length) return null;
    const frames = nodes
        .filter((frame) => isFrameNode(frame) && !frame.metadata?.frame?.collapsed && !draggedNodeIds.has(frame.id))
        .map((frame) => ({ id: frame.id, left: frame.position.x, top: frame.position.y + FRAME_HEADER_HEIGHT, right: frame.position.x + frame.width, bottom: frame.position.y + frame.height }))
        .reverse();
    return { draggedNodes, frames };
}

export function findFrameDropTargetFromContext(context: FrameDropContext | null, offset: Position) {
    if (!context) return null;
    return (
        context.frames.find((frame) =>
            context.draggedNodes.every(({ centerX, centerY }) => {
                const x = centerX + offset.x;
                const y = centerY + offset.y;
                return x >= frame.left && x <= frame.right && y >= frame.top && y <= frame.bottom;
            }),
        )?.id || null
    );
}

export function applyCanvasLiveNodeDrag(surface: HTMLElement | null, offset: Position) {
    if (!surface) return;
    surface.dataset.canvasNodeDragging = "true";
    surface.style.setProperty("--canvas-live-drag-x", `${offset.x}px`);
    surface.style.setProperty("--canvas-live-drag-y", `${offset.y}px`);
}

export function clearCanvasLiveNodeDrag(surface: HTMLElement | null) {
    if (!surface) return;
    delete surface.dataset.canvasNodeDragging;
    surface.style.removeProperty("--canvas-live-drag-x");
    surface.style.removeProperty("--canvas-live-drag-y");
}

export function shouldSyncCanvasDragPreview(now: number, lastSync: number) {
    return now - lastSync >= DRAG_PREVIEW_SYNC_INTERVAL_MS;
}
