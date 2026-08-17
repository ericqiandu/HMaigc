import { useCallback, useEffect, useRef, useState, type KeyboardEventHandler, type PointerEventHandler } from "react";

import { CANVAS_AGENT_DOCK_DEFAULT_WIDTH, clampCanvasAgentDockWidth, resizeCanvasAgentDockWidthFromKey } from "@/lib/canvas/canvas-agent-dock";

const CANVAS_AGENT_DOCK_WIDTH_STORAGE_KEY = "hmaigc:canvas-agent-dock-width";

function readPersistedDockWidth(): number {
    if (typeof window === "undefined") return CANVAS_AGENT_DOCK_DEFAULT_WIDTH;
    const value = Number(window.localStorage.getItem(CANVAS_AGENT_DOCK_WIDTH_STORAGE_KEY));
    return Number.isFinite(value) && value > 0 ? clampCanvasAgentDockWidth(value) : CANVAS_AGENT_DOCK_DEFAULT_WIDTH;
}

export function useCanvasAgentDock(): { width: number; startResize: PointerEventHandler<HTMLDivElement>; resizeByKeyboard: KeyboardEventHandler<HTMLDivElement> } {
    const [width, setWidth] = useState(readPersistedDockWidth);
    const widthRef = useRef(width);
    const cleanupRef = useRef<(() => void) | null>(null);

    useEffect(() => {
        widthRef.current = width;
    }, [width]);

    useEffect(() => () => cleanupRef.current?.(), []);

    const startResize = useCallback<PointerEventHandler<HTMLDivElement>>((event) => {
        event.preventDefault();
        cleanupRef.current?.();

        const pointerId = event.pointerId;
        const startX = event.clientX;
        const startWidth = widthRef.current;
        let pendingWidth = startWidth;
        let frame = 0;

        event.currentTarget.setPointerCapture(pointerId);

        const flush = () => {
            frame = 0;
            widthRef.current = pendingWidth;
            setWidth(pendingWidth);
        };
        const schedule = (nextWidth: number) => {
            pendingWidth = clampCanvasAgentDockWidth(nextWidth);
            if (!frame) frame = window.requestAnimationFrame(flush);
        };
        const handleMove = (moveEvent: PointerEvent) => {
            if (moveEvent.pointerId !== pointerId) return;
            schedule(startWidth + startX - moveEvent.clientX);
        };
        const cleanup = () => {
            window.removeEventListener("pointermove", handleMove);
            window.removeEventListener("pointerup", handleEnd);
            window.removeEventListener("pointercancel", handleEnd);
            if (frame) {
                window.cancelAnimationFrame(frame);
                flush();
            }
            window.localStorage.setItem(CANVAS_AGENT_DOCK_WIDTH_STORAGE_KEY, String(widthRef.current));
            cleanupRef.current = null;
        };
        const handleEnd = (endEvent: PointerEvent) => {
            if (endEvent.pointerId !== pointerId) return;
            cleanup();
        };

        window.addEventListener("pointermove", handleMove);
        window.addEventListener("pointerup", handleEnd);
        window.addEventListener("pointercancel", handleEnd);
        cleanupRef.current = cleanup;
    }, []);

    const resizeByKeyboard = useCallback<KeyboardEventHandler<HTMLDivElement>>((event) => {
        const nextWidth = resizeCanvasAgentDockWidthFromKey(widthRef.current, event.key);
        if (nextWidth === null) return;
        event.preventDefault();
        widthRef.current = nextWidth;
        setWidth(nextWidth);
        window.localStorage.setItem(CANVAS_AGENT_DOCK_WIDTH_STORAGE_KEY, String(nextWidth));
    }, []);

    return { width, startResize, resizeByKeyboard };
}
