import "./setup-happy-dom";

import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

let AdminThemeProvider: typeof import("../src/pages/admin/admin-theme").AdminThemeProvider;
let useAdminTheme: typeof import("../src/pages/admin/admin-theme").useAdminTheme;
let AdminLayoutSettingsProvider: typeof import("../src/pages/admin/admin-layout-settings").AdminLayoutSettingsProvider;
let useAdminLayoutSettings: typeof import("../src/pages/admin/admin-layout-settings").useAdminLayoutSettings;
let ADMIN_LAYOUT_SETTINGS_STORAGE_KEY: string;
let root: Root | null = null;

beforeAll(async () => {
    const module = await import("../src/pages/admin/admin-theme");
    const settingsModule = await import("../src/pages/admin/admin-layout-settings");
    AdminThemeProvider = module.AdminThemeProvider;
    useAdminTheme = module.useAdminTheme;
    AdminLayoutSettingsProvider = settingsModule.AdminLayoutSettingsProvider;
    useAdminLayoutSettings = settingsModule.useAdminLayoutSettings;
    ADMIN_LAYOUT_SETTINGS_STORAGE_KEY = settingsModule.ADMIN_LAYOUT_SETTINGS_STORAGE_KEY;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    localStorage.clear();
});

function ThemeProbe() {
    const { getPortalContainer } = useAdminTheme();
    const { settings, updateSettings } = useAdminLayoutSettings();
    return createElement(
        "button",
        {
            className: "admin-theme-probe",
            "data-portal-owner": getPortalContainer().className,
            onClick: () => updateSettings({ theme: settings.theme === "light" ? "dark" : "light", colorPrimary: "#123ABC" }),
            type: "button",
        },
        settings.theme,
    );
}

describe("admin theme scope", () => {
    test("does not let the creation dark class override the independent admin theme", async () => {
        const adminStyles = await Bun.file(new URL("../src/pages/admin/admin-art-layout.css", import.meta.url)).text();
        const themeStyles = await Bun.file(new URL("../src/pages/admin/admin-workspace.css", import.meta.url)).text();
        expect(adminStyles).not.toContain(".dark .admin-workspace");
        expect(adminStyles).toContain('.admin-theme-root[data-admin-theme="dark"] .admin-workspace');
        expect(themeStyles.trimStart()).toStartWith('.admin-theme-root[data-admin-theme="light"]');
    });

    test("defaults to light and never mutates the creation theme preference", async () => {
        const creationTheme = JSON.stringify({ state: { theme: "dark" } });
        localStorage.setItem("infinite-canvas:theme_store", creationTheme);
        const host = document.createElement("div");
        host.className = "admin-theme-test-host";
        document.body.append(host);
        root = createRoot(host);

        await renderThemeProbe();

        const button = document.querySelector<HTMLButtonElement>(".admin-theme-probe");
        expect(button?.textContent).toBe("light");
        expect(button?.dataset.portalOwner).toContain("admin-theme-root");
        expect(document.querySelector("[data-admin-theme='light']")).not.toBeNull();

        await act(async () => button?.click());

        expect(button?.textContent).toBe("dark");
        expect(document.querySelector("[data-admin-theme='dark']")).not.toBeNull();
        expect(document.querySelector<HTMLElement>(".admin-theme-root")?.style.getPropertyValue("--admin-color-primary")).toBe("#123ABC");
        expect(JSON.parse(localStorage.getItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY) ?? "null")).toMatchObject({ colorPrimary: "#123ABC", theme: "dark" });
        expect(localStorage.getItem("infinite-canvas:theme_store")).toBe(creationTheme);
    });

    test("falls back to light when the stored layout settings are invalid", async () => {
        localStorage.setItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY, JSON.stringify({ theme: "system" }));
        await renderThemeProbe();

        expect(document.querySelector(".admin-theme-probe")?.textContent).toBe("light");
    });
});

async function renderThemeProbe() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    await act(async () => root?.render(createElement(AdminLayoutSettingsProvider, null, createElement(AdminThemeProvider, null, createElement(ThemeProbe)))));
}
