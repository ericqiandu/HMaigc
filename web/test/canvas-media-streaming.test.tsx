import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";

import { CanvasMediaNodeContent } from "../src/components/canvas/canvas-media-node-content";
import { CanvasFrameNode } from "../src/components/canvas/canvas-frame-node";
import { CanvasNode } from "../src/components/canvas/canvas-node";
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
        const markup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Video, "video-resource"), theme: canvasThemes.dark, reduceMediaEffects: false, mediaVisibilityTier: "visible" }));

        expect(markup).toContain("<video");
        expect(markup).toContain('src="/api/resources/video-resource/file?direct=1"');
        expect(markup).toContain('preload="metadata"');
        expect(markup).toContain("data-canvas-video-player");
        expect(markup).toContain("data-canvas-video-controls");
        expect(markup).not.toContain('controls=""');
        expect(markup).not.toContain("加载并缓存视频");
    });

    test("远程音频立即使用鉴权资源地址播放，不等待整段 Blob 缓存", () => {
        const markup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Audio, "audio-resource"), theme: canvasThemes.dark, reduceMediaEffects: false, mediaVisibilityTier: "visible" }));

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

    test("播放失败只保留无凭据资源摘要和结构化原因", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        const node = mediaNode(CanvasNodeType.Video, "private-resource");
        node.metadata = {
            ...node.metadata,
            content: "https://viewer:secret@provider.example/private-resource.mp4?token=access-token-4821#frame",
            storageKey: undefined,
        };
        await renderMediaNode(node);
        await act(async () => {
            await new Promise<void>((resolve) => setTimeout(resolve, 0));
        });

        const video = document.querySelector("video");
        if (video) await act(async () => video.dispatchEvent(new Event("error")));

        const failure = document.querySelector<HTMLElement>("[data-canvas-media-error-kind]");
        expect(failure?.dataset.canvasMediaErrorKind).toBe("media");
        expect(failure?.dataset.canvasMediaErrorNode).toBe("video-node");
        expect(failure?.dataset.canvasMediaErrorResource).toBe("https://provider.example/private-resource.mp4");
        expect(failure?.dataset.canvasMediaErrorMessage).toBe("视频加载失败");
        expect(failure?.outerHTML).not.toContain("secret");
        expect(failure?.outerHTML).not.toContain("access-token-4821");
    });

    test("近区视频只渲染轻量占位且不挂载媒体元素", () => {
        const nearMarkup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Video, "near-video"), theme: canvasThemes.dark, reduceMediaEffects: true, mediaVisibilityTier: "near" }));
        const visibleMarkup = renderToStaticMarkup(createElement(CanvasMediaNodeContent, { node: mediaNode(CanvasNodeType.Video, "visible-video"), theme: canvasThemes.dark, reduceMediaEffects: true, mediaVisibilityTier: "visible", onVideoIntrinsicSize: () => undefined }));

        expect(nearMarkup).not.toContain("<video");
        expect(nearMarkup).toContain("data-canvas-deferred-media");
        expect(nearMarkup).not.toContain("data-canvas-video-controls");
        expect(visibleMarkup).toContain("data-canvas-video-player");
        expect(visibleMarkup).toContain('preload="metadata"');
    });

    test("近区图片继续使用浏览器懒加载和异步解码", () => {
        const imageNode: CanvasNodeData = {
            id: "near-image",
            type: CanvasNodeType.Image,
            title: "近区图片",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: { content: "https://provider.example/near-image.webp" },
        };
        const markup = renderToStaticMarkup(
            createElement(CanvasNode, {
                data: imageNode,
                scale: 1,
                isSelected: false,
                isRelated: false,
                isFocusRelated: false,
                isConnectionTarget: false,
                isConnecting: false,
                showImageInfo: false,
                mediaVisibilityTier: "near",
                onMouseDown: () => undefined,
                onHoverStart: () => undefined,
                onHoverEnd: () => undefined,
                onConnectStart: () => undefined,
                onResizeCommit: () => undefined,
                onContentChange: () => undefined,
                onContextMenu: () => undefined,
            }),
        );

        expect(markup).toContain('loading="lazy"');
        expect(markup).toContain('decoding="async"');
    });

    test("近区折叠背板不通过缩略预览重新挂载视频", () => {
        const frame: CanvasNodeData = {
            id: "near-frame",
            type: CanvasNodeType.Frame,
            title: "近区背板",
            position: { x: 0, y: 0 },
            width: 800,
            height: 500,
            metadata: { frame: { collapsed: true } },
        };
        const child = { ...mediaNode(CanvasNodeType.Video, "frame-video"), parentId: frame.id };
        const markup = renderToStaticMarkup(
            createElement(CanvasFrameNode, {
                data: frame,
                childNodes: [child],
                scale: 1,
                isSelected: false,
                isDropTarget: false,
                mediaVisibilityTier: "near",
                readOnly: true,
                onMouseDown: () => undefined,
                onResizeCommit: () => undefined,
                onToggleCollapsed: () => undefined,
                onTitleChange: () => undefined,
                onContextMenu: () => undefined,
            }),
        );

        expect(markup).not.toContain("<video");
        expect(markup).toContain('data-canvas-deferred-media="video"');
    });
});

async function renderMediaNode(node: CanvasNodeData) {
    await act(async () => {
        root?.render(createElement(CanvasMediaNodeContent, { node, theme: canvasThemes.dark, reduceMediaEffects: false, mediaVisibilityTier: "visible" }));
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
