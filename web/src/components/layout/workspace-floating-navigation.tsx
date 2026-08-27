import { useEffect } from "react";
import { Home } from "lucide-react";
import { NavLink } from "react-router";

import { navigationTools } from "@/constant/navigation-tools";
import { prefetchRouteModule, scheduleRouteModulePrefetches, type RoutePrefetchScheduler } from "@/lib/route-module-prefetch";
import { cn } from "@/lib/utils";
import { useUserStore } from "@/stores/use-user-store";

export function WorkspaceFloatingNavigation() {
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);

    useEffect(() => {
        if (!hydrated || !user) return;
        return scheduleRouteModulePrefetches({
            paths: navigationTools.map((tool) => `/${tool.slug}`),
            prefetch: prefetchRouteModule,
            scheduler: browserIdleScheduler,
            onError: reportRouteModulePrefetchFailure,
        });
    }, [hydrated, user]);

    if (!hydrated || !user) return null;

    return (
        <nav className="workspace-floating-navigation" aria-label="工作台导航">
            <NavLink
                to="/"
                end
                onPointerEnter={() => requestRouteModulePrefetch("/")}
                onPointerDown={() => requestRouteModulePrefetch("/")}
                onFocus={() => requestRouteModulePrefetch("/")}
                className={({ isActive }) => cn("workspace-floating-navigation-link", isActive && "is-active")}
                aria-label="首页"
                title="首页"
            >
                <span className="workspace-floating-navigation-icon-frame">
                    <Home className="workspace-floating-navigation-icon" aria-hidden="true" />
                </span>
                <span className="workspace-floating-navigation-label">首页</span>
            </NavLink>
            {navigationTools.map((tool) => {
                const Icon = tool.icon;
                return (
                    <NavLink
                        key={tool.slug}
                        to={`/${tool.slug}`}
                        onPointerEnter={() => requestRouteModulePrefetch(`/${tool.slug}`)}
                        onPointerDown={() => requestRouteModulePrefetch(`/${tool.slug}`)}
                        onFocus={() => requestRouteModulePrefetch(`/${tool.slug}`)}
                        className={({ isActive }) => cn("workspace-floating-navigation-link", isActive && "is-active")}
                        aria-label={tool.label}
                        title={tool.label}
                    >
                        <span className="workspace-floating-navigation-icon-frame">
                            <Icon className="workspace-floating-navigation-icon" aria-hidden="true" />
                        </span>
                        <span className="workspace-floating-navigation-label">{tool.label}</span>
                    </NavLink>
                );
            })}
        </nav>
    );
}

function requestRouteModulePrefetch(pathname: string) {
    void prefetchRouteModule(pathname).catch((error: unknown) => reportRouteModulePrefetchFailure(pathname, error));
}

const browserIdleScheduler: RoutePrefetchScheduler = {
    schedule: (callback) => {
        if (window.requestIdleCallback) return window.requestIdleCallback(callback, { timeout: 1_500 });
        return window.setTimeout(callback, 1_500);
    },
    cancel: (handle) => {
        if (window.cancelIdleCallback) {
            window.cancelIdleCallback(handle);
            return;
        }
        window.clearTimeout(handle);
    },
};

function reportRouteModulePrefetchFailure(pathname: string, error: unknown) {
    console.warn("路由模块预取失败", { pathname, error });
}
