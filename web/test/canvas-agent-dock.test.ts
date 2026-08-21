import { expect, test } from "bun:test";

import { CANVAS_AGENT_DOCK_MAX_WIDTH, CANVAS_AGENT_DOCK_MIN_WIDTH, clampCanvasAgentDockWidth, resizeCanvasAgentDockWidthFromKey, resizeViewportAroundCenter } from "../src/lib/canvas/canvas-agent-dock";

test("Agent 停靠宽度限制在可用区间", () => {
    expect(clampCanvasAgentDockWidth(200)).toBe(320);
    expect(clampCanvasAgentDockWidth(400)).toBe(400);
    expect(clampCanvasAgentDockWidth(800)).toBe(560);
});

test("Agent 停靠分隔条支持键盘调整并遵守宽度边界", () => {
    expect(resizeCanvasAgentDockWidthFromKey(400, "ArrowLeft")).toBe(416);
    expect(resizeCanvasAgentDockWidthFromKey(400, "ArrowRight")).toBe(384);
    expect(resizeCanvasAgentDockWidthFromKey(400, "Home")).toBe(CANVAS_AGENT_DOCK_MIN_WIDTH);
    expect(resizeCanvasAgentDockWidthFromKey(400, "End")).toBe(CANVAS_AGENT_DOCK_MAX_WIDTH);
    expect(resizeCanvasAgentDockWidthFromKey(400, "Enter")).toBeNull();
});

test("画布缩窄时保持当前世界中心并按可用区域缩放", () => {
    const viewport = resizeViewportAroundCenter(
        { x: 600, y: 360, k: 1 },
        { width: 1200, height: 720 },
        { width: 800, height: 720 },
    );

    expect(viewport.x).toBe(400);
    expect(viewport.y).toBe(360);
    expect(viewport.k).toBeCloseTo(2 / 3, 8);
});

test("无有效旧尺寸时不伪造新的画布视口", () => {
    const viewport = { x: 42, y: 18, k: 0.75 };
    expect(resizeViewportAroundCenter(viewport, { width: 0, height: 720 }, { width: 800, height: 720 })).toEqual(viewport);
});
