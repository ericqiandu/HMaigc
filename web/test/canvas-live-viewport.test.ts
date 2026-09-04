import "./setup-happy-dom";

import { expect, test } from "bun:test";

import { applyCanvasLiveViewport } from "../src/lib/canvas/canvas-live-viewport";

test("publishes reciprocal canvas scale for screen-sized node controls", () => {
    const container = document.createElement("div");

    applyCanvasLiveViewport(container, { x: 40, y: 24, k: 0.25 }, false);

    expect(container.style.getPropertyValue("--canvas-live-scale")).toBe("0.25");
    expect(container.style.getPropertyValue("--canvas-live-inverse-scale")).toBe("4");
});
