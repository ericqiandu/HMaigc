import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

export type AdminThemeName = "light" | "dark";
export type AdminMenuTheme = "follow" | "light" | "dark";
export type AdminSiderWidth = 208 | 236 | 272;
export type AdminContentWidth = "fixed" | "fluid";

export type AdminLayoutSettings = {
    theme: AdminThemeName;
    menuTheme: AdminMenuTheme;
    colorPrimary: string;
    siderWidth: AdminSiderWidth;
    contentWidth: AdminContentWidth;
    fixedHeader: boolean;
};

export type AdminLayoutStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

type AdminLayoutSettingsContextValue = {
    settings: AdminLayoutSettings;
    persistenceError: string | null;
    updateSettings: (patch: Partial<AdminLayoutSettings>) => boolean;
    resetSettings: () => boolean;
};

export const ADMIN_LAYOUT_SETTINGS_STORAGE_KEY = "hmaigc:admin-layout-settings:v1";

export const DEFAULT_ADMIN_LAYOUT_SETTINGS: Readonly<AdminLayoutSettings> = Object.freeze({
    theme: "light",
    menuTheme: "follow",
    colorPrimary: "#2979C9",
    siderWidth: 236,
    contentWidth: "fluid",
    fixedHeader: true,
});

const ADMIN_LAYOUT_SETTINGS_PERSISTENCE_ERROR = "无法保存后台界面设置到此浏览器，当前预览仍然有效。";
const ADMIN_LAYOUT_SETTINGS_KEYS: ReadonlyArray<keyof AdminLayoutSettings> = ["theme", "menuTheme", "colorPrimary", "siderWidth", "contentWidth", "fixedHeader"];
const AdminLayoutSettingsContext = createContext<AdminLayoutSettingsContextValue | null>(null);

export function AdminLayoutSettingsProvider({ children, storage = window.localStorage }: { children: ReactNode; storage?: AdminLayoutStorage }) {
    const [settings, setSettings] = useState<AdminLayoutSettings>(() => readAdminLayoutSettings(storage));
    const [persistenceError, setPersistenceError] = useState<string | null>(null);

    const commitSettings = useCallback(
        (nextSettings: AdminLayoutSettings) => {
            setSettings(nextSettings);
            const persisted = persistAdminLayoutSettings(storage, nextSettings);
            setPersistenceError(persisted ? null : ADMIN_LAYOUT_SETTINGS_PERSISTENCE_ERROR);
            return persisted;
        },
        [storage],
    );

    const updateSettings = useCallback(
        (patch: Partial<AdminLayoutSettings>) => {
            const nextSettings = normalizeAdminLayoutSettings({ ...settings, ...patch });
            if (!nextSettings) return false;
            return commitSettings(nextSettings);
        },
        [commitSettings, settings],
    );

    const resetSettings = useCallback(() => commitSettings(copyDefaultSettings()), [commitSettings]);
    const value = useMemo<AdminLayoutSettingsContextValue>(() => ({ persistenceError, resetSettings, settings, updateSettings }), [persistenceError, resetSettings, settings, updateSettings]);

    return <AdminLayoutSettingsContext.Provider value={value}>{children}</AdminLayoutSettingsContext.Provider>;
}

export function useAdminLayoutSettings() {
    const value = useContext(AdminLayoutSettingsContext);
    if (!value) throw new Error("useAdminLayoutSettings 必须在 AdminLayoutSettingsProvider 内使用");
    return value;
}

export function readAdminLayoutSettings(storage: Pick<AdminLayoutStorage, "getItem">): AdminLayoutSettings {
    try {
        return parseAdminLayoutSettings(storage.getItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY));
    } catch {
        return copyDefaultSettings();
    }
}

export function parseAdminLayoutSettings(raw: string | null): AdminLayoutSettings {
    if (!raw) return copyDefaultSettings();
    try {
        const parsed: unknown = JSON.parse(raw);
        return normalizeAdminLayoutSettings(parsed) ?? copyDefaultSettings();
    } catch {
        return copyDefaultSettings();
    }
}

export function persistAdminLayoutSettings(storage: Pick<AdminLayoutStorage, "setItem">, settings: AdminLayoutSettings): boolean {
    try {
        storage.setItem(ADMIN_LAYOUT_SETTINGS_STORAGE_KEY, JSON.stringify(settings));
        return true;
    } catch {
        return false;
    }
}

export function isAdminBrandColor(value: string): boolean {
    return /^#[0-9A-F]{6}$/i.test(value);
}

export function resolveAdminMenuTheme(settings: Pick<AdminLayoutSettings, "menuTheme" | "theme">): AdminThemeName {
    return settings.menuTheme === "follow" ? settings.theme : settings.menuTheme;
}

function normalizeAdminLayoutSettings(value: unknown): AdminLayoutSettings | null {
    if (!isRecord(value)) return null;
    const keys = Object.keys(value);
    if (keys.length !== ADMIN_LAYOUT_SETTINGS_KEYS.length || !ADMIN_LAYOUT_SETTINGS_KEYS.every((key) => Object.hasOwn(value, key))) return null;
    if (value.theme !== "light" && value.theme !== "dark") return null;
    if (value.menuTheme !== "follow" && value.menuTheme !== "light" && value.menuTheme !== "dark") return null;
    if (typeof value.colorPrimary !== "string" || !isAdminBrandColor(value.colorPrimary)) return null;
    if (value.siderWidth !== 208 && value.siderWidth !== 236 && value.siderWidth !== 272) return null;
    if (value.contentWidth !== "fixed" && value.contentWidth !== "fluid") return null;
    if (typeof value.fixedHeader !== "boolean") return null;

    return {
        theme: value.theme,
        menuTheme: value.menuTheme,
        colorPrimary: value.colorPrimary.toUpperCase(),
        siderWidth: value.siderWidth,
        contentWidth: value.contentWidth,
        fixedHeader: value.fixedHeader,
    };
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}

function copyDefaultSettings(): AdminLayoutSettings {
    return { ...DEFAULT_ADMIN_LAYOUT_SETTINGS };
}
