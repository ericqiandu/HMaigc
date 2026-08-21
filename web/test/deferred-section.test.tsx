import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { DeferredSection } from "../src/components/ui/deferred-section";

type ObserverCallback = ConstructorParameters<typeof IntersectionObserver>[0];

class TestIntersectionObserver implements IntersectionObserver {
    static instances: TestIntersectionObserver[] = [];

    readonly root = null;
    readonly rootMargin: string;
    readonly thresholds = [0];
    private readonly callback: ObserverCallback;

    constructor(callback: ObserverCallback, options?: IntersectionObserverInit) {
        this.callback = callback;
        this.rootMargin = options?.rootMargin || "0px";
        TestIntersectionObserver.instances.push(this);
    }

    disconnect() {}
    observe() {}
    takeRecords() {
        return [];
    }
    unobserve() {}

    reveal() {
        this.callback([{ isIntersecting: true } as IntersectionObserverEntry], this);
    }
}

let root: Root | null = null;
const originalObserver = globalThis.IntersectionObserver;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    TestIntersectionObserver.instances = [];
    globalThis.IntersectionObserver = originalObserver;
});

describe("DeferredSection", () => {
    test("does not mount expensive content until the section approaches the viewport", async () => {
        globalThis.IntersectionObserver = TestIntersectionObserver;
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(DeferredSection, { className: "test-deferred-section" }, createElement("span", { className: "expensive-child" }, "loaded")));
        });

        expect(document.querySelector(".expensive-child")).toBeNull();
        expect(TestIntersectionObserver.instances).toHaveLength(1);
        expect(TestIntersectionObserver.instances[0].rootMargin).toBe("600px 0px");

        await act(async () => TestIntersectionObserver.instances[0].reveal());

        expect(document.querySelector(".expensive-child")?.textContent).toBe("loaded");
    });
});
