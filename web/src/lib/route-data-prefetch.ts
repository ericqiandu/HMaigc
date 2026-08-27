import type { QueryClient } from "@tanstack/react-query";

import { routeModuleKeyForPathname } from "@/lib/route-module-prefetch";
import { createProjectsQueryOptions, projectsQueryOptions } from "@/queries/projects-query";

type ProjectsQueryOptions = ReturnType<typeof createProjectsQueryOptions>;

export function createRouteDataPrefetcher(projectQueryOptions: ProjectsQueryOptions = projectsQueryOptions) {
    return async (pathname: string, queryClient: QueryClient): Promise<void> => {
        const routeKey = routeModuleKeyForPathname(pathname);
        if (routeKey !== "projects" && routeKey !== "canvas") return;
        await queryClient.fetchQuery(projectQueryOptions);
    };
}

export const prefetchRouteData = createRouteDataPrefetcher();
