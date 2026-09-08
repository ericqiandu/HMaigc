import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "antd/dist/reset.css";

import { ConnectionPath } from "@/components/canvas/canvas-connections";
import { CanvasNode } from "@/components/canvas/canvas-node";
import {
    applyCanvasLiveConnectionDrag,
    applyCanvasLiveNodeDrag,
    applyCanvasLiveResize,
    clearCanvasLiveConnectionDrag,
    clearCanvasLiveNodeDrag,
    clearCanvasLiveResize,
    resolveCanvasLiveConnectionDragTargets,
    resolveCanvasLiveNodeDragTargets,
} from "@/lib/canvas/canvas-interaction-runtime";
import { createCanvasRenderIndex } from "@/lib/canvas/canvas-render-index";
import { fitVideoNodeToIntrinsicSize } from "@/lib/canvas/canvas-video-geometry";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "@/types/canvas";
import "@/styles/globals.css";
import "@/styles/canvas-chrome.css";

type CanvasBenchmarkScenario = "pan" | "zoom" | "drag-one" | "drag-20" | "resize" | "video-controls";
type LongTaskSample = { durationMs: number };
type HotPathViolations = { projectSerialization: number; networkRequest: number; fullGraphScan: number };
type CanvasGestureSamples = {
    scenario: CanvasBenchmarkScenario;
    frameIntervalsMs: number[];
    longTasks: LongTaskSample[];
    longTaskObserverSupported: boolean;
    mountedNodeCount: number;
    mountedEdgeCount: number;
    mountedVideoCount: number;
    offscreenMountedVideoCount: number;
    hotPathViolations: HotPathViolations;
};
type CanvasBenchmarkReady = {
    nodeCount: number;
    mediaCount: number;
    connectionCount: number;
    mountDurationMs: number;
    domNodeCount: number;
};
type BenchmarkFixtureCount = { mediaCount: number; connectionCount: number };

declare global {
    interface Window {
        __CANVAS_BENCHMARK_STARTED_AT__?: number;
        __CANVAS_BENCHMARK_READY__?: CanvasBenchmarkReady;
        __CANVAS_BENCHMARK_RECORD_FULL_GRAPH_SCAN__?: () => void;
        runCanvasBenchmarkScenario?: (scenario: CanvasBenchmarkScenario) => Promise<CanvasGestureSamples>;
    }
}

const BENCHMARK_SCENARIOS = new Set<CanvasBenchmarkScenario>(["pan", "zoom", "drag-one", "drag-20", "resize", "video-controls"]);
const BENCHMARK_FIXTURE_COUNTS: Readonly<Record<number, BenchmarkFixtureCount>> = Object.freeze({
    50: Object.freeze({ mediaCount: 50, connectionCount: 300 }),
    200: Object.freeze({ mediaCount: 100, connectionCount: 1_000 }),
    500: Object.freeze({ mediaCount: 200, connectionCount: 3_000 }),
});
const DEFAULT_CANVAS_SCALE = 0.55;
const MEDIA_CONTRACT_SCALES = new Set([0.18, 0.25, 0.55, 1, 2]);
const CANVAS_TOP = 80;
const CANVAS_LEFT = 40;
const NEAR_PADDING = 600;
const SAMPLE_FRAME_COUNT = 120;
const LIVE_DRAG_OFFSET = Object.freeze({ x: 0, y: 0 });
const FIXED_TIMESTAMP = "2026-01-01T00:00:00.000Z";
const IMAGE_ASSET_URL = "/showcase/banner-1.jpg";
const VIDEO_ASSET_URL = "/videos/hero.mp4";
const noOp = () => undefined;

const mediaContractNode: CanvasNodeData = {
    id: "benchmark-video-contract",
    type: CanvasNodeType.Video,
    title: "视频渲染契约",
    position: { x: 320, y: 180 },
    width: 1280,
    height: 640,
    metadata: { content: VIDEO_ASSET_URL, status: "success" },
};

function readScenario() {
    return new URLSearchParams(window.location.search).get("scenario");
}

function readNodeCount() {
    const requested = Number(new URLSearchParams(window.location.search).get("nodes"));
    if (!BENCHMARK_FIXTURE_COUNTS[requested]) throw new Error(`画布性能基准仅支持 50、200 或 500 节点，收到: ${requested}`);
    return requested;
}

function readCanvasScale(isMediaContract: boolean) {
    if (!isMediaContract) return DEFAULT_CANVAS_SCALE;
    const requested = Number(new URLSearchParams(window.location.search).get("scale") || DEFAULT_CANVAS_SCALE);
    if (!MEDIA_CONTRACT_SCALES.has(requested)) throw new Error(`媒体渲染契约不支持缩放值: ${requested}`);
    return requested;
}

function createBenchmarkNodes(nodeCount: number, mediaCount: number): CanvasNodeData[] {
    const columns = 8;
    return Array.from({ length: nodeCount }, (_, index) => {
        const isMedia = index < mediaCount;
        const isVideo = isMedia && index % 12 === 0;
        const type = isVideo ? CanvasNodeType.Video : isMedia ? CanvasNodeType.Image : CanvasNodeType.Text;
        return {
            id: `benchmark-node-${index}`,
            type,
            title: `${isVideo ? "视频" : isMedia ? "图片" : "文本"}节点 ${index + 1}`,
            position: { x: (index % columns) * 420, y: Math.floor(index / columns) * 320 },
            width: 360,
            height: 240,
            metadata: isVideo
                ? { content: VIDEO_ASSET_URL, status: "success", createdAt: FIXED_TIMESTAMP }
                : isMedia
                  ? { content: IMAGE_ASSET_URL, status: "success", createdAt: FIXED_TIMESTAMP }
                  : { content: `商业级大画布性能样本 ${index + 1}`, status: "idle", createdAt: FIXED_TIMESTAMP },
        };
    });
}

function createBenchmarkConnections(nodeCount: number, connectionCount: number): CanvasConnection[] {
    return Array.from({ length: connectionCount }, (_, index) => {
        const fromIndex = index % nodeCount;
        const hop = Math.floor(index / nodeCount) + 1;
        return {
            id: `benchmark-connection-${index}`,
            fromNodeId: `benchmark-node-${fromIndex}`,
            toNodeId: `benchmark-node-${(fromIndex + hop) % nodeCount}`,
        };
    });
}

function observeLongTasks(samples: LongTaskSample[]) {
    if (!("PerformanceObserver" in window) || !PerformanceObserver.supportedEntryTypes.includes("longtask")) return null;
    const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) samples.push({ durationMs: entry.duration });
    });
    observer.observe({ type: "longtask", buffered: false });
    return observer;
}

function installHotPathGuards(violations: HotPathViolations) {
    const originalFetch = window.fetch;
    const originalStorageSetItem = Storage.prototype.setItem;
    window.fetch = ((...input: Parameters<typeof window.fetch>) => {
        violations.networkRequest += 1;
        return originalFetch(...input);
    }) satisfies typeof window.fetch;
    Storage.prototype.setItem = function benchmarkStorageSetItem(key: string, value: string) {
        violations.projectSerialization += 1;
        return originalStorageSetItem.call(this, key, value);
    };
    window.__CANVAS_BENCHMARK_RECORD_FULL_GRAPH_SCAN__ = () => {
        violations.fullGraphScan += 1;
    };
    return () => {
        window.fetch = originalFetch;
        Storage.prototype.setItem = originalStorageSetItem;
        delete window.__CANVAS_BENCHMARK_RECORD_FULL_GRAPH_SCAN__;
    };
}

function collectMountFacts(surface: HTMLElement) {
    const videos = Array.from(surface.querySelectorAll<HTMLVideoElement>("video"));
    return {
        mountedNodeCount: surface.querySelectorAll("[data-node-id]").length,
        mountedEdgeCount: surface.querySelectorAll("[data-canvas-connection-id]").length,
        mountedVideoCount: videos.length,
        offscreenMountedVideoCount: videos.filter((video) => !intersectsViewport(video.getBoundingClientRect())).length,
    };
}

function intersectsViewport(rect: DOMRect) {
    return rect.right > 0 && rect.bottom > 0 && rect.left < window.innerWidth && rect.top < window.innerHeight;
}

async function sampleGestureFrames(applyFrame: (frame: number) => void) {
    const frameIntervalsMs: number[] = [];
    await new Promise<void>((resolve) => {
        window.requestAnimationFrame((startedAt) => {
            let frame = 0;
            let previousAt = startedAt;
            const step = (now: number) => {
                frameIntervalsMs.push(now - previousAt);
                previousAt = now;
                applyFrame(frame);
                frame += 1;
                if (frame >= SAMPLE_FRAME_COUNT) resolve();
                else window.requestAnimationFrame(step);
            };
            window.requestAnimationFrame(step);
        });
    });
    return frameIntervalsMs;
}

function CanvasPerformanceFixture() {
    const requestedScenario = useMemo(readScenario, []);
    const isMediaContract = requestedScenario === "media-contract";
    const canvasScale = useMemo(() => readCanvasScale(isMediaContract), [isMediaContract]);
    const nodeCount = useMemo(() => (isMediaContract ? 1 : readNodeCount()), [isMediaContract]);
    const fixtureCount = isMediaContract ? { mediaCount: 1, connectionCount: 0 } : BENCHMARK_FIXTURE_COUNTS[nodeCount];
    const [videoContractNode, setVideoContractNode] = useState(mediaContractNode);
    const nodes = useMemo(() => (isMediaContract ? [videoContractNode] : createBenchmarkNodes(nodeCount, fixtureCount.mediaCount)), [fixtureCount.mediaCount, isMediaContract, nodeCount, videoContractNode]);
    const connections = useMemo(() => (isMediaContract ? [] : createBenchmarkConnections(nodeCount, fixtureCount.connectionCount)), [fixtureCount.connectionCount, isMediaContract, nodeCount]);
    const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
    const connectionById = useMemo(() => new Map(connections.map((connection) => [connection.id, connection])), [connections]);
    const renderQuery = useMemo(() => {
        if (isMediaContract) return { nodeIds: [mediaContractNode.id], mediaTierByNodeId: new Map([[mediaContractNode.id, "visible" as const]]), connectionIds: [] };
        const index = createCanvasRenderIndex(nodes, connections, new Set());
        return index.query({
            visible: { left: 0, top: 0, right: (window.innerWidth - CANVAS_LEFT) / canvasScale, bottom: (window.innerHeight - CANVAS_TOP) / canvasScale },
            nearPadding: NEAR_PADDING,
        });
    }, [canvasScale, connections, isMediaContract, nodes]);
    const renderedNodes = useMemo(() => renderQuery.nodeIds.map((id) => nodeById.get(id)).filter((node): node is CanvasNodeData => Boolean(node)), [nodeById, renderQuery.nodeIds]);
    const renderedConnections = useMemo(
        () =>
            renderQuery.connectionIds.flatMap((id) => {
                const connection = connectionById.get(id);
                const from = connection ? nodeById.get(connection.fromNodeId) : undefined;
                const to = connection ? nodeById.get(connection.toNodeId) : undefined;
                return connection && from && to ? [{ connection, from, to }] : [];
            }),
        [connectionById, nodeById, renderQuery.connectionIds],
    );
    const worldRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        let cancelled = false;
        let secondFrame: number | null = null;
        const firstFrame = window.requestAnimationFrame(() => {
            secondFrame = window.requestAnimationFrame(() => {
                if (cancelled) return;
                const startedAt = window.__CANVAS_BENCHMARK_STARTED_AT__;
                if (!Number.isFinite(startedAt)) throw new Error("画布性能基准缺少启动时间");
                window.__CANVAS_BENCHMARK_READY__ = {
                    nodeCount,
                    mediaCount: fixtureCount.mediaCount,
                    connectionCount: fixtureCount.connectionCount,
                    mountDurationMs: performance.now() - Number(startedAt),
                    domNodeCount: document.getElementsByTagName("*").length,
                };
                document.documentElement.dataset.canvasBenchmarkReady = "true";
            });
        });
        return () => {
            cancelled = true;
            window.cancelAnimationFrame(firstFrame);
            if (secondFrame !== null) window.cancelAnimationFrame(secondFrame);
        };
    }, [fixtureCount.connectionCount, fixtureCount.mediaCount, nodeCount]);

    useEffect(() => {
        if (isMediaContract) return;
        window.runCanvasBenchmarkScenario = async (scenario) => {
            if (!BENCHMARK_SCENARIOS.has(scenario)) throw new Error(`不支持的画布性能场景: ${scenario}`);
            const surface = worldRef.current;
            if (!surface) throw new Error("画布性能基准找不到画布世界层");
            const isDragScenario = scenario === "drag-one" || scenario === "drag-20";
            const draggedIds = new Set(Array.from({ length: scenario === "drag-20" ? 20 : 1 }, (_, index) => `benchmark-node-${index}`));
            const nodeTargets = isDragScenario ? resolveCanvasLiveNodeDragTargets(surface, draggedIds) : [];
            const connectionTargets = isDragScenario ? resolveCanvasLiveConnectionDragTargets(surface, draggedIds) : [];
            const resizeTarget = scenario === "resize" ? surface.querySelector<HTMLElement>("[data-node-id='benchmark-node-0']") : null;
            const muteButton = scenario === "video-controls" ? surface.querySelector<HTMLButtonElement>("[data-canvas-video-player] button[aria-label='静音']") : null;
            if (isDragScenario && nodeTargets.length !== draggedIds.size) throw new Error(`${scenario} 仅解析到 ${nodeTargets.length}/${draggedIds.size} 个拖拽节点`);
            if (scenario === "resize" && !resizeTarget) throw new Error("resize 场景找不到首个节点");
            if (scenario === "video-controls" && !muteButton) throw new Error("video-controls 场景找不到可见视频控件");

            const longTasks: LongTaskSample[] = [];
            const violations: HotPathViolations = { projectSerialization: 0, networkRequest: 0, fullGraphScan: 0 };
            const observer = observeLongTasks(longTasks);
            if (!observer) throw new Error("当前浏览器不支持 Long Tasks 性能观测");
            const restoreGuards = installHotPathGuards(violations);
            surface.setAttribute("data-canvas-viewport-interacting", "true");

            try {
                const frameIntervalsMs = await sampleGestureFrames((frame) => {
                    const progress = frame / (SAMPLE_FRAME_COUNT - 1);
                    const wave = Math.sin(progress * Math.PI * 2);
                    if (scenario === "pan") surface.style.transform = `translate3d(${wave * 48}px, ${wave * 24}px, 0) scale(${canvasScale})`;
                    else if (scenario === "zoom") surface.style.transform = `translate3d(0, 0, 0) scale(${canvasScale + wave * 0.04})`;
                    else if (scenario === "drag-one" || scenario === "drag-20") {
                        const offset = { x: progress * 160, y: wave * 36 };
                        applyCanvasLiveNodeDrag(nodeTargets, offset);
                        applyCanvasLiveConnectionDrag(connectionTargets, offset);
                    } else if (scenario === "resize" && resizeTarget) {
                        applyCanvasLiveResize(resizeTarget, { x: 0, y: 0, width: 360 + progress * 120, height: 240 + progress * 80 });
                    } else if (scenario === "video-controls" && muteButton) muteButton.click();
                });
                for (const entry of observer.takeRecords()) longTasks.push({ durationMs: entry.duration });
                return {
                    scenario,
                    frameIntervalsMs,
                    longTasks,
                    longTaskObserverSupported: true,
                    ...collectMountFacts(surface),
                    hotPathViolations: violations,
                };
            } finally {
                observer.disconnect();
                restoreGuards();
                clearCanvasLiveNodeDrag(nodeTargets);
                clearCanvasLiveConnectionDrag(connectionTargets);
                if (resizeTarget) clearCanvasLiveResize(resizeTarget);
                surface.style.transform = `translate3d(0, 0, 0) scale(${canvasScale})`;
                surface.removeAttribute("data-canvas-viewport-interacting");
            }
        };
        return () => {
            delete window.runCanvasBenchmarkScenario;
        };
    }, [canvasScale, isMediaContract]);

    return (
        <main
            className="canvas-performance-page"
            style={{
                "--canvas-live-scale": canvasScale,
                "--canvas-live-inverse-scale": 1 / canvasScale,
            } as React.CSSProperties}
        >
            <output className="canvas-performance-status" aria-live="polite">
                HMaigc 真实混合图基准 · {nodeCount} 节点 · {fixtureCount.mediaCount} 媒体 · {fixtureCount.connectionCount} 连线
            </output>
            <div ref={worldRef} className="canvas-performance-world" style={{ transform: `translate3d(0, 0, 0) scale(${canvasScale})` }}>
                <svg className="canvas-performance-connections absolute inset-0 size-full overflow-visible">
                    {renderedConnections.map(({ connection, from, to }) => (
                        <ConnectionPath key={connection.id} connection={connection} from={from} to={to} active={false} onSelect={noOp} />
                    ))}
                </svg>
                <div className="canvas-performance-nodes absolute inset-0">
                    {renderedNodes.map((node, index) => (
                        <CanvasNode
                            key={node.id}
                            data={node}
                            dragOffset={index < 20 ? LIVE_DRAG_OFFSET : undefined}
                            scale={canvasScale}
                            isSelected={index === 0}
                            isRelated={false}
                            isFocusRelated={false}
                            isConnectionTarget={false}
                            isConnecting={false}
                            showImageInfo={false}
                            mediaVisibilityTier={renderQuery.mediaTierByNodeId.get(node.id) || "visible"}
                            reduceMediaEffects
                            readOnly
                            onMouseDown={noOp}
                            onHoverStart={noOp}
                            onHoverEnd={noOp}
                            onConnectStart={noOp}
                            onResizeCommit={noOp}
                            onVideoIntrinsicSize={(nodeId, size) => {
                                if (!isMediaContract || nodeId !== videoContractNode.id) return;
                                setVideoContractNode((current) => fitVideoNodeToIntrinsicSize(current, size));
                            }}
                            onContentChange={noOp}
                            onContextMenu={noOp}
                        />
                    ))}
                </div>
            </div>
        </main>
    );
}

const root = document.getElementById("canvas-performance-root");
if (!root) throw new Error("画布性能基准缺少根节点");
createRoot(root).render(<CanvasPerformanceFixture />);
