export const CANVAS_BENCHMARK_SCENARIOS = Object.freeze(["pan", "zoom", "drag-one", "drag-20", "resize", "video-controls"]);

export const CANVAS_PERFORMANCE_BUDGETS = Object.freeze({
    50: Object.freeze({ mediaCount: 50, connectionCount: 300, minimumFrameCount: 120, p95FrameIntervalMs: 20, maxLongTaskDurationMs: 50, maximumRelativeRegressionRatio: 1.15 }),
    200: Object.freeze({ mediaCount: 100, connectionCount: 1_000, minimumFrameCount: 120, p95FrameIntervalMs: 20, maxLongTaskDurationMs: 50, maximumRelativeRegressionRatio: 1.15 }),
    500: Object.freeze({ mediaCount: 200, connectionCount: 3_000, minimumFrameCount: 120, p95FrameIntervalMs: 20, maxLongTaskDurationMs: 50, maximumRelativeRegressionRatio: 1.15 }),
});

const createScenarioBaselines = () => Object.freeze(Object.fromEntries(CANVAS_BENCHMARK_SCENARIOS.map((scenario) => [scenario, Object.freeze({ p95FrameIntervalMs: 16.8 })])));

// This is intentionally reviewed source data. The benchmark runner reads it but never rewrites it.
export const CANVAS_PERFORMANCE_BASELINES = Object.freeze({
    50: createScenarioBaselines(),
    200: createScenarioBaselines(),
    500: createScenarioBaselines(),
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
    const intervals = Array.isArray(frameIntervalsMs) ? frameIntervalsMs.map(Number).filter(Number.isFinite) : [];
    const taskDurations = Array.isArray(longTasks) ? longTasks.map((task) => Number(task?.durationMs)).filter(Number.isFinite) : [];
    return {
        frameCount: intervals.length,
        p95FrameIntervalMs: percentile(intervals, 0.95),
        maxFrameIntervalMs: roundMeasurement(intervals.length > 0 ? Math.max(...intervals) : 0),
        longTaskCount: taskDurations.length,
        maxLongTaskDurationMs: roundMeasurement(taskDurations.length > 0 ? Math.max(...taskDurations) : 0),
        longTaskBlockingTimeMs: roundMeasurement(taskDurations.reduce((total, duration) => total + Math.max(0, duration - 50), 0)),
    };
}

export function assessRelativeRegression({ baseline, current, maximumRatio }) {
    if (!Number.isFinite(baseline) || baseline <= 0) throw new Error("性能基线必须是大于 0 的有限数字");
    if (!Number.isFinite(current) || current < 0) throw new Error("当前性能值必须是非负有限数字");
    if (!Number.isFinite(maximumRatio) || maximumRatio < 1) throw new Error("性能回退比例上限必须是不小于 1 的有限数字");
    const ratio = current / baseline;
    return { passed: ratio <= maximumRatio, ratio: roundMeasurement(ratio) };
}

export function assessCanvasPerformanceBenchmark(measurement) {
    const budget = CANVAS_PERFORMANCE_BUDGETS[measurement?.nodeCount];
    if (!budget) throw new Error(`不支持的画布性能节点规模: ${measurement?.nodeCount}`);
    if (!CANVAS_BENCHMARK_SCENARIOS.includes(measurement?.scenario)) throw new Error(`不支持的画布性能场景: ${measurement?.scenario}`);
    const baseline = CANVAS_PERFORMANCE_BASELINES[measurement.nodeCount][measurement.scenario];
    const failures = [];

    const mediaCount = readMeasurement(measurement, "mediaCount", "媒体数量", failures);
    const connectionCount = readMeasurement(measurement, "connectionCount", "连线数量", failures);
    readMeasurement(measurement, "mountDurationMs", "挂载耗时", failures);
    const frameCount = readMeasurement(measurement, "frameCount", "帧采样数量", failures);
    const p95FrameIntervalMs = readMeasurement(measurement, "p95FrameIntervalMs", "P95 帧间隔", failures);
    readMeasurement(measurement, "maxFrameIntervalMs", "最大帧间隔", failures);
    readMeasurement(measurement, "longTaskCount", "长任务数量", failures);
    if (measurement.longTaskObserverSupported !== true) failures.push("缺少 Long Tasks 观测能力");
    const maxLongTaskDurationMs = readMeasurement(measurement, "maxLongTaskDurationMs", "最大长任务耗时", failures);
    readMeasurement(measurement, "longTaskBlockingTimeMs", "长任务阻塞耗时", failures);
    const domNodeCount = readMeasurement(measurement, "domNodeCount", "DOM 节点数量", failures);
    const mountedNodeCount = readMeasurement(measurement, "mountedNodeCount", "已挂载节点数量", failures);
    const mountedEdgeCount = readMeasurement(measurement, "mountedEdgeCount", "已挂载连线数量", failures);
    const mountedVideoCount = readMeasurement(measurement, "mountedVideoCount", "已挂载视频数量", failures);
    const offscreenMountedVideoCount = readMeasurement(measurement, "offscreenMountedVideoCount", "离屏视频数量", failures);

    if (mediaCount !== null && mediaCount !== budget.mediaCount) failures.push(`媒体数量 ${mediaCount} 与基准契约 ${budget.mediaCount} 不一致`);
    if (connectionCount !== null && connectionCount !== budget.connectionCount) failures.push(`连线数量 ${connectionCount} 与基准契约 ${budget.connectionCount} 不一致`);
    if (frameCount !== null && frameCount < budget.minimumFrameCount) failures.push(`手势仅采集 ${frameCount} 帧，要求至少 ${budget.minimumFrameCount} 帧`);
    if (p95FrameIntervalMs !== null && p95FrameIntervalMs > budget.p95FrameIntervalMs) failures.push(`P95 帧间隔 ${p95FrameIntervalMs}ms 超过 ${budget.p95FrameIntervalMs}ms`);
    if (maxLongTaskDurationMs !== null && maxLongTaskDurationMs > budget.maxLongTaskDurationMs) failures.push(`手势期间出现 ${maxLongTaskDurationMs}ms 长任务，限制为 ${budget.maxLongTaskDurationMs}ms`);
    if (domNodeCount === 0) failures.push("基准页面没有 DOM 节点");
    if (mountedNodeCount === 0) failures.push("基准页面没有挂载画布节点");
    if (mountedEdgeCount === 0) failures.push("基准页面没有挂载连线");
    if (mountedVideoCount === 0) failures.push("基准页面没有挂载视频");
    if (offscreenMountedVideoCount !== null && offscreenMountedVideoCount > 0) failures.push(`挂载了 ${offscreenMountedVideoCount} 个离屏视频元素`);

    let relativeRegression = null;
    if (p95FrameIntervalMs !== null) {
        relativeRegression = assessRelativeRegression({ baseline: baseline.p95FrameIntervalMs, current: p95FrameIntervalMs, maximumRatio: budget.maximumRelativeRegressionRatio });
        if (!relativeRegression.passed) failures.push(`P95 帧间隔相对基线 ${baseline.p95FrameIntervalMs}ms 回退至 ${p95FrameIntervalMs}ms，超过 15%`);
    }

    const hotPathViolations = measurement.hotPathViolations;
    if (!hotPathViolations || typeof hotPathViolations !== "object") {
        failures.push("缺少手势热路径违规计数");
    } else {
        assessHotPathViolation(hotPathViolations, "projectSerialization", "项目序列化", failures);
        assessHotPathViolation(hotPathViolations, "networkRequest", "网络请求", failures);
        assessHotPathViolation(hotPathViolations, "fullGraphScan", "全图扫描", failures);
    }

    return { passed: failures.length === 0, budget, baseline, relativeRegression, failures };
}

function readMeasurement(measurement, field, label, failures) {
    const value = measurement[field];
    if (!Number.isFinite(value) || value < 0) {
        failures.push(`缺少${label}`);
        return null;
    }
    return value;
}

function assessHotPathViolation(violations, field, label, failures) {
    const value = violations[field];
    if (!Number.isFinite(value) || value < 0) {
        failures.push(`缺少手势热路径${label}违规计数`);
        return;
    }
    if (value > 0) failures.push(`手势热路径发生 ${value} 次${label}`);
}
