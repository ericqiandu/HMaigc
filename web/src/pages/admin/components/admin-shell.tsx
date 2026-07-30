import { Drawer, Tooltip } from "antd";
import {
    ChevronRight,
    Home,
    Menu,
    PanelLeftClose,
    PanelLeftOpen,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";

import { AppChangelogButton } from "@/components/layout/app-changelog-modal";
import { WORKSPACE_SIDEBAR_STORAGE_KEY } from "@/components/layout/workspace-sidebar-state";
import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { cn } from "@/lib/utils";
import "@/styles/workspace-ui.css";
import "@/styles/workspace-shell.css";
import "../admin-workspace.css";
import { AdminNavigation, findAdminNavigationGroup, findAdminNavigationItem } from "./admin-navigation";

export function AdminShell() {
    const [collapsed, setCollapsed] = useState(() => window.localStorage.getItem(WORKSPACE_SIDEBAR_STORAGE_KEY) === "1");
    const { settings } = useSiteSettings();
    const toggleCollapsed = () => {
        setCollapsed((current) => {
            const next = !current;
            window.localStorage.setItem(WORKSPACE_SIDEBAR_STORAGE_KEY, next ? "1" : "0");
            return next;
        });
    };

    return (
        <main className="app-user-workspace admin-workspace workspace-ui-scope flex h-full min-h-0 overflow-hidden text-foreground">
            <aside className={cn("app-workspace-sidebar admin-sidebar hidden shrink-0 flex-col overflow-hidden lg:flex", collapsed ? "w-16" : "w-[236px]")}>
                <div className={cn("admin-sidebar-brand flex h-16 shrink-0 items-center", collapsed ? "justify-center" : "gap-2.5 px-4")}>
                    {!collapsed ? (
                        <Link to="/" className="flex min-w-0 flex-1 items-center gap-2" title={settings.siteName}>
                            <span className="admin-brand-mark grid size-8 shrink-0 place-items-center overflow-hidden rounded-md bg-foreground/[.06]">
                                <img className="admin-brand-image size-5 object-contain" src={siteLogoURL(settings)} alt="" />
                            </span>
                            <span className="admin-brand-copy min-w-0">
                                <span className="admin-brand-name block truncate text-sm font-semibold">{settings.siteName}</span>
                                <span className="admin-brand-caption block truncate text-[9px] font-medium tracking-[0.16em] text-foreground/38">ADMIN CONSOLE</span>
                            </span>
                        </Link>
                    ) : null}
                    <Tooltip title={collapsed ? "展开侧栏" : "折叠侧栏"} placement="right">
                        <button type="button" className="app-workspace-icon-button shrink-0" onClick={toggleCollapsed} aria-label={collapsed ? "展开侧栏" : "折叠侧栏"}>
                            {collapsed ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
                        </button>
                    </Tooltip>
                </div>
                <AdminNavigation collapsed={collapsed} />
                <AdminNavigationFooter collapsed={collapsed} />
            </aside>
            <section className="admin-workspace-main flex min-w-0 flex-1 flex-col overflow-hidden">
                <MobileAdminNavigation />
                <Outlet />
            </section>
        </main>
    );
}

export function AdminPageFrame({ title, description, actions, children }: { title: string; description: string; actions?: ReactNode; children: ReactNode }) {
    const location = useLocation();
    const currentGroup = findAdminNavigationGroup(location.pathname)?.label ?? "管理";

    return (
        <main className="admin-page thin-scrollbar h-full overflow-y-auto">
            <div className="admin-page-frame mx-auto w-full">
                <header className="admin-page-header flex flex-wrap justify-between">
                    <div className="admin-page-heading min-w-0">
                        <div className="admin-page-breadcrumb flex items-center">
                            <span className="admin-page-breadcrumb-root">管理后台</span>
                            <ChevronRight className="admin-page-breadcrumb-separator size-3" />
                            <span className="admin-page-breadcrumb-current">{currentGroup}</span>
                        </div>
                        <h1 className="admin-page-title">{title}</h1>
                        <p className="admin-page-description max-w-2xl">{description}</p>
                    </div>
                    {actions ? <div className="admin-page-actions flex shrink-0 items-center">{actions}</div> : null}
                </header>
                <div className="admin-page-content">{children}</div>
            </div>
        </main>
    );
}

function MobileAdminNavigation() {
    const [open, setOpen] = useState(false);
    const location = useLocation();
    const { settings } = useSiteSettings();
    const currentItem = findAdminNavigationItem(location.pathname);

    return (
        <>
            <header className="admin-mobile-header flex shrink-0 items-center justify-between lg:hidden">
                <Link to="/" className="admin-mobile-brand flex min-w-0 items-center" title={settings.siteName}>
                    <span className="admin-mobile-brand-mark grid shrink-0 place-items-center overflow-hidden">
                        <img className="admin-mobile-brand-image object-contain" src={siteLogoURL(settings)} alt="" />
                    </span>
                    <span className="admin-mobile-brand-copy min-w-0">
                        <span className="admin-mobile-brand-name block truncate">{settings.siteName}</span>
                        <span className="admin-mobile-page-name block truncate">{currentItem?.label ?? "管理后台"}</span>
                    </span>
                </Link>
                <button type="button" className="admin-mobile-menu-button app-workspace-icon-button" onClick={() => setOpen(true)} aria-label="打开管理后台导航" aria-expanded={open}>
                    <Menu className="admin-mobile-menu-icon size-4" />
                </button>
            </header>
            <Drawer
                rootClassName="admin-mobile-navigation-drawer workspace-ui-scope"
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
                <AppChangelogButton
                    className={cn("admin-sidebar-footer-action flex w-full items-center transition-colors", collapsed ? "justify-center px-0" : "gap-2 px-2.5")}
                    showVersion={!collapsed}
                />
            </Tooltip>
            <Tooltip title={collapsed ? "返回创作台" : undefined} placement="right">
                <NavLink to="/canvas" onClick={onNavigate} className={cn("admin-sidebar-footer-action flex items-center transition-colors", collapsed ? "justify-center px-0" : "gap-2 px-2.5")}>
                    <Home className="admin-sidebar-footer-icon size-3.5" />
                    {!collapsed ? <span className="admin-sidebar-footer-label">返回创作台</span> : null}
                </NavLink>
            </Tooltip>
        </div>
    );
}
