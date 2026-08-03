import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

const DIST_DIR = resolve(import.meta.dirname, "../dist");
const MANIFEST_PATH = resolve(DIST_DIR, ".vite/manifest.json");

const KIB = 1024;
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

for (const budget of BUDGETS) {
    const chunk = manifest[budget.source];
    if (!chunk?.file) {
        failures.push(`${budget.label}：构建清单缺少 ${budget.source}`);
        continue;
    }

    const assetPath = resolve(DIST_DIR, chunk.file);
    const rawBytes = statSync(assetPath).size;
    const gzipBytes = gzipSync(readFileSync(assetPath), { level: 9 }).byteLength;
    const rawPassed = rawBytes <= budget.maxRawBytes;
    const gzipPassed = gzipBytes <= budget.maxGzipBytes;

    console.log(`${rawPassed && gzipPassed ? "PASS" : "FAIL"} ${budget.label}: ` + `${formatKib(rawBytes)} raw / ${formatKib(gzipBytes)} gzip ` + `(预算 ${formatKib(budget.maxRawBytes)} / ${formatKib(budget.maxGzipBytes)})`);

    if (!rawPassed || !gzipPassed) {
        failures.push(`${budget.label} 超出生产体积预算`);
    }
}

if (failures.length > 0) {
    throw new Error(`前端构建体积门禁失败：\n- ${failures.join("\n- ")}`);
}
