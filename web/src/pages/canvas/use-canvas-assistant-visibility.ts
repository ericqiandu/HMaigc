import { useCallback, useEffect, useRef, useState } from "react";

import { CANVAS_AGENT_PANEL_MOTION_MS } from "@/components/canvas/canvas-assistant-panel";

export function useCanvasAssistantVisibility() {
    const closeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [assistantCollapsed, setAssistantCollapsed] = useState(true);
    const [assistantMounted, setAssistantMounted] = useState(false);
    const [assistantClosing, setAssistantClosing] = useState(false);

    const openAgent = useCallback(() => {
        if (closeTimerRef.current) {
            clearTimeout(closeTimerRef.current);
            closeTimerRef.current = null;
        }
        setAssistantMounted(true);
        setAssistantClosing(false);
        setAssistantCollapsed(false);
    }, []);

    const closeAgent = useCallback(() => {
        if (!assistantMounted || assistantClosing) return;
        setAssistantCollapsed(true);
        setAssistantClosing(true);
        closeTimerRef.current = setTimeout(() => {
            closeTimerRef.current = null;
            setAssistantMounted(false);
            setAssistantClosing(false);
        }, CANVAS_AGENT_PANEL_MOTION_MS);
    }, [assistantClosing, assistantMounted]);

    useEffect(() => () => {
        if (closeTimerRef.current) clearTimeout(closeTimerRef.current);
    }, []);

    return {
        assistantClosing,
        assistantMounted,
        assistantOpen: assistantMounted && !assistantCollapsed,
        closeAgent,
        openAgent,
    };
}
