import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasNodeContextMenu } from "../src/components/canvas/canvas-context-menu";
import { CanvasNodeType, type CanvasNodeData, type ContextMenuState } from "../src/types/canvas";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("canvas context menu contract", () => {
    test("replaces the canvas root menu with one shared add-node command surface", async () => {
        await renderContextMenu({ type: "canvas", x: 120, y: 120, position: { x: 10, y: 20 } });

        const rootMenu = requiredElement<HTMLElement>('[role="menu"]');
        expect(rootMenu.getAttribute("aria-label")).toBe("画布命令");
        expect(rootMenu.querySelectorAll('[role="menuitem"]').length).toBe(6);

        await act(async () => button("添加节点").click());

        expect(document.body.textContent).not.toContain("上传到这里");
        expect(document.body.textContent).toContain("添加资源");
        expect(document.querySelectorAll(".canvas-command-menu").length).toBe(1);
        expect(document.querySelectorAll('[role="menu"]').length).toBe(1);
    });

    test("repositions the taller add-node menu inside the viewport", async () => {
        Object.defineProperty(window, "innerHeight", { configurable: true, value: 600 });
        await renderContextMenu({ type: "canvas", x: 120, y: 520, position: { x: 10, y: 20 } });

        await act(async () => button("添加节点").click());

        expect(requiredElement<HTMLElement>(".canvas-context-submenu--add").style.top).toBe("106px");
    });

    test("uses the shared command roles for node and connection actions", async () => {
        const node: CanvasNodeData = {
            id: "text-1",
            type: CanvasNodeType.Text,
            title: "文案",
            position: { x: 0, y: 0 },
            width: 320,
            height: 180,
            metadata: { content: "测试内容" },
        };
        await renderContextMenu({ type: "node", x: 120, y: 120, nodeId: node.id }, node);

        expect(requiredElement<HTMLElement>('[role="menu"]').getAttribute("aria-label")).toBe("节点命令");
        expect(requiredElement<HTMLButtonElement>('[aria-label="删除节点"]').getAttribute("role")).toBe("menuitem");

        await renderContextMenu({ type: "connection", x: 120, y: 120, connectionId: "edge-1" });

        expect(requiredElement<HTMLElement>('[role="menu"]').getAttribute("aria-label")).toBe("连接命令");
        expect(requiredElement<HTMLButtonElement>('[aria-label="删除连接"]').getAttribute("role")).toBe("menuitem");
    });
});

async function renderContextMenu(menu: ContextMenuState, node?: CanvasNodeData) {
    if (root) await act(async () => root?.unmount());
    document.body.replaceChildren();
    const host = document.createElement("div");
    host.className = "canvas-context-menu-test-host dark";
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(createElement(CanvasNodeContextMenu, createContextMenuProps(menu, node)));
    });
}

function createContextMenuProps(menu: ContextMenuState, node?: CanvasNodeData): Parameters<typeof CanvasNodeContextMenu>[0] {
    return {
        menu,
        node,
        workspaceMode: "professional",
        isProjectLinked: false,
        canUndo: true,
        canRedo: false,
        canPaste: true,
        onClose: () => undefined,
        onAddNode: () => undefined,
        onAddVideoComposition: () => undefined,
        onOpenDirector: () => undefined,
        onUpload: () => undefined,
        onOpenAssets: () => undefined,
        onOpenProjectCharacters: () => undefined,
        onUndo: () => undefined,
        onRedo: () => undefined,
        onPaste: () => undefined,
        onCopyNode: () => undefined,
        onDuplicate: () => undefined,
        onDelete: () => undefined,
        onSaveAsset: () => undefined,
        onViewMedia: () => undefined,
        onEditText: () => undefined,
        onGenerateImage: () => undefined,
        onCopyContent: () => undefined,
        onCopyMediaUrl: () => undefined,
        onSetAssetCategory: () => undefined,
        onToggleFrame: () => undefined,
    };
}

function button(label: string) {
    const item = [...document.querySelectorAll<HTMLButtonElement>("button")].find((element) => element.textContent?.trim() === label);
    if (!item) throw new Error(`缺少按钮：${label}`);
    return item;
}

function requiredElement<ElementType extends Element>(selector: string) {
    const element = document.querySelector<ElementType>(selector);
    if (!element) throw new Error(`缺少测试元素：${selector}`);
    return element;
}
