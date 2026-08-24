import { useEffect, useState } from "react";

const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const POST_LOAD_STABILITY_DELAY_MS = 2_500;

export function useDeferredMedia() {
    const [enabled, setEnabled] = useState(false);

    useEffect(() => {
        if (window.matchMedia(REDUCED_MOTION_QUERY).matches) return;

        let cancelled = false;
        let idleRequest: number | null = null;
        let stabilityTimeout: number | null = null;
        let idleFallbackTimeout: number | null = null;

        const enable = () => {
            if (!cancelled) setEnabled(true);
        };
        const scheduleWhenIdle = () => {
            if (typeof window.requestIdleCallback === "function") {
                idleRequest = window.requestIdleCallback(enable, { timeout: 2_500 });
                return;
            }
            idleFallbackTimeout = window.setTimeout(enable, 200);
        };
        const scheduleAfterLoad = () => {
            stabilityTimeout = window.setTimeout(scheduleWhenIdle, POST_LOAD_STABILITY_DELAY_MS);
        };

        if (document.readyState === "complete") scheduleAfterLoad();
        else window.addEventListener("load", scheduleAfterLoad, { once: true });

        return () => {
            cancelled = true;
            window.removeEventListener("load", scheduleAfterLoad);
            if (idleRequest !== null) window.cancelIdleCallback(idleRequest);
            if (stabilityTimeout !== null) window.clearTimeout(stabilityTimeout);
            if (idleFallbackTimeout !== null) window.clearTimeout(idleFallbackTimeout);
        };
    }, []);

    return enabled;
}
