import { describe, expect, test } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import { createWorkspaceRoutePrefetcher, hasWorkspaceRouteDataPrefetch } from "../src/lib/workspace-route-prefetch";

describe("workspace route intent prefetch policy", () => {
    test("only loads canonical route data for routes that own a shared query", () => {
        expect(hasWorkspaceRouteDataPrefetch("/projects")).toBe(true);
        expect(hasWorkspaceRouteDataPrefetch("/canvas")).toBe(true);
        expect(hasWorkspaceRouteDataPrefetch("/tasks")).toBe(false);
        expect(hasWorkspaceRouteDataPrefetch("/unmapped")).toBe(false);
    });
});

describe("workspace route prefetch composition", () => {
    test("prefetches the route module and canonical route data together", async () => {
        const calls: string[] = [];
        const queryClient = new QueryClient();
        const prefetch = createWorkspaceRoutePrefetcher({
            prefetchModule: async (pathname) => {
                calls.push(`module:${pathname}`);
            },
            prefetchData: async (pathname, receivedClient) => {
                expect(receivedClient).toBe(queryClient);
                calls.push(`data:${pathname}`);
            },
        });

        await prefetch("/projects", queryClient);
        expect(calls).toEqual(["module:/projects", "data:/projects"]);
    });

    test("propagates prefetch failures so callers can report the real error", async () => {
        const queryClient = new QueryClient();
        const prefetch = createWorkspaceRoutePrefetcher({
            prefetchModule: async () => {
                throw new Error("route chunk unavailable");
            },
            prefetchData: async () => undefined,
        });

        await expect(prefetch("/projects", queryClient)).rejects.toThrow("route chunk unavailable");
    });
});
