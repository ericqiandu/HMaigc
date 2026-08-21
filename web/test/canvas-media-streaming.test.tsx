import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";

import { CanvasMediaNodeContent } from "../src/components/canvas/canvas-media-node-content";
import { canvasThemes } from "../src/lib/canvas-theme";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("画布媒体流式预览", () => {
    test("远程视频立即使用鉴权资源地址播放，不等待整段 Blob 缓存", () => {
        const markup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Video, "video-resource"), theme: canvasThemes.dark, reduceMediaEffects: false }));

        expect(markup).toContain("<video");
        expect(markup).toContain('src="/api/resources/video-resource/file?direct=1"');
        expect(markup).toContain('preload="metadata"');
        expect(markup).not.toContain("加载并缓存视频");
    });

    test("远程音频立即使用鉴权资源地址播放，不等待整段 Blob 缓存", () => {
        const markup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Audio, "audio-resource"), theme: canvasThemes.dark, reduceMediaEffects: false }));

        expect(markup).toContain("<audio");
        expect(markup).toContain('src="/api/resources/audio-resource/file?direct=1"');
        expect(markup).toContain('preload="metadata"');
        expect(markup).not.toContain("加载并缓存音频");
    });

    test("节点切换到新资源后清除旧播放错误并加载新地址", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await renderMediaNode(mediaNode(CanvasNodeType.Video, "expired-resource"));
        const expiredVideo = requiredVideo();
        await act(async () => expiredVideo.dispatchEvent(new Event("error")));
        expect(document.body.textContent).toContain("媒体加载失败");

        await renderMediaNode(mediaNode(CanvasNodeType.Video, "replacement-resource"));

        expect(document.body.textContent).not.toContain("媒体加载失败");
        expect(requiredVideo().getAttribute("src")).toBe("/api/resources/replacement-resource/file?direct=1");
    });
});

async function renderMediaNode(node: CanvasNodeData) {
    await act(async () => {
        root?.render(createElement(CanvasMediaNodeContent, { node, theme: canvasThemes.dark, reduceMediaEffects: false }));
    });
}

function requiredVideo() {
    const video = document.querySelector("video");
    if (!video) throw new Error("缺少视频播放器");
    return video;
}

function mediaNode(type: CanvasNodeType.Video | CanvasNodeType.Audio, resourceId: string): CanvasNodeData {
    return {
        id: `${type}-node`,
        type,
        title: type === CanvasNodeType.Video ? "视频" : "音频",
        position: { x: 0, y: 0 },
        width: 420,
        height: 236,
        metadata: {
            content: `https://provider.example/${resourceId}.mp4`,
            storageKey: `resource:${resourceId}`,
        },
    };
}
