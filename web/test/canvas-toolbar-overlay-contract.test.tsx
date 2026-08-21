import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasToolbar } from "../src/components/canvas/canvas-toolbar";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("canvas toolbar overlay contract", () => {
    test("opens add-node with the shared command surface and no inline surface theme", async () => {
        await renderToolbar();

        await act(async () => requiredElement<HTMLButtonElement>('[aria-label="添加节点"]').click());

        const panel = requiredElement<HTMLElement>(".canvas-create-panel");
        expect(panel.classList.contains("canvas-command-menu")).toBe(true);
        expect(panel.style.backgroundColor).toBe("");
        expect(panel.style.borderColor).toBe("");
        expect(panel.style.boxShadow).toBe("");
        expect(panel.querySelectorAll('[role="menu"]').length).toBe(1);
        expect(panel.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(8);
    });

    test("keeps the appearance controls on the shared content-panel shell", async () => {
        await renderToolbar();

        await act(async () => requiredElement<HTMLButtonElement>('[aria-label="画布外观"]').click());

        const panel = requiredElement<HTMLElement>(".canvas-appearance-panel");
        expect(panel.classList.contains("canvas-overlay-panel")).toBe(true);
        expect(panel.classList.contains("canvas-command-menu")).toBe(false);
        expect(panel.textContent).toContain("主题模式");
        expect(panel.textContent).toContain("空间网格");
    });
});

async function renderToolbar() {
    const host = document.createElement("div");
    host.className = "canvas-toolbar-test-host dark";
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(CanvasToolbar, toolbarProps())));
}

function toolbarProps(): Parameters<typeof CanvasToolbar>[0] {
    const noop = () => undefined;
    return {
        selectedCount: 0,
        workspaceMode: "professional",
        isProjectLinked: false,
        canUndo: true,
        canRedo: true,
        backgroundMode: "dots",
        showImageInfo: true,
        onAddImage: noop,
        onAddVideo: noop,
        onAddVideoComposition: noop,
        onAddAudio: noop,
        onAddText: noop,
        onChooseStyle: noop,
        onAddScript: noop,
        onAddFrame: noop,
        onAddConfig: noop,
        onOpenDirector: noop,
        onUndo: noop,
        onRedo: noop,
        onUpload: noop,
        onDelete: noop,
        onClear: noop,
        onDeselect: noop,
        onBackgroundModeChange: noop,
        onShowImageInfoChange: noop,
        onOpenMyAssets: noop,
        onOpenProjectCharacters: noop,
    };
}

function requiredElement<ElementType extends Element>(selector: string) {
    const element = document.querySelector<ElementType>(selector);
    if (!element) throw new Error(`缺少测试元素：${selector}`);
    return element;
}
