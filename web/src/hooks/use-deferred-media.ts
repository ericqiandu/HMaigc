import { useEffect, useState } from "react";

const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

export function useDeferredMedia() {
    const [enabled, setEnabled] = useState(false);

    useEffect(() => {
        if (window.matchMedia(REDUCED_MOTION_QUERY).matches) return;

        let cancelled = false;
        let idleRequest: number | null = null;
        let timeout: number | null = null;

        const enable = () => {
            if (!cancelled) setEnabled(true);
        };
        const schedule = () => {
            if (typeof window.requestIdleCallback === "function") {
                idleRequest = window.requestIdleCallback(enable, { timeout: 2_500 });
                return;
            }
            timeout = window.setTimeout(enable, 200);
        };

        if (document.readyState === "complete") schedule();
        else window.addEventListener("load", schedule, { once: true });

        return () => {
            cancelled = true;
            window.removeEventListener("load", schedule);
            if (idleRequest !== null) window.cancelIdleCallback(idleRequest);
            if (timeout !== null) window.clearTimeout(timeout);
        };
    }, []);

    return enabled;
}
