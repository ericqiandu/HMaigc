import { App } from "antd";
import { createRoot } from "react-dom/client";
import { useCallback, useRef, useState } from "react";
import "antd/dist/reset.css";

import { CanvasNodeHoverToolbar } from "@/components/canvas/canvas-node-hover-toolbar";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import "@/styles/globals.css";
import "@/styles/canvas-chrome.css";

const NODE_WIDTH = 220;
const NODE_HEIGHT = 140;

const nodes: CanvasNodeData[] = [
    { id: "fixture-image", type: CanvasNodeType.Image, title: "成品图片", position: { x: 60, y: 180 }, width: NODE_WIDTH, height: NODE_HEIGHT, metadata: { content: "/showcase/banner-1.jpg", prompt: "fixture image", status: "success" } },
    { id: "fixture-video", type: CanvasNodeType.Video, title: "成品视频", position: { x: 320, y: 180 }, width: NODE_WIDTH, height: NODE_HEIGHT, metadata: { content: "/videos/hero.mp4", status: "success" } },
    { id: "fixture-audio", type: CanvasNodeType.Audio, title: "成品音频", position: { x: 580, y: 180 }, width: NODE_WIDTH, height: NODE_HEIGHT, metadata: { content: "/fixture/audio.mp3", status: "success" } },
    { id: "fixture-text", type: CanvasNodeType.Text, title: "文本", position: { x: 840, y: 180 }, width: NODE_WIDTH, height: NODE_HEIGHT, metadata: { content: "fixture text", status: "success" } },
    { id: "fixture-config", type: CanvasNodeType.Config, title: "生成配置", position: { x: 1100, y: 180 }, width: NODE_WIDTH, height: NODE_HEIGHT, metadata: { status: "idle" } },
];

function ToolbarFixture() {
    const containerRef = useRef<HTMLDivElement>(null);
    const leaveTimerRef = useRef<number | null>(null);
    const [hoveredNode, setHoveredNode] = useState<CanvasNodeData | null>(null);
    const [lastAction, setLastAction] = useState("none");
    const extractingVideoFrame = new URLSearchParams(window.location.search).get("busy") === "1";

    const cancelLeave = useCallback(() => {
        if (leaveTimerRef.current !== null) window.clearTimeout(leaveTimerRef.current);
        leaveTimerRef.current = null;
    }, []);
    const keepNode = useCallback((nodeId: string) => {
        cancelLeave();
        setHoveredNode(nodes.find((node) => node.id === nodeId) ?? null);
    }, [cancelLeave]);
    const leaveNode = useCallback(() => {
        cancelLeave();
        leaveTimerRef.current = window.setTimeout(() => setHoveredNode(null), 120);
    }, [cancelLeave]);
    const record = useCallback((action: string) => (node: CanvasNodeData) => {
        setLastAction(`${node.id}:${action}`);
    }, []);

    return (
        <main className="canvas-toolbar-fixture relative h-screen min-w-[1380px] overflow-hidden bg-neutral-950 text-white">
            <output data-testid="last-action" className="absolute left-4 top-4 z-10">{lastAction}</output>
            <div ref={containerRef} className="canvas-toolbar-fixture-stage relative size-full" data-testid="toolbar-stage">
                {nodes.map((node) => (
                    <div
                        key={node.id}
                        data-node-id={node.id}
                        data-testid={node.id}
                        className="node-element absolute grid place-items-center rounded-xl bg-neutral-800 text-sm"
                        style={{ left: node.position.x, top: node.position.y, width: node.width, height: node.height }}
                        onMouseEnter={() => keepNode(node.id)}
                        onMouseLeave={leaveNode}
                    >
                        {node.title}
                    </div>
                ))}
                <CanvasNodeHoverToolbar
                    node={hoveredNode}
                    viewport={{ x: 0, y: 0, k: 1 }}
                    containerRef={containerRef}
                    onKeep={keepNode}
                    onLeave={leaveNode}
                    onInfo={record("info")}
                    onEditText={record("editText")}
                    onDecreaseFont={record("decreaseFont")}
                    onIncreaseFont={record("increaseFont")}
                    onToggleDialog={record("toggleDialog")}
                    onAnnotate={record("annotation")}
                    onGenerateImage={record("generateImage")}
                    onUpload={record("upload")}
                    onDownload={record("download")}
                    onSaveAsset={record("saveAsset")}
                    onMaskEdit={record("maskEdit")}
                    onEmotion={record("emotion")}
                    onCrop={record("crop")}
                    onSplit={record("split")}
                    onUpscale={record("upscale")}
                    onSuperResolve={record("superResolve")}
                    onAngle={record("angle")}
                    onViewImage={record("view")}
                    onExtractVideoLastFrame={record("extractLastFrame")}
                    extractingVideoFrame={extractingVideoFrame}
                    onReversePrompt={record("reversePrompt")}
                    onRetry={record("retry")}
                    onToggleFreeResize={record("resize")}
                    onToggleLocked={record("toggleLocked")}
                    onDelete={record("delete")}
                    workspaceMode="professional"
                />
            </div>
        </main>
    );
}

const root = document.getElementById("canvas-toolbar-fixture-root");
if (!root) throw new Error("画布工具栏测试根节点缺失");
createRoot(root).render(<App><ToolbarFixture /></App>);
