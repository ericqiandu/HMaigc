import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, type ComponentType, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";

type CommandListProps = {
    ariaLabel: string;
    children: ReactNode;
    className?: string;
    onEscape?: () => void;
};

type CommandItemProps = {
    label: string;
    disabled?: boolean;
    onSelect?: () => void;
};

const commandModule: Partial<{
    CanvasCommandList: ComponentType<CommandListProps>;
    CanvasCommandItem: ComponentType<CommandItemProps>;
}> = await import("../src/components/canvas/canvas-create-command-grid");

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("canvas command menu", () => {
    test("exposes menu semantics and invokes the selected command", async () => {
        let selections = 0;
        await renderMenu({ onFirstSelect: () => (selections += 1) });

        const menu = requiredElement<HTMLElement>('[role="menu"]');
        const items = menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]');

        expect(menu.getAttribute("aria-label")).toBe("画布命令");
        expect(items.length).toBe(4);
        expect(items[0]?.getAttribute("aria-label")).toBe("第一个");
        await act(async () => items[0]?.click());
        expect(selections).toBe(1);
    });

    test("moves focus with arrow, Home and End keys while skipping disabled commands", async () => {
        await renderMenu();
        const menu = requiredElement<HTMLElement>('[role="menu"]');
        const items = [...menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')];

        items[0]?.focus();
        dispatchMenuKey(menu, "ArrowDown");
        expect(document.activeElement).toBe(items[2]);

        dispatchMenuKey(menu, "End");
        expect(document.activeElement).toBe(items[3]);

        dispatchMenuKey(menu, "ArrowDown");
        expect(document.activeElement).toBe(items[0]);

        dispatchMenuKey(menu, "Home");
        expect(document.activeElement).toBe(items[0]);

        dispatchMenuKey(menu, "ArrowUp");
        expect(document.activeElement).toBe(items[3]);
    });

    test("delegates Escape to the overlay owner", async () => {
        let escapes = 0;
        await renderMenu({ onEscape: () => (escapes += 1) });

        dispatchMenuKey(requiredElement<HTMLElement>('[role="menu"]'), "Escape");

        expect(escapes).toBe(1);
    });
});

async function renderMenu(options: { onFirstSelect?: () => void; onEscape?: () => void } = {}) {
    const CanvasCommandList = commandModule.CanvasCommandList;
    const CanvasCommandItem = commandModule.CanvasCommandItem;
    expect(CanvasCommandList).toBeDefined();
    expect(CanvasCommandItem).toBeDefined();
    if (!CanvasCommandList || !CanvasCommandItem) return;

    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(
            createElement(
                CanvasCommandList,
                { ariaLabel: "画布命令", onEscape: options.onEscape },
                createElement(CanvasCommandItem, { label: "第一个", onSelect: options.onFirstSelect }),
                createElement(CanvasCommandItem, { label: "已禁用", disabled: true }),
                createElement(CanvasCommandItem, { label: "第三个" }),
                createElement(CanvasCommandItem, { label: "最后一个" }),
            ),
        );
    });
}

function dispatchMenuKey(menu: HTMLElement, key: string) {
    act(() => {
        menu.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
    });
}

function requiredElement<ElementType extends Element>(selector: string) {
    const element = document.querySelector<ElementType>(selector);
    if (!element) throw new Error(`缺少测试元素：${selector}`);
    return element;
}
