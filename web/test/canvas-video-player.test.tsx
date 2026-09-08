import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasVideoPlayer, type CanvasVideoPlaybackError } from "../src/components/canvas/canvas-video-player";

const mediaPrototype = window.HTMLMediaElement.prototype;
const originalPlay = Object.getOwnPropertyDescriptor(mediaPrototype, "play");
const originalPause = Object.getOwnPropertyDescriptor(mediaPrototype, "pause");
const originalLoad = Object.getOwnPropertyDescriptor(mediaPrototype, "load");
const canvasPrototype = window.HTMLCanvasElement.prototype;
const originalGetContext = Object.getOwnPropertyDescriptor(canvasPrototype, "getContext");
const originalToBlob = Object.getOwnPropertyDescriptor(canvasPrototype, "toBlob");
const originalCreateObjectUrl = Object.getOwnPropertyDescriptor(URL, "createObjectURL");
const originalRevokeObjectUrl = Object.getOwnPropertyDescriptor(URL, "revokeObjectURL");
const anchorPrototype = window.HTMLAnchorElement.prototype;
const originalAnchorClick = Object.getOwnPropertyDescriptor(anchorPrototype, "click");
let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    restoreProperty(mediaPrototype, "play", originalPlay);
    restoreProperty(mediaPrototype, "pause", originalPause);
    restoreProperty(mediaPrototype, "load", originalLoad);
    restoreProperty(canvasPrototype, "getContext", originalGetContext);
    restoreProperty(canvasPrototype, "toBlob", originalToBlob);
    restoreProperty(URL, "createObjectURL", originalCreateObjectUrl);
    restoreProperty(URL, "revokeObjectURL", originalRevokeObjectUrl);
    restoreProperty(anchorPrototype, "click", originalAnchorClick);
});

describe("CanvasVideoPlayer", () => {
    test("renders a non-native player with a stable custom control layer", async () => {
        await renderPlayer();

        expect(requiredVideo().hasAttribute("controls")).toBeFalse();
        expect(requiredVideo().crossOrigin).toBeNull();
        expect(document.querySelector("[data-canvas-video-player]")).not.toBeNull();
        expect(document.querySelector("[data-canvas-video-controls]")).not.toBeNull();
        expect(requiredButton("播放视频")).not.toBeNull();
        expect(requiredButton("静音")).not.toBeNull();
        expect(requiredButton("保存当前视频帧")).not.toBeNull();
        expect(document.querySelector("button[aria-label='全屏播放']")).toBeNull();
        expect(requiredOutput("[data-canvas-video-current-time]").textContent).toBe("0:00");
        expect(requiredOutput("[data-canvas-video-duration]").textContent).toBe("0:00");
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
        expect(requiredOutput("[data-canvas-video-current-time]").textContent).toBe("0:30");
        expect(requiredOutput("[data-canvas-video-duration]").textContent).toBe("2:00");
        expect(requiredProgressShell().style.getPropertyValue("--canvas-video-progress-ratio")).toBe("25%");

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

    test("reports valid intrinsic dimensions once when video metadata loads", async () => {
        const intrinsicSizes: Array<{ width: number; height: number }> = [];
        await renderPlayer(undefined, (size) => intrinsicSizes.push(size));
        const video = requiredVideo();
        Object.defineProperty(video, "videoWidth", { configurable: true, value: 864 });
        Object.defineProperty(video, "videoHeight", { configurable: true, value: 496 });

        await act(async () => {
            video.dispatchEvent(new Event("loadedmetadata"));
            video.dispatchEvent(new Event("loadedmetadata"));
        });

        expect(intrinsicSizes).toEqual([{ width: 864, height: 496 }]);
    });

    test("supports keyboard playback, seeking, mute, and fullscreen shortcuts", async () => {
        installMediaMethods();
        await renderPlayer();
        const player = requiredPlayer();
        const video = requiredVideo();
        let fullscreenRequests = 0;
        Object.defineProperty(player, "requestFullscreen", {
            configurable: true,
            value: () => {
                fullscreenRequests += 1;
                return Promise.resolve();
            },
        });
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
        await press(player, "f");
        expect(fullscreenRequests).toBe(1);
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

        await doubleClick(video);
        expect(document.querySelector("[role='alert']")?.textContent).toContain("fullscreen denied");
        expect(errors).toEqual([{ kind: "fullscreen", message: "fullscreen denied" }]);

        await act(async () => video.dispatchEvent(new Event("error")));
        expect(document.querySelector("[role='alert']")?.textContent).toContain("视频加载失败");
        expect(errors[1]).toEqual({ kind: "media", message: "视频加载失败" });
    });

    test("downloads the current intrinsic video frame as a PNG", async () => {
        installMediaMethods();
        installFrameSource();
        await renderPlayer();
        const video = requiredVideo();
        Object.defineProperty(video, "currentTime", { configurable: true, writable: true, value: 3.25 });
        const drawnFrames: Array<{ source: CanvasImageSource; width: number; height: number }> = [];
        const context = {
            drawImage(source: CanvasImageSource, _x: number, _y: number, width: number, height: number) {
                drawnFrames.push({ source, width, height });
            },
        } as unknown as CanvasRenderingContext2D;
        Object.defineProperty(canvasPrototype, "getContext", {
            configurable: true,
            value: () => context,
        });
        Object.defineProperty(canvasPrototype, "toBlob", {
            configurable: true,
            value(callback: BlobCallback) {
                callback(new Blob(["frame"], { type: "image/png" }));
            },
        });
        Object.defineProperty(URL, "createObjectURL", {
            configurable: true,
            value: (() => {
                const urls = ["blob:video-source", "blob:captured-frame"];
                return () => urls.shift() || "blob:unexpected";
            })(),
        });
        const revokedUrls: string[] = [];
        Object.defineProperty(URL, "revokeObjectURL", {
            configurable: true,
            value: (url: string) => revokedUrls.push(url),
        });
        const downloads: Array<{ href: string; filename: string }> = [];
        Object.defineProperty(anchorPrototype, "click", {
            configurable: true,
            value(this: HTMLAnchorElement) {
                downloads.push({ href: this.href, filename: this.download });
            },
        });

        await click(requiredButton("保存当前视频帧"));

        expect(drawnFrames).toHaveLength(1);
        expect(drawnFrames[0]?.source).toBeInstanceOf(window.HTMLVideoElement);
        expect(drawnFrames[0]).toMatchObject({ width: 1920, height: 1080 });
        expect(downloads).toEqual([{ href: "blob:captured-frame", filename: "video-frame-3250.png" }]);
        expect(revokedUrls).toEqual(["blob:video-source", "blob:captured-frame"]);
    });

    test("shows a recoverable error when the capture source cannot be read", async () => {
        installFrameSource({ fail: true });
        const errors: CanvasVideoPlaybackError[] = [];
        await renderPlayer((error) => errors.push(error));

        await click(requiredButton("保存当前视频帧"));

        expect(document.querySelector("[role='alert']")?.textContent).toContain("当前视频帧读取失败");
        expect(errors).toEqual([{ kind: "capture", message: "当前视频帧读取失败" }]);
        expect(requiredVideo()).not.toBeNull();
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

async function renderPlayer(onPlaybackError: (error: CanvasVideoPlaybackError) => void = () => undefined, onIntrinsicSize?: (size: { width: number; height: number }) => void) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(
            createElement(CanvasVideoPlayer, {
                nodeId: "video-node",
                src: "/videos/hero.mp4",
                captureSource: new Blob(["video-source"], { type: "video/mp4" }),
                title: "测试视频",
                preload: "metadata",
                onPlaybackError,
                onIntrinsicSize,
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

function installFrameSource(options: { fail?: boolean } = {}) {
    if (options.fail) {
        Object.defineProperty(URL, "createObjectURL", { configurable: true, value: () => "blob:unreadable-video" });
        Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: () => undefined });
    }
    Object.defineProperty(mediaPrototype, "load", {
        configurable: true,
        value(this: HTMLMediaElement) {
            if (!(this instanceof window.HTMLVideoElement)) return;
            if (options.fail) {
                queueMicrotask(() => this.dispatchEvent(new Event("error")));
                return;
            }
            let frameTime = 0;
            Object.defineProperty(this, "duration", { configurable: true, value: 5 });
            Object.defineProperty(this, "readyState", { configurable: true, value: 2 });
            Object.defineProperty(this, "videoWidth", { configurable: true, value: 1920 });
            Object.defineProperty(this, "videoHeight", { configurable: true, value: 1080 });
            Object.defineProperty(this, "currentTime", {
                configurable: true,
                get: () => frameTime,
                set: (value: number) => {
                    frameTime = value;
                    queueMicrotask(() => this.dispatchEvent(new Event("seeked")));
                },
            });
            queueMicrotask(() => this.dispatchEvent(new Event("loadeddata")));
        },
    });
}

function restoreProperty(target: object, name: PropertyKey, descriptor: PropertyDescriptor | undefined) {
    if (descriptor) Object.defineProperty(target, name, descriptor);
    else Reflect.deleteProperty(target, name);
}

async function click(element: HTMLElement) {
    await act(async () => element.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true })));
}

async function doubleClick(element: HTMLElement) {
    await act(async () => element.dispatchEvent(new MouseEvent("dblclick", { bubbles: true, cancelable: true })));
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

function requiredProgressShell() {
    const shell = document.querySelector<HTMLElement>("[data-canvas-video-progress-track]");
    if (!shell) throw new Error("缺少视频进度轨道");
    return shell;
}

function requiredOutput(selector: string) {
    const output = document.querySelector<HTMLOutputElement>(selector);
    if (!output) throw new Error(`缺少视频时间输出: ${selector}`);
    return output;
}
