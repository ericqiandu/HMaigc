import { App, ConfigProvider, type ThemeConfig } from "antd";
import zhCN from "antd/locale/zh_CN";
import { createContext, useCallback, useContext, useMemo, useState, type CSSProperties, type ReactNode } from "react";

import { getAntThemeConfig } from "@/lib/app-theme";
import { resolveAdminMenuTheme, useAdminLayoutSettings, type AdminThemeName } from "./admin-layout-settings";

type AdminThemeContextValue = {
    getPortalContainer: () => HTMLElement;
};

type AdminThemeRootStyle = CSSProperties & {
    "--admin-color-primary": string;
};

const AdminThemeContext = createContext<AdminThemeContextValue | null>(null);

export function AdminThemeProvider({ children }: { children: ReactNode }) {
    const [rootElement, setRootElement] = useState<HTMLDivElement | null>(null);
    const { settings } = useAdminLayoutSettings();
    const getPortalContainer = useCallback(() => rootElement ?? document.body, [rootElement]);
    const value = useMemo<AdminThemeContextValue>(() => ({ getPortalContainer }), [getPortalContainer]);
    const rootStyle: AdminThemeRootStyle = { "--admin-color-primary": settings.colorPrimary };

    return (
        <div ref={setRootElement} className="admin-theme-root" data-admin-theme={settings.theme} data-admin-menu-theme={resolveAdminMenuTheme(settings)} style={rootStyle}>
            {rootElement ? (
                <AdminThemeContext.Provider value={value}>
                    <ConfigProvider locale={zhCN} getPopupContainer={getPortalContainer} theme={adminAntTheme(settings.theme, settings.colorPrimary)}>
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

function adminAntTheme(theme: AdminThemeName, colorPrimary: string): ThemeConfig {
    const dark = theme === "dark";
    const base = getAntThemeConfig(dark);

    return {
        ...base,
        cssVar: { key: `hmaigc-admin-${theme}` },
        token: {
            ...base.token,
            borderRadiusLG: 8,
            colorBgContainer: dark ? "#1b212b" : "#ffffff",
            colorBgLayout: dark ? "#0b0f14" : "#f5f7fa",
            colorInfo: colorPrimary,
            colorLink: colorPrimary,
            colorPrimary,
        },
    };
}
