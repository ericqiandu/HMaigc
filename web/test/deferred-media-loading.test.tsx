import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasMediaNodeContent } from "../src/components/canvas/canvas-media-node-content";
import { useDeferredMedia } from "../src/hooks/use-deferred-media";
import { canvasThemes } from "../src/lib/canvas-theme";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

let root: Root | null = null;
let idleCallback: IdleRequestCallback | null = null;
let stabilityDelayCallback: (() => void) | null = null;
let stabilityDelayMs: number | null = null;

const originalMatchMedia = window.matchMedia;
const originalRequestIdleCallback = window.requestIdleCallback;
const originalCancelIdleCallback = window.cancelIdleCallback;
const originalSetTimeout = window.setTimeout;
const originalClearTimeout = window.clearTimeout;
const originalPause = window.HTMLMediaElement.prototype.pause;

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
    Object.defineProperty(window.HTMLMediaElement.prototype, "pause", { configurable: true, value: originalPause });
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

    test("pauses a visible video before replacing it with a near-tier marker", async () => {
        let pauseCount = 0;
        Object.defineProperty(window.HTMLMediaElement.prototype, "pause", {
            configurable: true,
            value: () => {
                pauseCount += 1;
            },
        });
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        const node = videoNode();

        await act(async () => root?.render(createElement(CanvasMediaNodeContent, { node, theme: canvasThemes.dark, reduceMediaEffects: false, mediaVisibilityTier: "visible" })));
        expect(host.querySelector("video")).not.toBeNull();

        await act(async () => root?.render(createElement(CanvasMediaNodeContent, { node, theme: canvasThemes.dark, reduceMediaEffects: false, mediaVisibilityTier: "near" })));

        expect(pauseCount).toBe(1);
        expect(host.querySelector("video")).toBeNull();
        expect(host.querySelector("[data-canvas-deferred-media]")).not.toBeNull();
    });
});

function videoNode(): CanvasNodeData {
    return {
        id: "tiered-video",
        type: CanvasNodeType.Video,
        title: "分层视频",
        position: { x: 0, y: 0 },
        width: 420,
        height: 236,
        metadata: { content: "https://provider.example/tiered-video.mp4" },
    };
}
