import { readFileSync } from "node:fs";
import { describe, expect, test } from "bun:test";

import {
    createRouteModulePrefetcher,
    routeModuleKeyForPathname,
    type RouteModuleKey,
    type RouteModuleLoaders,
} from "../src/lib/route-module-prefetch";

const workspaceNavigationSource = readFileSync(
    new URL("../src/components/layout/workspace-floating-navigation.tsx", import.meta.url),
    "utf8",
);

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

    test("prefetches navigation modules only from explicit pointer or keyboard intent", () => {
        expect(workspaceNavigationSource).not.toContain("scheduleRouteModulePrefetches");
        expect(workspaceNavigationSource).not.toContain("useEffect(");
        expect(workspaceNavigationSource).toContain('onPointerEnter={() => requestRouteModulePrefetch("/")}');
        expect(workspaceNavigationSource).toContain("onPointerDown={() => requestRouteModulePrefetch(`/${tool.slug}`)}");
        expect(workspaceNavigationSource).toContain("onFocus={() => requestRouteModulePrefetch(`/${tool.slug}`)}");
    });
});
