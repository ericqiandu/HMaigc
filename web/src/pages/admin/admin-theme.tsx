import { App, ConfigProvider, type ThemeConfig } from "antd";
import zhCN from "antd/locale/zh_CN";
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

import { getAntThemeConfig } from "@/lib/app-theme";

export type AdminThemeName = "light" | "dark";

export const ADMIN_THEME_STORAGE_KEY = "hmaigc:admin-theme";

type AdminThemeContextValue = {
    getPortalContainer: () => HTMLElement;
    setTheme: (theme: AdminThemeName) => void;
    theme: AdminThemeName;
};

const AdminThemeContext = createContext<AdminThemeContextValue | null>(null);

export function readAdminTheme(storage: Pick<Storage, "getItem">): AdminThemeName {
    const stored = storage.getItem(ADMIN_THEME_STORAGE_KEY);
    return stored === "dark" || stored === "light" ? stored : "light";
}

export function AdminThemeProvider({ children }: { children: ReactNode }) {
    const [rootElement, setRootElement] = useState<HTMLDivElement | null>(null);
    const [theme, setThemeState] = useState<AdminThemeName>(() => readAdminTheme(window.localStorage));
    const getPortalContainer = useCallback(() => rootElement ?? document.body, [rootElement]);
    const setTheme = useCallback((nextTheme: AdminThemeName) => {
        window.localStorage.setItem(ADMIN_THEME_STORAGE_KEY, nextTheme);
        setThemeState(nextTheme);
    }, []);
    const value = useMemo<AdminThemeContextValue>(() => ({ getPortalContainer, setTheme, theme }), [getPortalContainer, setTheme, theme]);

    return (
        <div ref={setRootElement} className="admin-theme-root" data-admin-theme={theme}>
            {rootElement ? (
                <AdminThemeContext.Provider value={value}>
                    <ConfigProvider locale={zhCN} getPopupContainer={getPortalContainer} theme={adminAntTheme(theme)}>
                        <App className="admin-theme-app">{children}</App>
                    </ConfigProvider>
                </AdminThemeContext.Provider>
            ) : null}
        </div>
    );
}

export function useAdminTheme() {
    const value = useContext(AdminThemeContext);
    if (!value) throw new Error("useAdminTheme 必须在 AdminThemeProvider 内使用");
    return value;
}

function adminAntTheme(theme: AdminThemeName): ThemeConfig {
    const dark = theme === "dark";
    const base = getAntThemeConfig(dark);
    const brand = dark ? "#7ccbff" : "#2979c9";

    return {
        ...base,
        cssVar: { key: `hmaigc-admin-${theme}` },
        token: {
            ...base.token,
            borderRadiusLG: 8,
            colorBgContainer: dark ? "#1b212b" : "#ffffff",
            colorBgLayout: dark ? "#0b0f14" : "#f5f7fa",
            colorInfo: brand,
            colorLink: brand,
            colorPrimary: brand,
        },
    };
}
