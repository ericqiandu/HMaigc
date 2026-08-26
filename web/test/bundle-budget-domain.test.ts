import { describe, expect, test } from "bun:test";

import { collectJavaScriptImportClosure, collectStaticAssetClosure, evaluateJavaScriptRequestBudget } from "../scripts/bundle-budget-domain.mjs";

describe("bundle budget import closure", () => {
    test("counts every unique JavaScript dependency once and excludes non-JavaScript assets", () => {
        const manifest = {
            "index.html": { file: "assets/index.js", imports: ["shared", "feature"] },
            shared: { file: "assets/shared.js", imports: ["react"] },
            feature: { file: "assets/feature.js", imports: ["shared", "feature.css"] },
            react: { file: "assets/react.js", imports: ["shared"] },
            "feature.css": { file: "assets/feature.css" },
        };

        expect(collectJavaScriptImportClosure(manifest, ["index.html"])).toEqual(["assets/feature.js", "assets/index.js", "assets/react.js", "assets/shared.js"]);
    });

    test("combines a route entry with the application entry without double-counting shared chunks", () => {
        const manifest = {
            "index.html": { file: "assets/index.js", imports: ["shared"] },
            "src/pages/home/index.tsx": { file: "assets/home.js", imports: ["shared"] },
            shared: { file: "assets/shared.js" },
        };

        expect(collectJavaScriptImportClosure(manifest, ["index.html", "src/pages/home/index.tsx"])).toEqual(["assets/home.js", "assets/index.js", "assets/shared.js"]);
    });

    test("reports assets pulled into a route through static imports without counting dynamic features", () => {
        const manifest = {
            "src/pages/canvas/project.tsx": {
                file: "assets/project.js",
                imports: ["canvas-shared"],
                dynamicImports: ["video-export"],
                assets: ["assets/project-font.woff2"],
            },
            "canvas-shared": {
                file: "assets/canvas-shared.js",
                assets: ["assets/canvas-texture.webp"],
            },
            "video-export": {
                file: "assets/video-export.js",
                assets: ["assets/ffmpeg-core.wasm"],
            },
        };

        expect(collectStaticAssetClosure(manifest, ["src/pages/canvas/project.tsx"])).toEqual(["assets/canvas-texture.webp", "assets/project-font.woff2"]);
    });
});

describe("bundle budget request count", () => {
    test("fails only when a JavaScript closure exceeds its request budget", () => {
        expect(evaluateJavaScriptRequestBudget(["assets/index.js", "assets/shared.js"], 2)).toEqual({
            requestCount: 2,
            passed: true,
        });
        expect(evaluateJavaScriptRequestBudget(["assets/index.js", "assets/shared.js", "assets/feature.js"], 2)).toEqual({
            requestCount: 3,
            passed: false,
        });
    });
});
