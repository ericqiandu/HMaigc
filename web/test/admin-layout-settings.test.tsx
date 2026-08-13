import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import {
    ADMIN_LAYOUT_SETTINGS_STORAGE_KEY,
    AdminLayoutSettingsProvider,
    DEFAULT_ADMIN_LAYOUT_SETTINGS,
    isAdminBrandColor,
    parseAdminLayoutSettings,
    readAdminLayoutSettings,
    useAdminLayoutSettings,
    type AdminLayoutStorage,
} from "../src/pages/admin/admin-layout-settings";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    localStorage.clear();
});

describe("admin layout settings", () => {
    test("uses one strict versioned settings object and normalizes a valid brand color", () => {
        const stored = JSON.stringify({
            theme: "dark",
            menuTheme: "light",
            colorPrimary: "#123abc",
            siderWidth: 272,
            contentWidth: "fixed",
            fixedHeader: false,
        });

        expect(parseAdminLayoutSettings(stored)).toEqual({
            theme: "dark",
            menuTheme: "light",
            colorPrimary: "#123ABC",
            siderWidth: 272,
            contentWidth: "fixed",
            fixedHeader: false,
        });
        expect(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY).toBe("hmaigc:admin-layout-settings:v1");
        expect(isAdminBrandColor("#123ABC")).toBe(true);
        expect(isAdminBrandColor("123ABC")).toBe(false);
    });

    test("falls back to the complete default object when any stored field is invalid", () => {
        const invalidValues = [
            "not-json",
            JSON.stringify({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, menuTheme: "system" }),
            JSON.stringify({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, colorPrimary: "#12" }),
            JSON.stringify({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, siderWidth: 999 }),
            JSON.stringify({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, fixedHeader: "true" }),
        ];

        for (const invalidValue of invalidValues) {
            expect(parseAdminLayoutSettings(invalidValue)).toEqual(DEFAULT_ADMIN_LAYOUT_SETTINGS);
        }
    });

    test("falls back to defaults when browser storage cannot be read", () => {
        const unreadableStorage: AdminLayoutStorage = {
            getItem: () => {
                throw new Error("storage unavailable");
            },
            setItem: () => undefined,
            removeItem: () => undefined,
        };

        expect(readAdminLayoutSettings(unreadableStorage)).toEqual(DEFAULT_ADMIN_LAYOUT_SETTINGS);
    });

    test("keeps the in-memory preview and reports an explicit persistence error when storage writes fail", async () => {
        const failingStorage: AdminLayoutStorage = {
            getItem: () => null,
            setItem: () => {
                throw new Error("quota exceeded");
            },
            removeItem: () => undefined,
        };

        await renderProvider(failingStorage);
        const updateButton = document.querySelector<HTMLButtonElement>(".admin-layout-settings-update-probe");

        await act(async () => updateButton?.click());

        expect(document.querySelector(".admin-layout-settings-theme-probe")?.textContent).toBe("dark");
        expect(document.querySelector(".admin-layout-settings-error-probe")?.textContent).toContain("无法保存后台界面设置");
        expect(updateButton?.dataset.persisted).toBe("false");
    });

    test("persists updates and resets all settings without changing the creation theme", async () => {
        const creationTheme = JSON.stringify({ state: { theme: "dark" } });
        localStorage.setItem("infinite-canvas:theme_store", creationTheme);
        await renderProvider(localStorage);

        const updateButton = document.querySelector<HTMLButtonElement>(".admin-layout-settings-update-probe");
        const resetButton = document.querySelector<HTMLButtonElement>(".admin-layout-settings-reset-probe");
        await act(async () => updateButton?.click());

        expect(JSON.parse(localStorage.getItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY) ?? "null")).toMatchObject({
            theme: "dark",
            colorPrimary: "#123ABC",
            siderWidth: 272,
        });
        expect(updateButton?.dataset.persisted).toBe("true");
        expect(localStorage.getItem("infinite-canvas:theme_store")).toBe(creationTheme);

        await act(async () => resetButton?.click());

        expect(JSON.parse(localStorage.getItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY) ?? "null")).toEqual(DEFAULT_ADMIN_LAYOUT_SETTINGS);
        expect(document.querySelector(".admin-layout-settings-theme-probe")?.textContent).toBe("light");
        expect(resetButton?.dataset.persisted).toBe("true");
    });
});

function SettingsProbe() {
    const { persistenceError, resetSettings, settings, updateSettings } = useAdminLayoutSettings();
    return createElement(
        "div",
        { className: "admin-layout-settings-probe" },
        createElement("span", { className: "admin-layout-settings-theme-probe" }, settings.theme),
        createElement("span", { className: "admin-layout-settings-error-probe" }, persistenceError),
        createElement(
            "button",
            {
                className: "admin-layout-settings-update-probe",
                onClick: (event) => {
                    event.currentTarget.dataset.persisted = String(
                        updateSettings({
                            theme: "dark",
                            menuTheme: "dark",
                            colorPrimary: "#123ABC",
                            siderWidth: 272,
                            contentWidth: "fixed",
                            fixedHeader: false,
                        }),
                    );
                },
                type: "button",
            },
            "update",
        ),
        createElement(
            "button",
            {
                className: "admin-layout-settings-reset-probe",
                onClick: (event) => {
                    event.currentTarget.dataset.persisted = String(resetSettings());
                },
                type: "button",
            },
            "reset",
        ),
    );
}

async function renderProvider(storage: AdminLayoutStorage) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    await act(async () => root?.render(createElement(AdminLayoutSettingsProvider, { storage }, createElement(SettingsProbe))));
}
