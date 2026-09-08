import "./setup-happy-dom";

import { expect, test } from "bun:test";

import { applyCanvasLiveViewport } from "../src/lib/canvas/canvas-live-viewport";

test("publishes reciprocal canvas scale for screen-sized node controls", () => {
    const container = document.createElement("div");

    applyCanvasLiveViewport(container, { x: 40, y: 24, k: 0.25 }, false);

    expect(container.style.getPropertyValue("--canvas-live-scale")).toBe("0.25");
    expect(container.style.getPropertyValue("--canvas-live-inverse-scale")).toBe("4");
});

test("zoom keeps background spacing stable while panning still moves its phase", () => {
    const container = document.createElement("div");
    applyCanvasLiveViewport(container, { x: 55, y: -55, k: 1 }, false);
    const spacing = container.style.getPropertyValue("--canvas-grid-size");
    for (const k of [0.05, 0.25, 2]) {
        applyCanvasLiveViewport(container, { x: 55, y: -55, k }, false);
        expect(container.style.getPropertyValue("--canvas-grid-size")).toBe(spacing);
        expect(container.style.getPropertyValue("--canvas-grid-x")).toBe("7px");
        expect(container.style.getPropertyValue("--canvas-grid-y")).toBe("-7px");
    }
});
