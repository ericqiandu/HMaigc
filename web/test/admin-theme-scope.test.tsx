import "./setup-happy-dom";

import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

let AdminThemeProvider: typeof import("../src/pages/admin/admin-theme").AdminThemeProvider;
let useAdminTheme: typeof import("../src/pages/admin/admin-theme").useAdminTheme;
let ADMIN_THEME_STORAGE_KEY: string;
let root: Root | null = null;

beforeAll(async () => {
    const module = await import("../src/pages/admin/admin-theme");
    AdminThemeProvider = module.AdminThemeProvider;
    useAdminTheme = module.useAdminTheme;
    ADMIN_THEME_STORAGE_KEY = module.ADMIN_THEME_STORAGE_KEY;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    localStorage.clear();
});

function ThemeProbe() {
    const { getPortalContainer, setTheme, theme } = useAdminTheme();
    return createElement(
        "button",
        {
            className: "admin-theme-probe",
            "data-portal-owner": getPortalContainer().className,
            onClick: () => setTheme(theme === "light" ? "dark" : "light"),
            type: "button",
        },
        theme,
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

        await act(async () => root?.render(createElement(AdminThemeProvider, null, createElement(ThemeProbe))));

        const button = document.querySelector<HTMLButtonElement>(".admin-theme-probe");
        expect(button?.textContent).toBe("light");
        expect(button?.dataset.portalOwner).toContain("admin-theme-root");
        expect(document.querySelector("[data-admin-theme='light']")).not.toBeNull();

        await act(async () => button?.click());

        expect(button?.textContent).toBe("dark");
        expect(document.querySelector("[data-admin-theme='dark']")).not.toBeNull();
        expect(localStorage.getItem(ADMIN_THEME_STORAGE_KEY)).toBe("dark");
        expect(localStorage.getItem("infinite-canvas:theme_store")).toBe(creationTheme);
    });

    test("falls back to light when the stored admin theme is invalid", async () => {
        localStorage.setItem(ADMIN_THEME_STORAGE_KEY, "system");
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => root?.render(createElement(AdminThemeProvider, null, createElement(ThemeProbe))));

        expect(document.querySelector(".admin-theme-probe")?.textContent).toBe("light");
    });
});
