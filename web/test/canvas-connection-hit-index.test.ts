import { describe, expect, test } from "bun:test";

import { createCanvasConnectionHitIndex, queryCanvasConnectionHitIndex } from "../src/lib/canvas/canvas-connection-hit-index";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

const node = (id: string, x: number, y: number, type = CanvasNodeType.Image): CanvasNodeData => ({
    id,
    type,
    title: id,
    position: { x, y },
    width: 120,
    height: 90,
});

describe("canvas connection hit index", () => {
    test("queries only nearby connectable nodes from the drag-start snapshot", () => {
        const farNodes = Array.from({ length: 1_000 }, (_, index) => node(`far-${index}`, 5_000 + index * 200, 5_000));
        const nearbyBack = node("near-back", 140, 90);
        const nearbyFront = node("near-front", 150, 100);
        const index = createCanvasConnectionHitIndex([...farNodes, nearbyBack, nearbyFront], 256);

        expect(queryCanvasConnectionHitIndex(index, { x: 160, y: 120 }, 36).map((item) => item.id)).toEqual([nearbyFront.id, nearbyBack.id]);
        expect(queryCanvasConnectionHitIndex(index, { x: -500, y: -500 }, 36)).toEqual([]);
    });

    test("excludes frames and nodes hidden by collapsed canvas structure", () => {
        const frame = { ...node("frame", 0, 0, CanvasNodeType.Frame), width: 600, height: 400, metadata: { frame: { collapsed: true } } };
        const hiddenChild = { ...node("hidden", 40, 60), parentId: frame.id };
        const visible = node("visible", 40, 60);
        const index = createCanvasConnectionHitIndex([frame, hiddenChild, visible], 256);

        expect(queryCanvasConnectionHitIndex(index, { x: 60, y: 80 }, 16).map((item) => item.id)).toEqual([visible.id]);
    });
});
