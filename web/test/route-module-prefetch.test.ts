import { describe, expect, test } from "bun:test";

import {
    createRouteModulePrefetcher,
    routeModuleKeyForPathname,
    scheduleRouteModulePrefetches,
    type RouteModuleKey,
    type RouteModuleLoaders,
} from "../src/lib/route-module-prefetch";

function controlledLoaders(load: (key: RouteModuleKey) => Promise<unknown>): RouteModuleLoaders {
    return {
        home: () => load("home"),
        projects: () => load("projects"),
        projectDetail: () => load("projectDetail"),
        canvas: () => load("canvas"),
        canvasProject: () => load("canvasProject"),
        tasks: () => load("tasks"),
        assets: () => load("assets"),
        skills: () => load("skills"),
        teams: () => load("teams"),
        wallet: () => load("wallet"),
        settings: () => load("settings"),
    };
}

describe("route module prefetch", () => {
    test("maps concrete paths to their actual route modules without semantic fallbacks", () => {
        expect(routeModuleKeyForPathname("/")).toBe("home");
        expect(routeModuleKeyForPathname("/projects")).toBe("projects");
        expect(routeModuleKeyForPathname("/projects/project-1/overview")).toBe("projectDetail");
        expect(routeModuleKeyForPathname("/canvas")).toBe("canvas");
        expect(routeModuleKeyForPathname("/canvas/canvas-1")).toBe("canvasProject");
        expect(routeModuleKeyForPathname("/unknown")).toBeNull();
    });

    test("deduplicates successful intent prefetches for the same module", async () => {
        const calls: RouteModuleKey[] = [];
        const prefetch = createRouteModulePrefetcher(
            controlledLoaders(async (key) => {
                calls.push(key);
                return { default: () => null };
            }),
        );

        await Promise.all([prefetch("/projects/one/overview"), prefetch("/projects/two/assets")]);
        await prefetch("/projects/three/chapters");

        expect(calls).toEqual(["projectDetail"]);
    });

    test("does not cache a rejected preload so a later user intent can retry", async () => {
        let attempts = 0;
        const prefetch = createRouteModulePrefetcher(
            controlledLoaders(async () => {
                attempts += 1;
                if (attempts === 1) throw new Error("route chunk unavailable");
                return { default: () => null };
            }),
        );

        await expect(prefetch("/canvas")).rejects.toThrow("route chunk unavailable");
        await expect(prefetch("/canvas")).resolves.toBeUndefined();
        expect(attempts).toBe(2);
    });

    test("warms common routes one at a time during idle periods and supports cleanup", async () => {
        const callbacks = new Map<number, () => void>();
        const cancelledHandles: number[] = [];
        const prefetched: string[] = [];
        let nextHandle = 1;
        const cleanup = scheduleRouteModulePrefetches({
            paths: ["/projects", "/canvas", "/projects"],
            prefetch: async (pathname) => {
                prefetched.push(pathname);
            },
            scheduler: {
                schedule: (callback) => {
                    const handle = nextHandle++;
                    callbacks.set(handle, callback);
                    return handle;
                },
                cancel: (handle) => {
                    cancelledHandles.push(handle);
                    callbacks.delete(handle);
                },
            },
            onError: () => undefined,
        });

        expect([...callbacks.keys()]).toEqual([1]);
        callbacks.get(1)?.();
        await Promise.resolve();
        await Promise.resolve();
        expect(prefetched).toEqual(["/projects"]);
        expect([...callbacks.keys()]).toEqual([1, 2]);

        cleanup();
        expect(cancelledHandles).toEqual([2]);
        callbacks.get(2)?.();
        await Promise.resolve();
        expect(prefetched).toEqual(["/projects"]);
    });
});
