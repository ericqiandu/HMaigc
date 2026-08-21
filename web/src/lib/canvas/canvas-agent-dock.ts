import type { CanvasViewportSize } from "@/lib/canvas/canvas-viewport";
import type { ViewportTransform } from "@/types/canvas";

export const CANVAS_AGENT_DOCK_DEFAULT_WIDTH = 400;
export const CANVAS_AGENT_DOCK_MIN_WIDTH = 320;
export const CANVAS_AGENT_DOCK_MAX_WIDTH = 560;

const MIN_CANVAS_SCALE = 0.05;
const MAX_CANVAS_SCALE = 2;

export function clampCanvasAgentDockWidth(width: number): number {
    return Math.min(CANVAS_AGENT_DOCK_MAX_WIDTH, Math.max(CANVAS_AGENT_DOCK_MIN_WIDTH, Math.round(width)));
}

export function resizeCanvasAgentDockWidthFromKey(width: number, key: string): number | null {
    if (key === "Home") return CANVAS_AGENT_DOCK_MIN_WIDTH;
    if (key === "End") return CANVAS_AGENT_DOCK_MAX_WIDTH;
    const delta = key === "ArrowLeft" ? 16 : key === "ArrowRight" ? -16 : key === "PageUp" ? 48 : key === "PageDown" ? -48 : 0;
    return delta ? clampCanvasAgentDockWidth(width + delta) : null;
}

export function resizeViewportAroundCenter(viewport: ViewportTransform, previousSize: CanvasViewportSize, nextSize: CanvasViewportSize): ViewportTransform {
    if (previousSize.width <= 0 || previousSize.height <= 0 || nextSize.width <= 0 || nextSize.height <= 0 || viewport.k <= 0) return viewport;

    const worldCenterX = (previousSize.width / 2 - viewport.x) / viewport.k;
    const worldCenterY = (previousSize.height / 2 - viewport.y) / viewport.k;
    const sizeRatio = Math.min(nextSize.width / previousSize.width, nextSize.height / previousSize.height);
    const k = Math.min(MAX_CANVAS_SCALE, Math.max(MIN_CANVAS_SCALE, viewport.k * sizeRatio));

    return {
        x: nextSize.width / 2 - worldCenterX * k,
        y: nextSize.height / 2 - worldCenterY * k,
        k,
    };
}
