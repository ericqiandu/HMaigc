import { describe, expect, test } from "bun:test";
import { readFile } from "node:fs/promises";

const webRoot = new URL("../", import.meta.url);

describe("navigation performance contract", () => {
    test("does not preload homepage-only images for every route", async () => {
        const html = await readFile(new URL("index.html", webRoot), "utf8");

        expect(html).not.toContain('rel="preload" as="image"');
    });

    test("project detail respects the shared query freshness window", async () => {
        const source = await readFile(new URL("src/pages/projects/detail.tsx", webRoot), "utf8");

        expect(source).not.toContain('refetchOnMount: "always"');
    });

    test("floating navigation prefetches only from explicit user intent", async () => {
        const source = await readFile(new URL("src/components/layout/workspace-floating-navigation.tsx", webRoot), "utf8");

        expect(source).not.toContain("useEffect");
        expect(source).not.toContain("requestIdleCallback");
        expect(source).not.toContain('addEventListener("load"');
        expect(source).toContain("onPointerEnter");
        expect(source).toContain("onPointerDown");
        expect(source).toContain("onFocus");
        expect(source).toContain("requestWorkspaceRoutePrefetch");
    });
});
