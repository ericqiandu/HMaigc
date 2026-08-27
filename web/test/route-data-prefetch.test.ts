import { describe, expect, test } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import { createRouteDataPrefetcher } from "../src/lib/route-data-prefetch";
import { createProjectsQueryOptions, projectsQueryKey } from "../src/queries/projects-query";

describe("route data prefetch", () => {
    test("warms the canonical projects query for projects and canvas without a fallback route", async () => {
        let fetchCount = 0;
        const result = { projects: [] };
        const queryOptions = createProjectsQueryOptions(async () => {
            fetchCount += 1;
            return result;
        });
        const prefetch = createRouteDataPrefetcher(queryOptions);
        const projectsClient = new QueryClient();
        const canvasClient = new QueryClient();
        const unknownClient = new QueryClient();

        await prefetch("/projects", projectsClient);
        expect(projectsClient.getQueryData(projectsQueryKey)).toBe(result);

        await prefetch("/canvas", canvasClient);
        expect(canvasClient.getQueryData(projectsQueryKey)).toBe(result);

        await prefetch("/unmapped", unknownClient);
        expect(unknownClient.getQueryData(projectsQueryKey)).toBeUndefined();
        expect(fetchCount).toBe(2);

        await prefetch("/projects", projectsClient);
        expect(fetchCount).toBe(2);
    });

    test("propagates project query failures to the route prefetch caller", async () => {
        const queryOptions = createProjectsQueryOptions(async () => {
            throw new Error("projects unavailable");
        });
        const prefetch = createRouteDataPrefetcher(queryOptions);

        await expect(prefetch("/projects", new QueryClient())).rejects.toThrow("projects unavailable");
    });
});
