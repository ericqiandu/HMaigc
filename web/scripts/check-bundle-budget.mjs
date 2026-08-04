import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

const DIST_DIR = resolve(import.meta.dirname, "../dist");
const MANIFEST_PATH = resolve(DIST_DIR, ".vite/manifest.json");

const KIB = 1024;
const THREE_RUNTIME_FILE_MARKER = "react-three-fiber";
const THREE_FEATURE_ENTRIES = [
    "src/components/canvas/canvas-emotion-workspace.tsx",
    "src/components/canvas/director/canvas-director-workbench.tsx",
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

const threeRuntimeSource = Object.entries(manifest).find(([, chunk]) => chunk.file?.includes(THREE_RUNTIME_FILE_MARKER))?.[0];
if (!threeRuntimeSource) {
    failures.push("3D 渲染器：构建清单中缺少 react-three-fiber 运行时块");
} else {
    measureChunk(threeRuntimeSource, "3D 渲染器按需功能包", 900 * KIB, 250 * KIB);

    const applicationShell = collectImportClosure("index.html");
    if (applicationShell.has(threeRuntimeSource)) {
        failures.push("3D 渲染器泄漏进应用入口，必须仅由按需功能入口加载");
    }

    for (const featureSource of THREE_FEATURE_ENTRIES) {
        const featureChunk = manifest[featureSource];
        if (!featureChunk?.isDynamicEntry) {
            failures.push(`${featureSource} 必须保持动态入口`);
            continue;
        }
        if (!collectImportClosure(featureSource).has(threeRuntimeSource)) {
            failures.push(`${featureSource} 未加载预期的 3D 渲染器运行时`);
        }
    }
}

if (failures.length > 0) {
    throw new Error(`前端构建体积门禁失败：\n- ${failures.join("\n- ")}`);
}
