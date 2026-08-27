import type { QueryClient } from "@tanstack/react-query";

import { prefetchRouteModule, routeModuleKeyForPathname } from "@/lib/route-module-prefetch";

export type WorkspaceRoutePrefetchScheduler = {
    schedule: (callback: () => void) => number;
    cancel: (handle: number) => void;
};

export type BackgroundWorkspacePrefetchContext = {
    visibilityState: DocumentVisibilityState;
    saveData: boolean;
    effectiveType?: string;
};

type WorkspaceRoutePrefetchDependencies = {
    prefetchModule: (pathname: string) => Promise<void>;
    prefetchData: (pathname: string, queryClient: QueryClient) => Promise<void>;
};

type WorkspaceRoutePrefetchSchedule = {
    pathname: string;
    prefetch: (pathname: string) => Promise<void>;
    scheduler: WorkspaceRoutePrefetchScheduler;
    onError: (pathname: string, error: unknown) => void;
};

export function backgroundWorkspaceRouteForPathname(pathname: string): string | null {
    return routeModuleKeyForPathname(pathname) === "home" ? "/projects" : null;
}

export function canBackgroundPrefetch({ visibilityState, saveData, effectiveType }: BackgroundWorkspacePrefetchContext): boolean {
    if (visibilityState !== "visible" || saveData) return false;
    return effectiveType !== "slow-2g" && effectiveType !== "2g";
}

export function hasWorkspaceRouteDataPrefetch(pathname: string): boolean {
    const routeKey = routeModuleKeyForPathname(pathname);
    return routeKey === "projects" || routeKey === "canvas";
}

export function scheduleWorkspaceRoutePrefetch({ pathname, prefetch, scheduler, onError }: WorkspaceRoutePrefetchSchedule): () => void {
    let cancelled = false;
    let dispatched = false;
    const handle = scheduler.schedule(() => {
        if (cancelled || dispatched) return;
        dispatched = true;
        void prefetch(pathname).catch((error: unknown) => onError(pathname, error));
    });

    return () => {
        if (cancelled) return;
        cancelled = true;
        if (!dispatched) scheduler.cancel(handle);
    };
}

export function createWorkspaceRoutePrefetcher({ prefetchModule, prefetchData }: WorkspaceRoutePrefetchDependencies) {
    return async (pathname: string, queryClient: QueryClient): Promise<void> => {
        await Promise.all([prefetchModule(pathname), prefetchData(pathname, queryClient)]);
    };
}

export const prefetchWorkspaceRoute = createWorkspaceRoutePrefetcher({
    prefetchModule: prefetchRouteModule,
    prefetchData: async (pathname, queryClient) => {
        if (!hasWorkspaceRouteDataPrefetch(pathname)) return;
        const { prefetchRouteData } = await import("@/lib/route-data-prefetch");
        await prefetchRouteData(pathname, queryClient);
    },
});
