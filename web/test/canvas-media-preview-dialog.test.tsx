import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasProjectStatusDialogs } from "../src/pages/canvas/canvas-project-status-dialogs";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("画布视频预览弹窗", () => {
    test("账号资源使用稳定播放地址而不是节点中的旧内容地址", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(CanvasProjectStatusDialogs, {
                theme: { node: { stroke: "#333", panel: "#222", muted: "#999", fill: "#111" } },
                task: null,
                taskLogs: [],
                taskLoading: false,
                onCloseTask: () => undefined,
                superResolveNode: null,
                onCloseSuperResolve: () => undefined,
                previewNode: videoNode(),
                onClosePreview: () => undefined,
                clearConfirmOpen: false,
                onCancelClear: () => undefined,
                onConfirmClear: () => undefined,
            }));
        });
        await settleModalMotion();

        expect(requiredPreviewVideo().getAttribute("src")).toBe("/api/resources/video-resource/file?direct=1");
    });

    test("播放失败后显示明确错误并允许重新加载", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(CanvasProjectStatusDialogs, {
                theme: { node: { stroke: "#333", panel: "#222", muted: "#999", fill: "#111" } },
                task: null,
                taskLogs: [],
                taskLoading: false,
                onCloseTask: () => undefined,
                superResolveNode: null,
                onCloseSuperResolve: () => undefined,
                previewNode: videoNode(),
                onClosePreview: () => undefined,
                clearConfirmOpen: false,
                onCancelClear: () => undefined,
                onConfirmClear: () => undefined,
            }));
        });
        await settleModalMotion();

        const failedVideo = requiredPreviewVideo();
        await act(async () => failedVideo.dispatchEvent(new Event("error")));

        expect(document.body.textContent).toContain("视频加载失败");
        const retryButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("重新加载"));
        if (!retryButton) throw new Error("缺少重新加载按钮");

        await act(async () => retryButton.click());

        expect(requiredPreviewVideo()).not.toBe(failedVideo);
    });

    test("图片加载失败后显示明确错误并允许重新加载", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(CanvasProjectStatusDialogs, {
                theme: { node: { stroke: "#333", panel: "#222", muted: "#999", fill: "#111" } },
                task: null,
                taskLogs: [],
                taskLoading: false,
                onCloseTask: () => undefined,
                superResolveNode: null,
                onCloseSuperResolve: () => undefined,
                previewNode: imageNode(),
                onClosePreview: () => undefined,
                clearConfirmOpen: false,
                onCancelClear: () => undefined,
                onConfirmClear: () => undefined,
            }));
        });
        await settleModalMotion();

        const failedImage = requiredPreviewImage();
        await act(async () => failedImage.dispatchEvent(new Event("error")));

        expect(document.body.textContent).toContain("图片加载失败");
        const retryButton = Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.includes("重新加载"));
        if (!retryButton) throw new Error("缺少重新加载按钮");

        await act(async () => retryButton.click());

        expect(requiredPreviewImage()).not.toBe(failedImage);
    });
});

function requiredPreviewVideo() {
    const video = document.querySelector("video");
    if (!video) throw new Error("缺少视频预览播放器");
    return video;
}

function requiredPreviewImage() {
    const image = document.querySelector<HTMLImageElement>(".canvas-media-preview-image");
    if (!image) throw new Error("缺少图片预览元素");
    return image;
}

async function settleModalMotion() {
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)));
}

function videoNode(): CanvasNodeData {
    return {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "视频",
        position: { x: 0, y: 0 },
        width: 420,
        height: 236,
        metadata: {
            content: "https://expired-provider.example/video.mp4",
            storageKey: "resource:video-resource",
        },
    };
}

function imageNode(): CanvasNodeData {
    return {
        id: "image-node",
        type: CanvasNodeType.Image,
        title: "图片",
        position: { x: 0, y: 0 },
        width: 420,
        height: 420,
        metadata: {
            content: "https://expired-provider.example/image.png",
            storageKey: "resource:image-resource",
        },
    };
}
