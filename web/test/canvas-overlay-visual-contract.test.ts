import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const overlayStyles = readFileSync(new URL("../src/styles/canvas-overlays.css", import.meta.url), "utf8");

afterEach(() => {
    document.head.replaceChildren();
    document.body.replaceChildren();
});

describe("canvas overlay visual contract", () => {
    test.each([
        { theme: "dark", background: "#262626", border: "#363636" },
        { theme: "light", background: "#ffffff", border: "#e5e7eb" },
    ])("renders the approved $theme compact command surface", ({ theme, background, border }) => {
        const { menu, item, icon } = mountCommandMenu(theme);

        expect(getComputedStyle(menu).width).toBe("196px");
        expect(getComputedStyle(menu).padding).toBe("8px");
        expect(getComputedStyle(menu).borderRadius).toBe("16px");
        expect(getComputedStyle(menu).backgroundColor).toBe(background);
        expect(getComputedStyle(menu).borderColor).toBe(border);
        expect(getComputedStyle(item).height).toBe("32px");
        expect(getComputedStyle(item).borderRadius).toBe("8px");
        expect(getComputedStyle(item).fontSize).toBe("13px");
        expect(getComputedStyle(item).lineHeight).toBe("20px");
        expect(getComputedStyle(icon).width).toBe("14px");
        expect(getComputedStyle(icon).height).toBe("14px");
    });
});

function mountCommandMenu(theme: string) {
    const style = document.createElement("style");
    style.className = "canvas-overlay-test-styles";
    style.textContent = overlayStyles;
    document.head.append(style);

    const themeRoot = document.createElement("div");
    themeRoot.className = `canvas-overlay-test-theme ${theme}`;
    const menu = document.createElement("div");
    menu.className = "canvas-overlay-panel canvas-command-menu";
    const item = document.createElement("button");
    item.className = "canvas-command-item";
    const iconSlot = document.createElement("span");
    iconSlot.className = "canvas-command-item-icon";
    const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    icon.classList.add("canvas-overlay-test-icon");
    iconSlot.append(icon);
    item.append(iconSlot);
    menu.append(item);
    themeRoot.append(menu);
    document.body.append(themeRoot);

    return { menu, item, icon };
}
