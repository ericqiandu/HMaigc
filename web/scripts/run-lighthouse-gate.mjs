import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { spawn } from "node:child_process";
import lighthouse from "lighthouse";
import desktopConfig from "lighthouse/core/config/desktop-config.js";
import { launch } from "chrome-launcher";

const PORT = 3100;
const EXTERNAL_BASE_URL = process.env.LIGHTHOUSE_BASE_URL?.trim();
const BASE_URL = EXTERNAL_BASE_URL || `http://127.0.0.1:${PORT}`;
const REPORT_DIR = resolve(import.meta.dirname, "../.lighthouseci");
const VITE_BIN = resolve(import.meta.dirname, "../node_modules/vite/bin/vite.js");
const RUNS_PER_ROUTE = 3;
const ROUTES = [
    { id: "home", url: `${BASE_URL}/` },
    { id: "login", url: `${BASE_URL}/login` },
];

const BUDGETS = {
    largestContentfulPaint: 2500,
    totalBlockingTime: 200,
    cumulativeLayoutShift: 0.1,
    performance: 0.9,
    accessibility: 0.95,
    bestPractices: 0.9,
    seo: 0.95,
};

const waitForServer = async () => {
    const deadline = Date.now() + 120_000;
    while (Date.now() < deadline) {
        try {
            const response = await fetch(BASE_URL);
            if (response.ok) return;
        } catch {
            // The preview process can take a moment to accept connections.
        }
        await new Promise((resolveDelay) => setTimeout(resolveDelay, 250));
    }
    throw new Error("Vite 生产预览在 120 秒内未就绪");
};

const median = (values) => {
    const ordered = [...values].sort((left, right) => left - right);
    return ordered[Math.floor(ordered.length / 2)];
};

const readScore = (result, category) => result.categories[category]?.score ?? 0;
const readMetric = (result, audit) => result.audits[audit]?.numericValue ?? Number.POSITIVE_INFINITY;

const preview = EXTERNAL_BASE_URL
    ? undefined
    : spawn(process.execPath, [VITE_BIN, "preview", "--host", "127.0.0.1", "--port", String(PORT), "--strictPort"], {
          stdio: "inherit",
      });

let chrome;
try {
    await waitForServer();
    mkdirSync(REPORT_DIR, { recursive: true });
    chrome = await launch({ chromeFlags: ["--headless", "--no-sandbox", "--disable-gpu"] });

    const failures = [];
    for (const route of ROUTES) {
        const results = [];
        for (let runIndex = 1; runIndex <= RUNS_PER_ROUTE; runIndex += 1) {
            const runner = await lighthouse(
                route.url,
                {
                    port: chrome.port,
                    logLevel: "error",
                    output: "json",
                    onlyCategories: ["performance", "accessibility", "best-practices", "seo"],
                },
                desktopConfig,
            );
            if (!runner) throw new Error(`${route.id} 第 ${runIndex} 次 Lighthouse 未返回结果`);
            results.push(runner.lhr);
            writeFileSync(resolve(REPORT_DIR, `${route.id}-${runIndex}.json`), JSON.stringify(runner.lhr), "utf8");
        }

        const summary = {
            largestContentfulPaint: median(results.map((result) => readMetric(result, "largest-contentful-paint"))),
            totalBlockingTime: median(results.map((result) => readMetric(result, "total-blocking-time"))),
            cumulativeLayoutShift: median(results.map((result) => readMetric(result, "cumulative-layout-shift"))),
            performance: median(results.map((result) => readScore(result, "performance"))),
            accessibility: median(results.map((result) => readScore(result, "accessibility"))),
            bestPractices: median(results.map((result) => readScore(result, "best-practices"))),
            seo: median(results.map((result) => readScore(result, "seo"))),
        };
        writeFileSync(resolve(REPORT_DIR, `${route.id}-summary.json`), JSON.stringify(summary, null, 2), "utf8");
        console.log(`${route.id}: ${JSON.stringify(summary)}`);

        for (const [metric, budget] of Object.entries(BUDGETS)) {
            const actual = summary[metric];
            const isTimingMetric = metric.endsWith("Paint") || metric.endsWith("Time");
            const passed = isTimingMetric || metric === "cumulativeLayoutShift" ? actual <= budget : actual >= budget;
            if (!passed) failures.push(`${route.id} ${metric}: ${actual}（门槛 ${budget}）`);
        }
    }

    if (failures.length > 0) {
        throw new Error(`Lighthouse 生产门禁失败：\n- ${failures.join("\n- ")}`);
    }
} finally {
    if (chrome) {
        try {
            await chrome.kill();
        } catch {
            console.warn("Chrome 审计进程已停止；Windows 临时目录将由系统回收");
        }
    }
    preview?.kill();
}
