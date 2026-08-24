import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useDeferredMedia } from "../src/hooks/use-deferred-media";

let root: Root | null = null;
let idleCallback: IdleRequestCallback | null = null;
let stabilityDelayCallback: (() => void) | null = null;
let stabilityDelayMs: number | null = null;

const originalMatchMedia = window.matchMedia;
const originalRequestIdleCallback = window.requestIdleCallback;
const originalCancelIdleCallback = window.cancelIdleCallback;
const originalSetTimeout = window.setTimeout;
const originalClearTimeout = window.clearTimeout;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    idleCallback = null;
    stabilityDelayCallback = null;
    stabilityDelayMs = null;
    window.matchMedia = originalMatchMedia;
    window.requestIdleCallback = originalRequestIdleCallback;
    window.cancelIdleCallback = originalCancelIdleCallback;
    window.setTimeout = originalSetTimeout;
    window.clearTimeout = originalClearTimeout;
    document.body.replaceChildren();
});

function configureBrowser(reducedMotion: boolean) {
    window.matchMedia = () =>
        ({
            matches: reducedMotion,
            media: "(prefers-reduced-motion: reduce)",
            onchange: null,
            addListener: () => undefined,
            removeListener: () => undefined,
            addEventListener: () => undefined,
            removeEventListener: () => undefined,
            dispatchEvent: () => true,
        }) satisfies MediaQueryList;
    window.requestIdleCallback = (callback) => {
        idleCallback = callback;
        return 1;
    };
    window.cancelIdleCallback = () => undefined;
    window.setTimeout = ((handler: TimerHandler, timeout?: number) => {
        if (typeof handler !== "function") throw new Error("媒体稳定窗口必须使用函数回调");
        stabilityDelayCallback = () => handler();
        stabilityDelayMs = timeout ?? 0;
        return 2;
    }) as typeof window.setTimeout;
    window.clearTimeout = () => undefined;
}

function DeferredMediaProbe() {
    const enabled = useDeferredMedia();
    return <output className="deferred-media-probe" data-enabled={enabled ? "true" : "false"} />;
}

async function renderProbe() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(DeferredMediaProbe)));
    return host;
}

describe("useDeferredMedia", () => {
    test("waits for the post-load stability window and browser idle before enabling heavy media", async () => {
        configureBrowser(false);
        const host = await renderProbe();

        expect(host.querySelector(".deferred-media-probe")?.getAttribute("data-enabled")).toBe("false");
        await act(async () => window.dispatchEvent(new Event("load")));
        expect(stabilityDelayMs).toBe(2_500);
        expect(idleCallback).toBeNull();

        const delayCallback = stabilityDelayCallback;
        if (!delayCallback) throw new Error("未安排媒体稳定窗口");
        await act(async () => delayCallback());
        expect(idleCallback).not.toBeNull();

        const callback = idleCallback;
        if (!callback) throw new Error("未安排空闲媒体加载");
        await act(async () => callback({ didTimeout: false, timeRemaining: () => 50 }));

        expect(host.querySelector(".deferred-media-probe")?.getAttribute("data-enabled")).toBe("true");
    });

    test("keeps heavy media disabled for reduced-motion users", async () => {
        configureBrowser(true);
        const host = await renderProbe();

        await act(async () => window.dispatchEvent(new Event("load")));

        expect(idleCallback).toBeNull();
        expect(host.querySelector(".deferred-media-probe")?.getAttribute("data-enabled")).toBe("false");
    });
});
