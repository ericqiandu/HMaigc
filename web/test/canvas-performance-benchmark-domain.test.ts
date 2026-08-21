import { describe, expect, test } from "bun:test";

import { CANVAS_PERFORMANCE_BUDGETS, assessCanvasPerformanceBenchmark, percentile, summarizeCanvasFrameSamples } from "../scripts/canvas-performance-benchmark-domain.mjs";

describe("canvas browser performance benchmark domain", () => {
    test("calculates deterministic frame percentiles and blocking time", () => {
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
            longTaskBlockingTimeMs: 30,
        });
    });

    test("accepts measurements that meet the node-count-specific budgets", () => {
        const result = assessCanvasPerformanceBenchmark({
            nodeCount: 1_000,
            mountDurationMs: CANVAS_PERFORMANCE_BUDGETS[1_000].mountDurationMs,
            frameCount: 120,
            p95FrameIntervalMs: CANVAS_PERFORMANCE_BUDGETS[1_000].p95FrameIntervalMs,
            maxFrameIntervalMs: CANVAS_PERFORMANCE_BUDGETS[1_000].maxFrameIntervalMs,
            longTaskCount: 1,
            longTaskBlockingTimeMs: CANVAS_PERFORMANCE_BUDGETS[1_000].longTaskBlockingTimeMs,
            domNodeCount: 12_000,
        });

        expect(result.passed).toBeTrue();
        expect(result.failures).toEqual([]);
    });

    test("reports every violated budget without hiding unsupported node counts", () => {
        const result = assessCanvasPerformanceBenchmark({
            nodeCount: 500,
            mountDurationMs: CANVAS_PERFORMANCE_BUDGETS[500].mountDurationMs + 1,
            frameCount: 119,
            p95FrameIntervalMs: CANVAS_PERFORMANCE_BUDGETS[500].p95FrameIntervalMs + 1,
            maxFrameIntervalMs: CANVAS_PERFORMANCE_BUDGETS[500].maxFrameIntervalMs + 1,
            longTaskCount: 3,
            longTaskBlockingTimeMs: CANVAS_PERFORMANCE_BUDGETS[500].longTaskBlockingTimeMs + 1,
            domNodeCount: 6_000,
        });

        expect(result.passed).toBeFalse();
        expect(result.failures).toHaveLength(5);
        expect(() =>
            assessCanvasPerformanceBenchmark({
                nodeCount: 750,
                mountDurationMs: 1,
                frameCount: 120,
                p95FrameIntervalMs: 1,
                maxFrameIntervalMs: 1,
                longTaskCount: 0,
                longTaskBlockingTimeMs: 0,
                domNodeCount: 1,
            }),
        ).toThrow("不支持的画布性能节点规模: 750");
    });
});
