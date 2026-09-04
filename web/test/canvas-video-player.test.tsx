import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasVideoPlayer, type CanvasVideoPlaybackError } from "../src/components/canvas/canvas-video-player";

const mediaPrototype = window.HTMLMediaElement.prototype;
const originalPlay = Object.getOwnPropertyDescriptor(mediaPrototype, "play");
const originalPause = Object.getOwnPropertyDescriptor(mediaPrototype, "pause");
let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    restoreMediaMethod("play", originalPlay);
    restoreMediaMethod("pause", originalPause);
});

describe("CanvasVideoPlayer", () => {
    test("renders a non-native player with a stable custom control layer", async () => {
        await renderPlayer();

        expect(requiredVideo().hasAttribute("controls")).toBeFalse();
        expect(document.querySelector("[data-canvas-video-player]")).not.toBeNull();
        expect(document.querySelector("[data-canvas-video-controls]")).not.toBeNull();
        expect(requiredButton("播放视频")).not.toBeNull();
        expect(requiredButton("静音")).not.toBeNull();
        expect(requiredButton("全屏播放")).not.toBeNull();
    });

    test("projects playback events, seeks, and toggles mute through the media element", async () => {
        installMediaMethods();
        await renderPlayer();
        const video = requiredVideo();
        Object.defineProperty(video, "duration", { configurable: true, value: 120 });
        Object.defineProperty(video, "currentTime", { configurable: true, writable: true, value: 30 });

        await act(async () => {
            video.dispatchEvent(new Event("loadedmetadata"));
            video.dispatchEvent(new Event("timeupdate"));
        });

        const progress = requiredProgress();
        expect(progress.max).toBe("120");
        expect(progress.value).toBe("30");
        expect(document.body.textContent).toContain("00:30 / 02:00");

        await click(requiredButton("播放视频"));
        expect(requiredButton("暂停视频")).not.toBeNull();

        progress.value = "75";
        await act(async () => progress.dispatchEvent(new Event("input", { bubbles: true })));
        expect(video.currentTime).toBe(75);

        await click(requiredButton("静音"));
        expect(video.muted).toBeTrue();
        expect(requiredButton("取消静音")).not.toBeNull();

        await click(requiredButton("暂停视频"));
        expect(requiredButton("播放视频")).not.toBeNull();
    });

    test("supports keyboard playback, seeking, and mute shortcuts", async () => {
        installMediaMethods();
        await renderPlayer();
        const player = requiredPlayer();
        const video = requiredVideo();
        Object.defineProperty(video, "duration", { configurable: true, value: 40 });
        Object.defineProperty(video, "currentTime", { configurable: true, writable: true, value: 10 });
        await act(async () => video.dispatchEvent(new Event("loadedmetadata")));

        await press(player, " ");
        expect(requiredButton("暂停视频")).not.toBeNull();
        await press(player, "ArrowRight");
        expect(video.currentTime).toBe(15);
        await press(player, "ArrowLeft");
        expect(video.currentTime).toBe(10);
        await press(player, "m");
        expect(video.muted).toBeTrue();
        await press(player, "Enter");
        expect(requiredButton("播放视频")).not.toBeNull();
    });

    test("reports fullscreen rejection and media failure as structured visible errors", async () => {
        installMediaMethods();
        const errors: CanvasVideoPlaybackError[] = [];
        await renderPlayer((error) => errors.push(error));
        const player = requiredPlayer();
        const video = requiredVideo();
        Object.defineProperty(player, "requestFullscreen", {
            configurable: true,
            value: () => Promise.reject(new Error("fullscreen denied")),
        });
        Object.defineProperty(video, "requestFullscreen", {
            configurable: true,
            value: () => Promise.reject(new Error("video element must not own fullscreen")),
        });

        await click(requiredButton("全屏播放"));
        expect(document.querySelector("[role='alert']")?.textContent).toContain("fullscreen denied");
        expect(errors).toEqual([{ kind: "fullscreen", message: "fullscreen denied" }]);

        await act(async () => video.dispatchEvent(new Event("error")));
        expect(document.querySelector("[role='alert']")?.textContent).toContain("视频加载失败");
        expect(errors[1]).toEqual({ kind: "media", message: "视频加载失败" });
    });

    test("keeps pointer, mouse, and wheel control events out of the canvas", async () => {
        installMediaMethods();
        await renderPlayer();
        const controls = requiredControls();
        const received: string[] = [];
        const record = (event: Event) => received.push(event.type);
        document.body.addEventListener("pointerdown", record);
        document.body.addEventListener("mousedown", record);
        document.body.addEventListener("wheel", record);

        await act(async () => {
            controls.dispatchEvent(new Event("pointerdown", { bubbles: true, cancelable: true }));
            controls.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, cancelable: true }));
            controls.dispatchEvent(new Event("wheel", { bubbles: true, cancelable: true }));
        });

        expect(received).toEqual([]);
    });
});

async function renderPlayer(onPlaybackError: (error: CanvasVideoPlaybackError) => void = () => undefined) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(
            createElement(CanvasVideoPlayer, {
                nodeId: "video-node",
                src: "/videos/hero.mp4",
                title: "测试视频",
                preload: "metadata",
                onPlaybackError,
            }),
        );
    });
}

function installMediaMethods() {
    Object.defineProperty(mediaPrototype, "play", {
        configurable: true,
        value(this: HTMLMediaElement) {
            this.dispatchEvent(new Event("play"));
            return Promise.resolve();
        },
    });
    Object.defineProperty(mediaPrototype, "pause", {
        configurable: true,
        value(this: HTMLMediaElement) {
            this.dispatchEvent(new Event("pause"));
        },
    });
}

function restoreMediaMethod(name: "play" | "pause", descriptor: PropertyDescriptor | undefined) {
    if (descriptor) Object.defineProperty(mediaPrototype, name, descriptor);
    else Reflect.deleteProperty(mediaPrototype, name);
}

async function click(element: HTMLElement) {
    await act(async () => element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true })));
}

async function press(element: HTMLElement, key: string) {
    await act(async () => element.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true })));
}

function requiredPlayer() {
    const player = document.querySelector<HTMLElement>("[data-canvas-video-player]");
    if (!player) throw new Error("缺少画布视频播放器");
    return player;
}

function requiredControls() {
    const controls = document.querySelector<HTMLElement>("[data-canvas-video-controls]");
    if (!controls) throw new Error("缺少视频控制层");
    return controls;
}

function requiredVideo() {
    const video = document.querySelector("video");
    if (!video) throw new Error("缺少视频元素");
    return video;
}

function requiredButton(label: string) {
    const button = document.querySelector<HTMLButtonElement>(`button[aria-label='${label}']`);
    if (!button) throw new Error(`缺少按钮: ${label}`);
    return button;
}

function requiredProgress() {
    const progress = document.querySelector<HTMLInputElement>("input[type='range']");
    if (!progress) throw new Error("缺少视频进度控件");
    return progress;
}
