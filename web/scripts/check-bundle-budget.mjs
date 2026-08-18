import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

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

for (const budget of BUDGETS) {
    measureChunk(budget.source, budget.label, budget.maxRawBytes, budget.maxGzipBytes);
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
