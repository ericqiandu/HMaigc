import type { QueryClient } from "@tanstack/react-query";

import { prefetchRouteModule, routeModuleKeyForPathname } from "@/lib/route-module-prefetch";

type WorkspaceRoutePrefetchDependencies = {
    prefetchModule: (pathname: string) => Promise<void>;
    prefetchData: (pathname: string, queryClient: QueryClient) => Promise<void>;
};

export function hasWorkspaceRouteDataPrefetch(pathname: string): boolean {
    const routeKey = routeModuleKeyForPathname(pathname);
    return routeKey === "projects" || routeKey === "canvas";
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
