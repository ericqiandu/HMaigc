import React, { useEffect, useMemo, useRef } from "react";
import { createRoot } from "react-dom/client";
import "antd/dist/reset.css";

import { CanvasNode } from "@/components/canvas/canvas-node";
import { applyCanvasLiveNodeDrag, clearCanvasLiveNodeDrag, resolveCanvasLiveNodeDragTargets } from "@/lib/canvas/canvas-drag-performance";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import "@/styles/globals.css";
import "@/styles/canvas-chrome.css";

type LongTaskSample = { durationMs: number };
type CanvasDragSamples = { frameIntervalsMs: number[]; longTasks: LongTaskSample[] };

declare global {
    interface Window {
        __CANVAS_BENCHMARK_STARTED_AT__?: number;
        __CANVAS_BENCHMARK_READY__?: { nodeCount: number; mountDurationMs: number; domNodeCount: number };
        runCanvasDragBenchmark?: () => Promise<CanvasDragSamples>;
    }
}

const SUPPORTED_NODE_COUNTS = new Set([500, 1_000]);
const LIVE_DRAG_OFFSET = Object.freeze({ x: 0, y: 0 });
const noOp = () => undefined;

function readNodeCount() {
    const requested = Number(new URLSearchParams(window.location.search).get("nodes"));
    if (!SUPPORTED_NODE_COUNTS.has(requested)) throw new Error(`画布性能基准仅支持 500 或 1000 节点，收到: ${requested}`);
    return requested;
}

function createBenchmarkNodes(nodeCount: number): CanvasNodeData[] {
    const columns = 40;
    return Array.from({ length: nodeCount }, (_, index) => ({
        id: `benchmark-text-${index}`,
        type: CanvasNodeType.Text,
        title: `文本节点 ${index + 1}`,
        position: { x: (index % columns) * 280, y: Math.floor(index / columns) * 200 },
        width: 240,
        height: 160,
        metadata: { content: `商业级大画布性能样本 ${index + 1}`, status: "idle" },
        createdAt: "2026-01-01T00:00:00.000Z",
    }));
}

function observeLongTasks(samples: LongTaskSample[]) {
    if (!("PerformanceObserver" in window)) return null;
    const supportedTypes = PerformanceObserver.supportedEntryTypes;
    if (!supportedTypes.includes("longtask")) return null;
    const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) samples.push({ durationMs: entry.duration });
    });
    observer.observe({ type: "longtask", buffered: false });
    return observer;
}

function CanvasPerformanceFixture() {
    const nodeCount = useMemo(readNodeCount, []);
    const nodes = useMemo(() => createBenchmarkNodes(nodeCount), [nodeCount]);
    const worldRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        let cancelled = false;
        let firstFrame = 0;
        let secondFrame = 0;
        firstFrame = window.requestAnimationFrame(() => {
            secondFrame = window.requestAnimationFrame(() => {
                if (cancelled) return;
                const startedAt = window.__CANVAS_BENCHMARK_STARTED_AT__;
                if (!Number.isFinite(startedAt)) throw new Error("画布性能基准缺少启动时间");
                window.__CANVAS_BENCHMARK_READY__ = {
                    nodeCount,
                    mountDurationMs: performance.now() - Number(startedAt),
                    domNodeCount: document.getElementsByTagName("*").length,
                };
                document.documentElement.dataset.canvasBenchmarkReady = "true";
            });
        });
        return () => {
            cancelled = true;
            window.cancelAnimationFrame(firstFrame);
            window.cancelAnimationFrame(secondFrame);
        };
    }, [nodeCount]);

    useEffect(() => {
        window.runCanvasDragBenchmark = async () => {
            const surface = worldRef.current;
            if (!surface) throw new Error("画布性能基准找不到画布世界层");
            const targets = resolveCanvasLiveNodeDragTargets(surface, new Set(["benchmark-text-0"]));
            if (targets.length !== 1) throw new Error(`画布性能基准解析到 ${targets.length} 个拖拽节点，预期 1 个`);
            const frameIntervalsMs: number[] = [];
            const longTasks: LongTaskSample[] = [];
            const observer = observeLongTasks(longTasks);
            worldRef.current?.setAttribute("data-canvas-viewport-interacting", "true");

            try {
                await new Promise<void>((resolve) => {
                    let frame = 0;
                    let previousAt = performance.now();
                    const step = (now: number) => {
                        frameIntervalsMs.push(now - previousAt);
                        previousAt = now;
                        applyCanvasLiveNodeDrag(targets, { x: frame * 1.25, y: Math.sin(frame / 12) * 24 });
                        frame += 1;
                        if (frame >= 120) {
                            resolve();
                            return;
                        }
                        window.requestAnimationFrame(step);
                    };
                    window.requestAnimationFrame(step);
                });
            } finally {
                for (const entry of observer?.takeRecords() ?? []) longTasks.push({ durationMs: entry.duration });
                observer?.disconnect();
                clearCanvasLiveNodeDrag(targets);
                worldRef.current?.removeAttribute("data-canvas-viewport-interacting");
            }
            return { frameIntervalsMs, longTasks };
        };
        return () => {
            delete window.runCanvasDragBenchmark;
        };
    }, []);

    return (
        <main className="canvas-performance-page">
            <output className="canvas-performance-status" aria-live="polite">
                HMaigc 真实节点基准 · {nodeCount} 节点
            </output>
            <div ref={worldRef} className="canvas-performance-world">
                {nodes.map((node, index) => (
                    <CanvasNode
                        key={node.id}
                        data={node}
                        dragOffset={index === 0 ? LIVE_DRAG_OFFSET : undefined}
                        scale={0.18}
                        isSelected={index === 0}
                        isRelated={false}
                        isFocusRelated={false}
                        isConnectionTarget={false}
                        isConnecting={false}
                        showImageInfo={false}
                        reduceMediaEffects
                        readOnly
                        onMouseDown={noOp}
                        onHoverStart={noOp}
                        onHoverEnd={noOp}
                        onConnectStart={noOp}
                        onResize={noOp}
                        onContentChange={noOp}
                        onContextMenu={noOp}
                    />
                ))}
            </div>
        </main>
    );
}

const root = document.getElementById("canvas-performance-root");
if (!root) throw new Error("画布性能基准缺少根节点");
createRoot(root).render(<CanvasPerformanceFixture />);
