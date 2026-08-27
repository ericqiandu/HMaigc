import { describe, expect, test } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import {
    backgroundWorkspaceRouteForPathname,
    canBackgroundPrefetch,
    createWorkspaceRoutePrefetcher,
    hasWorkspaceRouteDataPrefetch,
    scheduleWorkspaceRoutePrefetch,
} from "../src/lib/workspace-route-prefetch";

describe("workspace route background policy", () => {
    test("only proposes projects while the user is on home", () => {
        expect(backgroundWorkspaceRouteForPathname("/")).toBe("/projects");
        expect(backgroundWorkspaceRouteForPathname("/projects")).toBeNull();
        expect(backgroundWorkspaceRouteForPathname("/canvas/project-1")).toBeNull();
    });

    test("rejects hidden, data-saving, and slow network contexts", () => {
        expect(canBackgroundPrefetch({ visibilityState: "visible", saveData: false, effectiveType: "4g" })).toBe(true);
        expect(canBackgroundPrefetch({ visibilityState: "hidden", saveData: false, effectiveType: "4g" })).toBe(false);
        expect(canBackgroundPrefetch({ visibilityState: "visible", saveData: true, effectiveType: "4g" })).toBe(false);
        expect(canBackgroundPrefetch({ visibilityState: "visible", saveData: false, effectiveType: "2g" })).toBe(false);
        expect(canBackgroundPrefetch({ visibilityState: "visible", saveData: false, effectiveType: "slow-2g" })).toBe(false);
    });

    test("only loads canonical route data for routes that own a shared query", () => {
        expect(hasWorkspaceRouteDataPrefetch("/projects")).toBe(true);
        expect(hasWorkspaceRouteDataPrefetch("/canvas")).toBe(true);
        expect(hasWorkspaceRouteDataPrefetch("/tasks")).toBe(false);
        expect(hasWorkspaceRouteDataPrefetch("/unmapped")).toBe(false);
    });
});

describe("workspace route scheduling", () => {
    test("schedules exactly one pathname and dispatches it once", async () => {
        const callbacks = new Map<number, () => void>();
        const prefetched: string[] = [];
        const cleanup = scheduleWorkspaceRoutePrefetch({
            pathname: "/projects",
            prefetch: async (pathname) => {
                prefetched.push(pathname);
            },
            scheduler: {
                schedule: (callback) => {
                    callbacks.set(1, callback);
                    return 1;
                },
                cancel: () => undefined,
            },
            onError: () => undefined,
        });

        expect([...callbacks.keys()]).toEqual([1]);
        callbacks.get(1)?.();
        callbacks.get(1)?.();
        await Promise.resolve();

        expect(prefetched).toEqual(["/projects"]);
        cleanup();
    });

    test("cancels a pending idle callback during cleanup", () => {
        const callbacks = new Map<number, () => void>();
        const cancelledHandles: number[] = [];
        const prefetched: string[] = [];
        const cleanup = scheduleWorkspaceRoutePrefetch({
            pathname: "/projects",
            prefetch: async (pathname) => {
                prefetched.push(pathname);
            },
            scheduler: {
                schedule: (callback) => {
                    callbacks.set(7, callback);
                    return 7;
                },
                cancel: (handle) => {
                    cancelledHandles.push(handle);
                    callbacks.delete(handle);
                },
            },
            onError: () => undefined,
        });

        cleanup();
        expect(cancelledHandles).toEqual([7]);
        expect(callbacks.size).toBe(0);
        expect(prefetched).toEqual([]);
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
