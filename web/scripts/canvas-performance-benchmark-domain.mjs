export const CANVAS_PERFORMANCE_BUDGETS = Object.freeze({
    500: Object.freeze({
        mountDurationMs: 2_500,
        p95FrameIntervalMs: 25,
        maxFrameIntervalMs: 80,
        longTaskBlockingTimeMs: 100,
    }),
    1_000: Object.freeze({
        mountDurationMs: 5_000,
        p95FrameIntervalMs: 34,
        maxFrameIntervalMs: 120,
        longTaskBlockingTimeMs: 200,
    }),
});

const roundMeasurement = (value) => Math.round(value * 100) / 100;

export function percentile(values, ratio) {
    if (!Array.isArray(values) || values.length === 0) return 0;
    if (!Number.isFinite(ratio) || ratio < 0 || ratio > 1) throw new Error(`无效的百分位参数: ${ratio}`);
    const ordered = values
        .map(Number)
        .filter(Number.isFinite)
        .sort((left, right) => left - right);
    if (ordered.length === 0) return 0;
    const index = Math.max(0, Math.ceil(ordered.length * ratio) - 1);
    return roundMeasurement(ordered[index]);
}

export function summarizeCanvasFrameSamples({ frameIntervalsMs, longTasks }) {
    const intervals = frameIntervalsMs.map(Number).filter(Number.isFinite);
    const taskDurations = longTasks.map((task) => Number(task.durationMs)).filter(Number.isFinite);
    return {
        frameCount: intervals.length,
        p95FrameIntervalMs: percentile(intervals, 0.95),
        maxFrameIntervalMs: roundMeasurement(intervals.length > 0 ? Math.max(...intervals) : 0),
        longTaskCount: taskDurations.length,
        longTaskBlockingTimeMs: roundMeasurement(taskDurations.reduce((total, duration) => total + Math.max(0, duration - 50), 0)),
    };
}

export function assessCanvasPerformanceBenchmark(measurement) {
    const budget = CANVAS_PERFORMANCE_BUDGETS[measurement.nodeCount];
    if (!budget) throw new Error(`不支持的画布性能节点规模: ${measurement.nodeCount}`);

    const failures = [];
    if (measurement.mountDurationMs > budget.mountDurationMs) failures.push(`挂载耗时 ${measurement.mountDurationMs}ms 超过 ${budget.mountDurationMs}ms`);
    if (measurement.frameCount < 120) failures.push(`拖拽仅采集 ${measurement.frameCount} 帧，要求至少 120 帧`);
    if (measurement.p95FrameIntervalMs > budget.p95FrameIntervalMs) failures.push(`P95 帧间隔 ${measurement.p95FrameIntervalMs}ms 超过 ${budget.p95FrameIntervalMs}ms`);
    if (measurement.maxFrameIntervalMs > budget.maxFrameIntervalMs) failures.push(`最大帧间隔 ${measurement.maxFrameIntervalMs}ms 超过 ${budget.maxFrameIntervalMs}ms`);
    if (measurement.longTaskBlockingTimeMs > budget.longTaskBlockingTimeMs) failures.push(`长任务阻塞 ${measurement.longTaskBlockingTimeMs}ms 超过 ${budget.longTaskBlockingTimeMs}ms`);

    return { passed: failures.length === 0, budget, failures };
}
