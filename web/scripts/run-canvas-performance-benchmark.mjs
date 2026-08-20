import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { launch } from "chrome-launcher";
import puppeteer from "puppeteer-core";
import { build as viteBuild } from "vite";

import { assessCanvasPerformanceBenchmark, summarizeCanvasFrameSamples } from "./canvas-performance-benchmark-domain.mjs";

const RUNS_PER_SCALE = 3;
const NODE_COUNTS = [500, 1_000];
const EXTERNAL_BASE_URL = process.env.CANVAS_BENCHMARK_BASE_URL?.trim();
const WEB_DIR = resolve(import.meta.dirname, "..");
const VITE_BIN = resolve(import.meta.dirname, "../node_modules/vite/bin/vite.js");
const REPORT_DIR = resolve(import.meta.dirname, "../../qa-artifacts/canvas-performance");
const BENCHMARK_DIST_DIR = resolve(REPORT_DIR, "dist");

const delay = (milliseconds) => new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));

const findAvailablePort = () =>
    new Promise((resolvePort, rejectPort) => {
        const server = createServer();
        server.once("error", rejectPort);
        server.listen(0, "127.0.0.1", () => {
            const address = server.address();
            if (!address || typeof address === "string") {
                server.close();
                rejectPort(new Error("无法分配画布性能基准端口"));
                return;
            }
            server.close((error) => (error ? rejectPort(error) : resolvePort(address.port)));
        });
    });

const port = EXTERNAL_BASE_URL ? null : await findAvailablePort();
const BASE_URL = EXTERNAL_BASE_URL || `http://127.0.0.1:${port}`;

if (!EXTERNAL_BASE_URL) {
    await viteBuild({
        root: WEB_DIR,
        configFile: resolve(WEB_DIR, "vite.config.ts"),
        build: {
            outDir: BENCHMARK_DIST_DIR,
            emptyOutDir: true,
            rollupOptions: { input: resolve(WEB_DIR, "perf/canvas-performance.html") },
        },
    });
}

const waitForServer = async () => {
    const deadline = Date.now() + 120_000;
    while (Date.now() < deadline) {
        try {
            const response = await fetch(`${BASE_URL}/perf/canvas-performance.html?nodes=500`);
            if (response.ok && (await response.text()).includes("HMaigc 画布性能基准")) return;
        } catch {
            // Vite may still be binding its port.
        }
        await delay(250);
    }
    throw new Error("画布性能基准服务器在 120 秒内未就绪");
};

const median = (values) => {
    const ordered = [...values].sort((left, right) => left - right);
    return ordered[Math.floor(ordered.length / 2)];
};

const roundedMedian = (values) => Math.round(median(values) * 100) / 100;

const summarizeRuns = (nodeCount, runs) => ({
    nodeCount,
    mountDurationMs: roundedMedian(runs.map((run) => run.mountDurationMs)),
    frameCount: Math.min(...runs.map((run) => run.frameCount)),
    p95FrameIntervalMs: roundedMedian(runs.map((run) => run.p95FrameIntervalMs)),
    maxFrameIntervalMs: roundedMedian(runs.map((run) => run.maxFrameIntervalMs)),
    longTaskCount: roundedMedian(runs.map((run) => run.longTaskCount)),
    longTaskBlockingTimeMs: roundedMedian(runs.map((run) => run.longTaskBlockingTimeMs)),
    domNodeCount: roundedMedian(runs.map((run) => run.domNodeCount)),
});

const preview = EXTERNAL_BASE_URL
    ? undefined
    : spawn(process.execPath, [VITE_BIN, "preview", "--host", "127.0.0.1", "--port", String(port), "--strictPort", "--outDir", BENCHMARK_DIST_DIR], {
          stdio: "inherit",
      });

let chrome;
let browser;
try {
    await waitForServer();
    mkdirSync(REPORT_DIR, { recursive: true });
    chrome = await launch({ chromeFlags: ["--headless=new", "--no-sandbox", "--disable-dev-shm-usage", "--window-size=1440,900"] });
    browser = await puppeteer.connect({ browserURL: `http://127.0.0.1:${chrome.port}` });

    const failures = [];
    for (const nodeCount of NODE_COUNTS) {
        const runs = [];
        for (let runIndex = 1; runIndex <= RUNS_PER_SCALE; runIndex += 1) {
            const page = await browser.newPage();
            try {
                const pageErrors = [];
                page.on("pageerror", (error) => pageErrors.push(error.message));
                await page.setViewport({ width: 1440, height: 900, deviceScaleFactor: 1 });
                await page.goto(`${BASE_URL}/perf/canvas-performance.html?nodes=${nodeCount}`, { waitUntil: "networkidle0", timeout: 120_000 });
                await page.waitForSelector("html[data-canvas-benchmark-ready='true']", { timeout: 120_000 });
                const ready = await page.evaluate(() => window.__CANVAS_BENCHMARK_READY__);
                if (!ready) throw new Error(`${nodeCount} 节点第 ${runIndex} 轮缺少挂载事实`);
                const samples = await page.evaluate(async () => {
                    if (!window.runCanvasDragBenchmark) throw new Error("画布性能基准未注册拖拽采样器");
                    return window.runCanvasDragBenchmark();
                });
                if (pageErrors.length > 0) throw new Error(`${nodeCount} 节点第 ${runIndex} 轮页面异常:\n- ${pageErrors.join("\n- ")}`);
                const frameSummary = summarizeCanvasFrameSamples(samples);
                const measurement = { ...ready, ...frameSummary };
                runs.push(measurement);
                writeFileSync(resolve(REPORT_DIR, `${nodeCount}-nodes-run-${runIndex}.json`), JSON.stringify(measurement, null, 2), "utf8");
            } finally {
                await page.close();
            }
        }

        const summary = summarizeRuns(nodeCount, runs);
        const assessment = assessCanvasPerformanceBenchmark(summary);
        writeFileSync(resolve(REPORT_DIR, `${nodeCount}-nodes-summary.json`), JSON.stringify({ ...summary, assessment }, null, 2), "utf8");
        console.log(`${nodeCount} 节点: ${JSON.stringify({ ...summary, passed: assessment.passed })}`);
        failures.push(...assessment.failures.map((failure) => `${nodeCount} 节点 ${failure}`));
    }

    if (failures.length > 0) throw new Error(`画布浏览器性能门禁失败:\n- ${failures.join("\n- ")}`);
} finally {
    await browser?.disconnect();
    if (chrome) {
        try {
            await chrome.kill();
        } catch {
            console.warn("Chrome 性能基准进程已停止；Windows 临时目录将由系统回收");
        }
    }
    preview?.kill();
}
