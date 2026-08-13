import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { AdminLayoutSettingsProvider, DEFAULT_ADMIN_LAYOUT_SETTINGS, useAdminLayoutSettings, type AdminLayoutStorage } from "../src/pages/admin/admin-layout-settings";
import { AdminThemeProvider } from "../src/pages/admin/admin-theme";
import { AdminLayoutSettingsDrawer } from "../src/pages/admin/components/admin-layout-settings-drawer";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    localStorage.clear();
});

describe("admin layout settings drawer", () => {
    test("updates all supported layout settings and restores the complete default", async () => {
        await renderDrawer(localStorage);

        await clickControl(".admin-layout-theme-dark");
        await clickControl(".admin-layout-menu-light");
        await clickControl(".admin-layout-sider-272");
        await clickControl(".admin-layout-content-fixed");
        await clickControl(".admin-layout-fixed-header");

        expect(settingsProbe()).toMatchObject({
            theme: "dark",
            menuTheme: "light",
            siderWidth: 272,
            contentWidth: "fixed",
            fixedHeader: false,
        });

        await clickControl(".admin-layout-reset-button");
        expect(settingsProbe()).toEqual(DEFAULT_ADMIN_LAYOUT_SETTINGS);
    });

    test("keeps invalid custom color input visible without changing the active brand color", async () => {
        await renderDrawer(localStorage);
        const input = requiredElement<HTMLInputElement>(".admin-layout-color-input");

        await setInputValue(input, "#12");

        expect(input.value).toBe("#12");
        expect(document.querySelector(".admin-layout-color-error")?.textContent).toContain("#RRGGBB");
        expect(settingsProbe().colorPrimary).toBe("#2979C9");

        await setInputValue(input, "#123abc");

        expect(settingsProbe().colorPrimary).toBe("#123ABC");
        expect(document.querySelector(".admin-layout-color-error")).toBeNull();
    });

    test("shows persistence failure as an explicit preview-only state", async () => {
        const failingStorage: AdminLayoutStorage = {
            getItem: () => null,
            setItem: () => {
                throw new Error("quota exceeded");
            },
            removeItem: () => undefined,
        };
        await renderDrawer(failingStorage);

        await clickControl(".admin-layout-theme-dark");

        expect(settingsProbe().theme).toBe("dark");
        const error = document.querySelector(".admin-layout-persistence-error");
        expect(error?.textContent).toContain("当前预览已生效");
        expect(error?.textContent).toContain("无法保存到此浏览器");
        expect(document.body.textContent).not.toContain("已保存");
    });

    test("renders one right-side responsive drawer with accessible controls", async () => {
        await renderDrawer(localStorage);

        expect(document.querySelectorAll(".admin-layout-settings-drawer")).toHaveLength(1);
        expect(document.querySelector(".admin-layout-settings-drawer .ant-drawer-content-wrapper")).not.toBeNull();
        expect(requiredElement<HTMLButtonElement>(".admin-layout-theme-light").getAttribute("aria-pressed")).toBe("true");
        expect(requiredElement<HTMLInputElement>(".admin-layout-color-input").getAttribute("aria-label")).toBe("自定义后台品牌色");
        expect(requiredElement<HTMLButtonElement>(".admin-layout-reset-button").textContent).toContain("恢复默认设置");
    });
});

function SettingsProbe() {
    const { settings } = useAdminLayoutSettings();
    return createElement("output", { className: "admin-layout-settings-probe" }, JSON.stringify(settings));
}

async function renderDrawer(storage: AdminLayoutStorage) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(AdminLayoutSettingsProvider, { storage }, createElement(AdminThemeProvider, null, createElement(AdminLayoutSettingsDrawer, { onClose: () => undefined, open: true }), createElement(SettingsProbe)))));
}

function settingsProbe() {
    const raw = document.querySelector(".admin-layout-settings-probe")?.textContent;
    if (!raw) throw new Error("后台布局设置探针不存在");
    return JSON.parse(raw) as typeof DEFAULT_ADMIN_LAYOUT_SETTINGS;
}

function requiredElement<T extends Element>(selector: string): T {
    const element = document.querySelector<T>(selector);
    if (!element) throw new Error(`找不到元素：${selector}`);
    return element;
}

async function clickControl(selector: string) {
    await act(async () => requiredElement<HTMLButtonElement>(selector).click());
}

async function setInputValue(input: HTMLInputElement, value: string) {
    await act(async () => {
        const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
        setter?.call(input, value);
        input.dispatchEvent(new Event("input", { bubbles: true }));
        input.dispatchEvent(new Event("change", { bubbles: true }));
    });
}
