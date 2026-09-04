import "./setup-happy-dom";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, useCallback, useLayoutEffect, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasNode } from "../src/components/canvas/canvas-node";
import { useCanvasNodeEditor } from "../src/pages/canvas/use-canvas-node-editor";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

let root: Root | null = null;
let committedNodeChanges = 0;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    committedNodeChanges = 0;
    document.body.replaceChildren();
});

describe("video intrinsic layout", () => {
    test("fits the node height to the real video ratio and persists intrinsic dimensions", async () => {
        await renderHarness();
        const video = requiredVideo();
        Object.defineProperty(video, "videoWidth", { configurable: true, value: 864 });
        Object.defineProperty(video, "videoHeight", { configurable: true, value: 496 });

        await act(async () => video.dispatchEvent(new Event("loadedmetadata")));

        expect(Number(requiredOutput("[data-video-node-width]").textContent)).toBe(1_280);
        expect(Number(requiredOutput("[data-video-node-height]").textContent)).toBeCloseTo(734.8148148148148, 8);
        expect(requiredOutput("[data-video-intrinsic-size]").textContent).toBe("864x496");

        await act(async () => video.dispatchEvent(new Event("loadedmetadata")));
        expect(committedNodeChanges).toBe(1);
    });
});

function VideoLayoutHarness() {
    const [nodes, setNodes] = useState<CanvasNodeData[]>([videoNode()]);
    const nodesRef = useRef(nodes);
    const trackedSetNodes = useCallback<Dispatch<SetStateAction<CanvasNodeData[]>>>((action) => {
        setNodes((current) => {
            const next = typeof action === "function" ? action(current) : action;
            if (next !== current) committedNodeChanges += 1;
            return next;
        });
    }, []);
    const [, setSelectedNodeIds] = useState<Set<string>>(new Set());
    const [, setSelectedConnectionId] = useState<string | null>(null);
    const [, setDialogNodeId] = useState<string | null>(null);
    const [, setToolbarNodeId] = useState<string | null>(null);
    const [, setHoveredNodeId] = useState<string | null>(null);
    useLayoutEffect(() => {
        nodesRef.current = nodes;
    }, [nodes]);

    const { handleNodeResizeCommit, handleNodeContentChange, handleVideoIntrinsicSize } = useCanvasNodeEditor({
        canvasId: "canvas-video-layout-test",
        nodesRef,
        setNodes: trackedSetNodes,
        setSelectedNodeIds,
        setSelectedConnectionId,
        setDialogNodeId,
        setToolbarNodeId,
        setHoveredNodeId,
    });
    const node = nodes[0];
    if (!node) throw new Error("视频节点测试数据缺失");

    return createElement(
        "section",
        { className: "canvas-video-layout-test" },
        createElement(CanvasNode, {
            data: node,
            scale: 1,
            isSelected: false,
            isRelated: false,
            isFocusRelated: false,
            isConnectionTarget: false,
            isConnecting: false,
            showImageInfo: false,
            mediaVisibilityTier: "visible",
            onMouseDown: () => undefined,
            onHoverStart: () => undefined,
            onHoverEnd: () => undefined,
            onConnectStart: () => undefined,
            onResizeCommit: handleNodeResizeCommit,
            onContentChange: handleNodeContentChange,
            onVideoIntrinsicSize: handleVideoIntrinsicSize,
            onContextMenu: () => undefined,
        }),
        createElement("output", { className: "canvas-video-layout-test-width", "data-video-node-width": true }, node.width),
        createElement("output", { className: "canvas-video-layout-test-height", "data-video-node-height": true }, node.height),
        createElement("output", { className: "canvas-video-layout-test-intrinsic", "data-video-intrinsic-size": true }, `${node.metadata?.naturalWidth || 0}x${node.metadata?.naturalHeight || 0}`),
    );
}

async function renderHarness() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    await act(async () => {
        root?.render(createElement(QueryClientProvider, { client: queryClient }, createElement(App, null, createElement(VideoLayoutHarness))));
    });
}

function videoNode(): CanvasNodeData {
    return {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "真实比例视频",
        position: { x: 120, y: 80 },
        width: 1_280,
        height: 720,
        metadata: { content: "/videos/hero.mp4", status: "success" },
    };
}

function requiredVideo() {
    const video = document.querySelector("video");
    if (!video) throw new Error("缺少视频播放器");
    return video;
}

function requiredOutput(selector: string) {
    const output = document.querySelector<HTMLOutputElement>(selector);
    if (!output) throw new Error(`缺少视频布局输出：${selector}`);
    return output;
}
