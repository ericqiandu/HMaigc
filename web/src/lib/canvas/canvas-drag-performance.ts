import { FRAME_HEADER_HEIGHT, canFrameContain, isFrameNode } from "@/lib/canvas/canvas-frame";
import type { CanvasNodeData, Position } from "@/types/canvas";

export type CanvasLiveConnectionDragTarget = {
    paths: Array<{ element: SVGPathElement; initialPath: string }>;
    gradient: SVGLinearGradientElement | null;
    initialGradient: { x1: string | null; y1: string | null; x2: string | null; y2: string | null } | null;
    start: Position;
    end: Position;
    movesStart: boolean;
    movesEnd: boolean;
};

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

export function resolveCanvasLiveNodeDragTargets(surface: HTMLElement | null, draggedNodeIds: ReadonlySet<string>) {
    if (!surface || draggedNodeIds.size === 0) return [];
    return Array.from(surface.querySelectorAll<HTMLElement>("[data-node-id]")).filter((element) => draggedNodeIds.has(element.dataset.nodeId || ""));
}

export function applyCanvasLiveNodeDrag(targets: readonly HTMLElement[], offset: Position) {
    for (const target of targets) {
        target.dataset.canvasNodeDragging = "true";
        target.style.setProperty("--canvas-live-drag-x", `${offset.x}px`);
        target.style.setProperty("--canvas-live-drag-y", `${offset.y}px`);
    }
}

export function clearCanvasLiveNodeDrag(targets: readonly HTMLElement[]) {
    for (const target of targets) {
        delete target.dataset.canvasNodeDragging;
        target.style.removeProperty("--canvas-live-drag-x");
        target.style.removeProperty("--canvas-live-drag-y");
    }
}

export function createCanvasConnectionPathD(start: Position, end: Position) {
    const curvature = Math.max(Math.abs(end.x - start.x) * 0.5, 50);
    return `M ${start.x} ${start.y} C ${start.x + curvature} ${start.y}, ${end.x - curvature} ${end.y}, ${end.x} ${end.y}`;
}

export function resolveCanvasLiveConnectionDragTargets(surface: HTMLElement | null, draggedNodeIds: ReadonlySet<string>): CanvasLiveConnectionDragTarget[] {
    if (!surface || draggedNodeIds.size === 0) return [];
    return Array.from(surface.querySelectorAll<SVGGElement>("[data-canvas-connection-id]"))
        .flatMap((group) => {
            const fromNodeId = group.dataset.fromNodeId || "";
            const toNodeId = group.dataset.toNodeId || "";
            const movesStart = draggedNodeIds.has(fromNodeId);
            const movesEnd = draggedNodeIds.has(toNodeId);
            if (!movesStart && !movesEnd) return [];
            const start = { x: Number(group.dataset.startX), y: Number(group.dataset.startY) };
            const end = { x: Number(group.dataset.endX), y: Number(group.dataset.endY) };
            if (![start.x, start.y, end.x, end.y].every(Number.isFinite)) return [];
            const gradient = group.querySelector<SVGLinearGradientElement>("linearGradient");
            return [{
                paths: Array.from(group.querySelectorAll<SVGPathElement>("path")).map((element) => ({ element, initialPath: element.getAttribute("d") || "" })),
                gradient,
                initialGradient: gradient
                    ? { x1: gradient.getAttribute("x1"), y1: gradient.getAttribute("y1"), x2: gradient.getAttribute("x2"), y2: gradient.getAttribute("y2") }
                    : null,
                start,
                end,
                movesStart,
                movesEnd,
            }];
        });
}

export function applyCanvasLiveConnectionDrag(targets: readonly CanvasLiveConnectionDragTarget[], offset: Position) {
    for (const target of targets) {
        const start = target.movesStart ? { x: target.start.x + offset.x, y: target.start.y + offset.y } : target.start;
        const end = target.movesEnd ? { x: target.end.x + offset.x, y: target.end.y + offset.y } : target.end;
        const path = createCanvasConnectionPathD(start, end);
        target.paths.forEach(({ element }) => element.setAttribute("d", path));
        if (target.gradient) {
            target.gradient.setAttribute("x1", String(start.x));
            target.gradient.setAttribute("y1", String(start.y));
            target.gradient.setAttribute("x2", String(end.x));
            target.gradient.setAttribute("y2", String(end.y));
        }
    }
}

export function clearCanvasLiveConnectionDrag(targets: readonly CanvasLiveConnectionDragTarget[]) {
    for (const target of targets) {
        target.paths.forEach(({ element, initialPath }) => element.setAttribute("d", initialPath));
        if (target.gradient && target.initialGradient) {
            restoreAttribute(target.gradient, "x1", target.initialGradient.x1);
            restoreAttribute(target.gradient, "y1", target.initialGradient.y1);
            restoreAttribute(target.gradient, "x2", target.initialGradient.x2);
            restoreAttribute(target.gradient, "y2", target.initialGradient.y2);
        }
    }
}

function restoreAttribute(element: Element, name: string, value: string | null) {
    if (value == null) element.removeAttribute(name);
    else element.setAttribute(name, value);
}
