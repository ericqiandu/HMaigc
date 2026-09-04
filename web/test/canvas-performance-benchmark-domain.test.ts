import { describe, expect, test } from "bun:test";

import {
    CANVAS_BENCHMARK_SCENARIOS,
    CANVAS_PERFORMANCE_BUDGETS,
    assessCanvasPerformanceBenchmark,
    assessRelativeRegression,
    percentile,
    summarizeCanvasFrameSamples,
} from "../scripts/canvas-performance-benchmark-domain.mjs";

const greenMeasurement = {
    scenario: "pan",
    nodeCount: 50,
    mediaCount: 50,
    connectionCount: 300,
    mountDurationMs: 120,
    frameCount: 120,
    p95FrameIntervalMs: 16,
    maxFrameIntervalMs: 18,
    longTaskCount: 0,
    longTaskObserverSupported: true,
    maxLongTaskDurationMs: 0,
    longTaskBlockingTimeMs: 0,
    domNodeCount: 500,
    mountedNodeCount: 12,
    mountedEdgeCount: 18,
    mountedVideoCount: 1,
    offscreenMountedVideoCount: 0,
    hotPathViolations: {
        projectSerialization: 0,
        networkRequest: 0,
        fullGraphScan: 0,
    },
};

describe("canvas browser performance benchmark domain", () => {
    test("publishes the three required scales and six gesture scenarios", () => {
        expect(Object.keys(CANVAS_PERFORMANCE_BUDGETS).map(Number)).toEqual([50, 200, 500]);
        expect(CANVAS_BENCHMARK_SCENARIOS).toEqual(["pan", "zoom", "drag-one", "drag-20", "resize", "video-controls"]);
    });

    test("calculates deterministic frame percentiles and long-task facts", () => {
        expect(percentile([8, 16, 17, 18, 40], 0.95)).toBe(40);
        expect(
            summarizeCanvasFrameSamples({
                frameIntervalsMs: [16, 17, 18, 40],
                longTasks: [{ durationMs: 58 }, { durationMs: 72 }],
            }),
        ).toEqual({
            frameCount: 4,
            p95FrameIntervalMs: 40,
            maxFrameIntervalMs: 40,
            longTaskCount: 2,
            maxLongTaskDurationMs: 72,
            longTaskBlockingTimeMs: 30,
        });
    });

    test("accepts a complete measurement inside absolute and checked-in relative budgets", () => {
        const result = assessCanvasPerformanceBenchmark(greenMeasurement);

        expect(result.passed).toBeTrue();
        expect(result.failures).toEqual([]);
    });

    test("enforces the 20ms frame gate and 15 percent relative regression gate", () => {
        expect(assessCanvasPerformanceBenchmark({ ...greenMeasurement, p95FrameIntervalMs: 20.01 }).failures).toContain("P95 帧间隔 20.01ms 超过 20ms");
        expect(assessRelativeRegression({ baseline: 16, current: 18.5, maximumRatio: 1.15 }).passed).toBeFalse();
        expect(assessRelativeRegression({ baseline: 16, current: 18.4, maximumRatio: 1.15 }).passed).toBeTrue();
    });

    test("reports every gesture-time correctness and performance violation", () => {
        const result = assessCanvasPerformanceBenchmark({
            ...greenMeasurement,
            frameCount: 119,
            longTaskObserverSupported: false,
            p95FrameIntervalMs: 20.01,
            longTaskCount: 1,
            maxLongTaskDurationMs: 50.01,
            offscreenMountedVideoCount: 1,
            hotPathViolations: {
                projectSerialization: 1,
                networkRequest: 2,
                fullGraphScan: 3,
            },
        });

        expect(result.passed).toBeFalse();
        expect(result.failures).toEqual(
            expect.arrayContaining([
                "手势仅采集 119 帧，要求至少 120 帧",
                "缺少 Long Tasks 观测能力",
                "P95 帧间隔 20.01ms 超过 20ms",
                "手势期间出现 50.01ms 长任务，限制为 50ms",
                "挂载了 1 个离屏视频元素",
                "手势热路径发生 1 次项目序列化",
                "手势热路径发生 2 次网络请求",
                "手势热路径发生 3 次全图扫描",
            ]),
        );
    });

    test("rejects an optimized-away fixture that mounted no measurable graph", () => {
        const result = assessCanvasPerformanceBenchmark({
            ...greenMeasurement,
            domNodeCount: 0,
            mountedNodeCount: 0,
            mountedEdgeCount: 0,
            mountedVideoCount: 0,
        });

        expect(result.failures).toEqual(
            expect.arrayContaining(["基准页面没有 DOM 节点", "基准页面没有挂载画布节点", "基准页面没有挂载连线", "基准页面没有挂载视频"]),
        );
    });

    test("rejects fixture cardinality drift for every scale", () => {
        for (const nodeCount of [50, 200, 500]) {
            const budget = CANVAS_PERFORMANCE_BUDGETS[nodeCount];
            const base = {
                ...greenMeasurement,
                nodeCount,
                mediaCount: budget.mediaCount,
                connectionCount: budget.connectionCount,
            };

            expect(assessCanvasPerformanceBenchmark({ ...base, mediaCount: budget.mediaCount - 1 }).failures).toContain(`媒体数量 ${budget.mediaCount - 1} 与基准契约 ${budget.mediaCount} 不一致`);
            expect(assessCanvasPerformanceBenchmark({ ...base, connectionCount: budget.connectionCount - 1 }).failures).toContain(`连线数量 ${budget.connectionCount - 1} 与基准契约 ${budget.connectionCount} 不一致`);
        }
    });

    test("does not silently accept absent measurements", () => {
        const result = assessCanvasPerformanceBenchmark({ scenario: "pan", nodeCount: 50 });

        expect(result.passed).toBeFalse();
        expect(result.failures).toEqual(
            expect.arrayContaining([
                "缺少媒体数量",
                "缺少连线数量",
                "缺少挂载耗时",
                "缺少帧采样数量",
                "缺少P95 帧间隔",
                "缺少最大帧间隔",
                "缺少长任务数量",
                "缺少 Long Tasks 观测能力",
                "缺少最大长任务耗时",
                "缺少长任务阻塞耗时",
                "缺少DOM 节点数量",
                "缺少已挂载节点数量",
                "缺少已挂载连线数量",
                "缺少已挂载视频数量",
                "缺少离屏视频数量",
                "缺少手势热路径违规计数",
            ]),
        );
    });

    test("rejects unsupported scales and scenarios", () => {
        expect(() => assessCanvasPerformanceBenchmark({ ...greenMeasurement, nodeCount: 1_000 })).toThrow("不支持的画布性能节点规模: 1000");
        expect(() => assessCanvasPerformanceBenchmark({ ...greenMeasurement, scenario: "scroll" })).toThrow("不支持的画布性能场景: scroll");
    });
});
