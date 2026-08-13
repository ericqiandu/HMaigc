import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Image as ImageIcon, LoaderCircle, RefreshCw } from "lucide-react";
import { CanvasNodeAction, CanvasNodeEmptyState, CanvasNodeStatusLayout } from "../src/components/canvas/canvas-node-ui";
import { canvasThemes } from "../src/lib/canvas-theme";

describe("画布节点统一 UI", () => {
    test("共享进度态使用当前画布语义层级", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasNodeStatusLayout, {
                icon: createElement(LoaderCircle),
                title: "任务处理中",
                detail: "生成中 · 48%",
                progress: 48,
                meta: "8分38秒 · b29f...8614",
                tone: "progress",
                theme: canvasThemes.dark,
            }),
        );

        expect(markup).toContain("canvas-node-state");
        expect(markup).toContain("canvas-node-state-progress");
        expect(markup).toContain('role="status"');
        expect(markup).toContain('aria-valuenow="48"');
        expect(markup).not.toContain("rounded-full");
    });

    test("共享空态和操作复用同一节点结构", () => {
        const emptyMarkup = renderToStaticMarkup(
            createElement(CanvasNodeEmptyState, {
                icon: createElement(ImageIcon),
                title: "空图片节点",
                description: "连接参考素材或直接输入提示词",
                theme: canvasThemes.dark,
            }),
        );
        const actionMarkup = renderToStaticMarkup(
            createElement(CanvasNodeAction, {
                icon: createElement(RefreshCw),
                label: "重试",
                onClick: () => undefined,
            }),
        );

        expect(emptyMarkup).toContain("canvas-node-state-empty");
        expect(emptyMarkup).toContain("canvas-node-state-title");
        expect(actionMarkup).toContain("canvas-node-action-neutral");
        expect(actionMarkup).toContain('aria-label="重试"');
    });

    test("媒体与任务状态硬切到共享节点表现层", () => {
        const source = readFileSync(new URL("../src/components/canvas/canvas-node.tsx", import.meta.url), "utf8");

        expect(source).toContain("<CanvasNodeEmptyState");
        expect(source).toContain("<CanvasNodeStatusLayout");
        expect(source).toContain("<CanvasNodeAction");
        expect(source).not.toContain('className="canvas-audio-node-heading"');
        expect(source).not.toContain('className="canvas-video-composition-heading"');
        expect(source).not.toContain("canvas-node-shell--audio");
    });

    test("节点私有样式不再覆盖公共外壳", () => {
        const audioCSS = readFileSync(new URL("../src/components/canvas/canvas-audio-node.css", import.meta.url), "utf8");
        const compositionCSS = readFileSync(new URL("../src/components/canvas/canvas-video-composition-node.css", import.meta.url), "utf8");

        expect(audioCSS).not.toContain(".canvas-audio-node-heading");
        expect(audioCSS).not.toContain(".canvas-node-shell--audio");
        expect(compositionCSS).not.toContain(".canvas-video-composition-heading");
    });

    test("节点家族不维护私有主题色或嵌套卡片", () => {
        const source = readFileSync(new URL("../src/components/canvas/canvas-node.tsx", import.meta.url), "utf8");
        const audioCSS = readFileSync(new URL("../src/components/canvas/canvas-audio-node.css", import.meta.url), "utf8");
        const compositionCSS = readFileSync(new URL("../src/components/canvas/canvas-video-composition-node.css", import.meta.url), "utf8");
        const globalsCSS = readFileSync(new URL("../src/styles/globals.css", import.meta.url), "utf8");
        const actionRule = globalsCSS.slice(globalsCSS.indexOf(".canvas-node-action {"), globalsCSS.indexOf(".canvas-node-action-danger"));
        const actionHoverRule = globalsCSS.slice(globalsCSS.indexOf(".canvas-node-action:hover"), globalsCSS.indexOf(".canvas-node-action:focus-visible"));

        expect(audioCSS).not.toContain("#0071e3");
        expect(compositionCSS).not.toContain("#f87171");
        expect(source).not.toContain('className="thin-scrollbar mt-3 min-h-0 flex-1 overflow-hidden rounded-xl border');
        expect(actionRule).toContain("background: var(--workspace-surface);");
        expect(actionHoverRule).toContain("background: var(--bg-hover);");
        expect(actionRule).not.toContain("var(--workspace-ui-control");
    });

    test("图片、视频与音频提示词区复用节点主体表面色", () => {
        const source = readFileSync(new URL("../src/components/canvas/canvas-node-prompt-panel.tsx", import.meta.url), "utf8");
        const composerCSS = readFileSync(new URL("../src/components/canvas/canvas-media-composer.css", import.meta.url), "utf8");

        expect(source).toContain("background: theme.node.fill");
        expect(source).toContain("borderColor: theme.node.stroke");
        expect(source).not.toContain("background: theme.spatial.elevated");
        expect(composerCSS).not.toContain("background: color-mix(in srgb, var(--background) 94%, var(--foreground) 6%) !important;");
        expect(composerCSS).not.toContain("background: #ffffff !important;");
    });
});
