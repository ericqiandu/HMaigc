import { Drawer, Tooltip } from "antd";
import { ChevronRight, Home, Menu, PanelLeftClose, PanelLeftOpen, Settings2 } from "lucide-react";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link, NavLink, Outlet, useLocation } from "react-router";

import { AppChangelogButton } from "@/components/layout/app-changelog-modal";
import { WORKSPACE_SIDEBAR_STORAGE_KEY } from "@/components/layout/workspace-sidebar-state";
import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { cn } from "@/lib/utils";
import "@/styles/workspace-ui.css";
import "@/styles/workspace-shell.css";
import "../admin-workspace.css";
import "../admin-domain-workspace.css";
import "../admin-feature-workspace.css";
import "../admin-responsive.css";
import "../admin-art-layout.css";
import "../admin-navigation-layout.css";
import { useAdminTheme } from "../admin-theme";
import { AdminLayoutSettingsDrawer } from "./admin-layout-settings-drawer";
import { AdminNavigation, findAdminNavigationGroup, findAdminNavigationItem } from "./admin-navigation";
import { AdminModelCenterTabs } from "./admin-model-center-tabs";

const AdminPageActionTargetContext = createContext<HTMLElement | null>(null);

export function AdminShell() {
    const [collapsed, setCollapsed] = useState(() => window.localStorage.getItem(WORKSPACE_SIDEBAR_STORAGE_KEY) === "1");
    const [settingsOpen, setSettingsOpen] = useState(false);
    const { settings } = useSiteSettings();
    const toggleCollapsed = () => {
        setCollapsed((current) => {
            const next = !current;
            window.localStorage.setItem(WORKSPACE_SIDEBAR_STORAGE_KEY, next ? "1" : "0");
            return next;
        });
    };

    return (
        <div className="app-user-workspace admin-workspace workspace-ui-scope flex h-full min-h-0 overflow-hidden text-foreground">
            <a className="admin-skip-link" href="#admin-main-content">
                跳到主要内容
            </a>
            <aside className={cn("app-workspace-sidebar admin-sidebar hidden shrink-0 flex-col overflow-hidden xl:flex", collapsed ? "w-16" : "w-[236px]")}>
                <div className={cn("admin-sidebar-brand flex h-16 shrink-0 items-center", collapsed ? "justify-center" : "gap-2.5 px-4")}>
                    <Link to="/" className={cn("admin-brand-link flex min-w-0 items-center", collapsed ? "justify-center" : "flex-1 gap-2.5")} title={settings.siteName}>
                        <span className="admin-brand-mark grid size-8 shrink-0 place-items-center overflow-hidden rounded-md bg-foreground/[.06]">
                            <img className="admin-brand-image size-5 object-contain" src={siteLogoURL(settings)} alt="" />
                        </span>
                        {!collapsed ? (
                            <span className="admin-brand-copy min-w-0">
                                <span className="admin-brand-name block truncate text-sm font-semibold">{settings.siteName}</span>
                                <span className="admin-brand-caption block truncate text-[11px] font-medium tracking-[0.08em] text-foreground/45">ADMIN CONSOLE</span>
                            </span>
                        ) : null}
                    </Link>
                </div>
                <AdminNavigation collapsed={collapsed} />
                <AdminNavigationFooter collapsed={collapsed} />
            </aside>
            <section className="admin-workspace-main flex min-w-0 flex-1 flex-col overflow-hidden">
                <MobileAdminNavigation onOpenSettings={() => setSettingsOpen(true)} settingsOpen={settingsOpen} />
                <AdminDesktopHeader collapsed={collapsed} onOpenSettings={() => setSettingsOpen(true)} onToggleCollapsed={toggleCollapsed} settingsOpen={settingsOpen} />
                <Outlet />
            </section>
            <AdminLayoutSettingsDrawer open={settingsOpen} onClose={() => setSettingsOpen(false)} />
        </div>
    );
}

function AdminDesktopHeader({ collapsed, onOpenSettings, onToggleCollapsed, settingsOpen }: { collapsed: boolean; onOpenSettings: () => void; onToggleCollapsed: () => void; settingsOpen: boolean }) {
    const location = useLocation();
    const currentGroup = findAdminNavigationGroup(location.pathname)?.label ?? "管理后台";
    const currentItem = findAdminNavigationItem(location.pathname)?.label ?? "管理";

    return (
        <header className="admin-desktop-header hidden shrink-0 items-center justify-between xl:flex">
            <div className="admin-desktop-header-leading flex min-w-0 items-center">
                <Tooltip title={collapsed ? "展开侧栏" : "折叠侧栏"} placement="bottom">
                    <button type="button" className="admin-desktop-collapse-button app-workspace-icon-button shrink-0" onClick={onToggleCollapsed} aria-label={collapsed ? "展开侧栏" : "折叠侧栏"}>
                        {collapsed ? <PanelLeftOpen className="admin-desktop-collapse-icon size-4" /> : <PanelLeftClose className="admin-desktop-collapse-icon size-4" />}
                    </button>
                </Tooltip>
                <div className="admin-desktop-location flex min-w-0 items-center" aria-label="当前位置">
                    <span className="admin-desktop-location-group truncate">{currentGroup}</span>
                    <ChevronRight className="admin-desktop-location-separator size-3" aria-hidden="true" />
                    <span className="admin-desktop-location-current truncate">{currentItem}</span>
                </div>
            </div>
            <div className="admin-desktop-header-actions flex items-center">
                <Tooltip title="界面设置" placement="bottom">
                    <button type="button" className="admin-layout-settings-trigger app-workspace-icon-button" aria-label="打开后台界面设置" aria-expanded={settingsOpen} onClick={onOpenSettings}>
                        <Settings2 className="admin-layout-settings-trigger-icon size-4" />
                    </button>
                </Tooltip>
                <NavLink to="/canvas" className="admin-desktop-return-link flex items-center">
                    <Home className="admin-desktop-return-icon size-3.5" />
                    <span className="admin-desktop-return-label">返回创作台</span>
                </NavLink>
            </div>
        </header>
    );
}

export function AdminPageFrame({ title, description, actions, modelCenter = false, children }: { title: string; description: string; actions?: ReactNode; modelCenter?: boolean; children: ReactNode }) {
    const [actionTarget, setActionTarget] = useState<HTMLDivElement | null>(null);

    return (
        <AdminPageActionTargetContext.Provider value={actionTarget}>
            <main id="admin-main-content" className="admin-page thin-scrollbar h-full overflow-y-auto" tabIndex={-1}>
                <div className="admin-page-frame mx-auto w-full">
                    <header className="admin-page-header flex flex-wrap justify-between">
                        <div className="admin-page-heading min-w-0">
                            <h1 className="admin-page-title">{title}</h1>
                            <p className="admin-page-description max-w-2xl">{description}</p>
                        </div>
                        <div ref={setActionTarget} className="admin-page-actions flex shrink-0 items-center">
                            {actions}
                        </div>
                    </header>
                    {modelCenter ? <AdminModelCenterTabs /> : null}
                    <div className="admin-page-content">{children}</div>
                </div>
            </main>
        </AdminPageActionTargetContext.Provider>
    );
}

export function AdminPageActions({ children }: { children: ReactNode }) {
    const target = useContext(AdminPageActionTargetContext);
    return target ? createPortal(children, target) : null;
}

function MobileAdminNavigation({ onOpenSettings, settingsOpen }: { onOpenSettings: () => void; settingsOpen: boolean }) {
    const [open, setOpen] = useState(false);
    const location = useLocation();
    const { getPortalContainer } = useAdminTheme();
    const { settings } = useSiteSettings();
    const currentItem = findAdminNavigationItem(location.pathname);

    useEffect(() => {
        setOpen(false);
    }, [location.pathname]);

    useEffect(() => {
        const desktopBreakpoint = window.matchMedia("(min-width: 1280px)");
        const closeAtDesktopBreakpoint = (event: MediaQueryListEvent) => {
            if (event.matches) {
                setOpen(false);
            }
        };

        desktopBreakpoint.addEventListener("change", closeAtDesktopBreakpoint);
        return () => desktopBreakpoint.removeEventListener("change", closeAtDesktopBreakpoint);
    }, []);

    return (
        <>
            <header className="admin-mobile-header flex shrink-0 items-center justify-between xl:hidden">
                <Link to="/" className="admin-mobile-brand flex min-w-0 items-center" title={settings.siteName}>
                    <span className="admin-mobile-brand-mark grid shrink-0 place-items-center overflow-hidden">
                        <img className="admin-mobile-brand-image object-contain" src={siteLogoURL(settings)} alt="" />
                    </span>
                    <span className="admin-mobile-brand-copy min-w-0">
                        <span className="admin-mobile-brand-name block truncate">{settings.siteName}</span>
                        <span className="admin-mobile-page-name block truncate">{currentItem?.label ?? "管理后台"}</span>
                    </span>
                </Link>
                <div className="admin-mobile-header-actions flex items-center">
                    <button type="button" className="admin-layout-settings-trigger app-workspace-icon-button" aria-label="打开后台界面设置" aria-expanded={settingsOpen} onClick={onOpenSettings}>
                        <Settings2 className="admin-layout-settings-trigger-icon size-4" />
                    </button>
                    <button type="button" className="admin-mobile-menu-button app-workspace-icon-button" onClick={() => setOpen(true)} aria-label="打开管理后台导航" aria-expanded={open}>
                        <Menu className="admin-mobile-menu-icon size-4" />
                    </button>
                </div>
            </header>
            <Drawer
                rootClassName="admin-mobile-navigation-drawer"
                title={
                    <div className="admin-mobile-drawer-title">
                        <span className="admin-mobile-drawer-title-primary">管理后台</span>
                        <span className="admin-mobile-drawer-title-secondary">{settings.siteName}</span>
                    </div>
                }
                placement="left"
                width={320}
                open={open}
                onClose={() => setOpen(false)}
                getContainer={getPortalContainer}
                destroyOnHidden
            >
                <div className="admin-mobile-drawer-body flex h-full min-h-0 flex-col">
                    <AdminNavigation collapsed={false} onNavigate={() => setOpen(false)} />
                    <AdminNavigationFooter collapsed={false} onNavigate={() => setOpen(false)} />
                </div>
            </Drawer>
        </>
    );
}

function AdminNavigationFooter({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
    return (
        <div className="admin-sidebar-footer shrink-0">
            <Tooltip title={collapsed ? "更新日志" : undefined} placement="right">
                <AppChangelogButton className={cn("admin-sidebar-footer-action flex w-full items-center transition-colors", collapsed ? "justify-center px-0" : "gap-2 px-2.5")} showVersion={!collapsed} />
            </Tooltip>
            {onNavigate ? (
                <NavLink to="/canvas" onClick={onNavigate} className="admin-sidebar-footer-action flex items-center gap-2 px-2.5 transition-colors">
                    <Home className="admin-sidebar-footer-icon size-3.5" />
                    <span className="admin-sidebar-footer-label">返回创作台</span>
                </NavLink>
            ) : null}
        </div>
    );
}
