import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

import {
    collectJavaScriptImportClosure,
    collectStaticAssetClosure,
    evaluateJavaScriptRequestBudget,
} from "./bundle-budget-domain.mjs";

const DIST_DIR = resolve(import.meta.dirname, "../dist");
const MANIFEST_PATH = resolve(DIST_DIR, ".vite/manifest.json");

const KIB = 1024;
const THREE_FEATURE_ENTRIES = [
    {
        source: "src/components/canvas/director/canvas-director-workbench.tsx",
        label: "导演台 3D 按需功能包",
        maxRawBytes: 1050 * KIB,
        maxGzipBytes: 280 * KIB,
    },
];
const BUDGETS = [
    {
        source: "index.html",
        label: "应用入口",
        maxRawBytes: 220 * KIB,
        maxGzipBytes: 75 * KIB,
    },
    {
        source: "src/pages/canvas/project.tsx",
        label: "画布初始功能包",
        maxRawBytes: 500 * KIB,
        maxGzipBytes: 150 * KIB,
    },
];
const CLOSURE_BUDGETS = [
    { sources: ["index.html"], label: "应用入口依赖闭包", maxRequests: 36, maxRawBytes: 760 * KIB, maxGzipBytes: 265 * KIB },
    { sources: ["index.html", "src/pages/home/index.tsx"], label: "首页依赖闭包", maxRequests: 42, maxRawBytes: 1960 * KIB, maxGzipBytes: 660 * KIB },
    { sources: ["index.html", "src/pages/projects/index.tsx"], label: "项目页依赖闭包", maxRequests: 70, maxRawBytes: 1280 * KIB, maxGzipBytes: 440 * KIB },
    { sources: ["index.html", "src/pages/skills/index.tsx"], label: "技能页依赖闭包", maxRequests: 64, maxRawBytes: 1220 * KIB, maxGzipBytes: 420 * KIB },
    { sources: ["index.html", "src/pages/canvas/project.tsx"], label: "画布依赖闭包", maxRequests: 135, maxRawBytes: 2820 * KIB, maxGzipBytes: 940 * KIB },
    { sources: ["index.html", "src/pages/admin/index.tsx"], label: "后台外壳依赖闭包", maxRequests: 66, maxRawBytes: 1400 * KIB, maxGzipBytes: 480 * KIB },
];
const DEFERRED_ASSET_BOUNDARIES = [
    {
        source: "src/pages/canvas/project.tsx",
        label: "画布 FFmpeg 运行时",
        forbiddenAsset: /^assets\/ffmpeg-core-/,
    },
];

const manifest = JSON.parse(readFileSync(MANIFEST_PATH, "utf8"));
const failures = [];

function formatKib(bytes) {
    return `${(bytes / KIB).toFixed(2)} KiB`;
}

function collectImportClosure(source, visited = new Set()) {
    if (visited.has(source)) return visited;
    visited.add(source);
    for (const dependency of manifest[source]?.imports || []) {
        collectImportClosure(dependency, visited);
    }
    return visited;
}

function measureChunk(source, label, maxRawBytes, maxGzipBytes) {
    const chunk = manifest[source];
    if (!chunk?.file) {
        failures.push(`${label}：构建清单缺少 ${source}`);
        return;
    }

    const assetPath = resolve(DIST_DIR, chunk.file);
    const rawBytes = statSync(assetPath).size;
    const gzipBytes = gzipSync(readFileSync(assetPath), { level: 9 }).byteLength;
    const rawPassed = rawBytes <= maxRawBytes;
    const gzipPassed = gzipBytes <= maxGzipBytes;

    console.log(`${rawPassed && gzipPassed ? "PASS" : "FAIL"} ${label}: ` + `${formatKib(rawBytes)} raw / ${formatKib(gzipBytes)} gzip ` + `(预算 ${formatKib(maxRawBytes)} / ${formatKib(maxGzipBytes)})`);

    if (!rawPassed || !gzipPassed) {
        failures.push(`${label} 超出生产体积预算`);
    }
}

function measureClosure(sources, label, maxRequests, maxRawBytes, maxGzipBytes) {
    const files = collectJavaScriptImportClosure(manifest, sources);
    if (files.length === 0) {
        failures.push(`${label}：构建清单没有可计量的 JavaScript 文件`);
        return;
    }

    let rawBytes = 0;
    let gzipBytes = 0;
    for (const file of files) {
        const content = readFileSync(resolve(DIST_DIR, file));
        rawBytes += content.byteLength;
        gzipBytes += gzipSync(content, { level: 9 }).byteLength;
    }

    const rawPassed = rawBytes <= maxRawBytes;
    const gzipPassed = gzipBytes <= maxGzipBytes;
    const requestBudget = evaluateJavaScriptRequestBudget(files, maxRequests);
    console.log(
        `${rawPassed && gzipPassed && requestBudget.passed ? "PASS" : "FAIL"} ${label}: ` +
            `${requestBudget.requestCount} 个 JS（预算 ${requestBudget.maxRequests}），` +
            `${formatKib(rawBytes)} raw / ${formatKib(gzipBytes)} gzip ` +
            `(预算 ${formatKib(maxRawBytes)} / ${formatKib(maxGzipBytes)})`,
    );
    if (!requestBudget.passed) failures.push(`${label} 超出生产请求数预算`);
    if (!rawPassed || !gzipPassed) failures.push(`${label} 超出生产体积预算`);
}

for (const budget of BUDGETS) {
    measureChunk(budget.source, budget.label, budget.maxRawBytes, budget.maxGzipBytes);
}

for (const budget of CLOSURE_BUDGETS) {
    measureClosure(budget.sources, budget.label, budget.maxRequests, budget.maxRawBytes, budget.maxGzipBytes);
}

for (const boundary of DEFERRED_ASSET_BOUNDARIES) {
    const eagerAssets = collectStaticAssetClosure(manifest, [boundary.source]).filter((asset) => boundary.forbiddenAsset.test(asset));
    if (eagerAssets.length > 0) {
        failures.push(`${boundary.label} 必须按操作加载，当前静态资产：${eagerAssets.join("、")}`);
    } else {
        console.log(`PASS ${boundary.label}: 未进入画布静态资产闭包`);
    }
}

const applicationShell = collectImportClosure("index.html");
for (const feature of THREE_FEATURE_ENTRIES) {
    const featureChunk = manifest[feature.source];
    if (!featureChunk?.isDynamicEntry) {
        failures.push(`${feature.source} 必须保持动态入口`);
        continue;
    }

    measureChunk(feature.source, feature.label, feature.maxRawBytes, feature.maxGzipBytes);
    if (applicationShell.has(feature.source)) {
        failures.push(`${feature.label} 泄漏进应用入口，必须保持按需加载`);
    }
}

if (failures.length > 0) {
    throw new Error(`前端构建体积门禁失败：\n- ${failures.join("\n- ")}`);
}
