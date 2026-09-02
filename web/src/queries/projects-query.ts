import { queryOptions } from "@tanstack/react-query";

import { listProjects } from "@/services/api/projects";

export const projectsQueryKey = ["projects"] as const;

export function createProjectsQueryOptions(queryFn: typeof listProjects = listProjects) {
    return queryOptions({
        queryKey: projectsQueryKey,
        queryFn,
        staleTime: 30_000,
    });
}

export const projectsQueryOptions = createProjectsQueryOptions();
