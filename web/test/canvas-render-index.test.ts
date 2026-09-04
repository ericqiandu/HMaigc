import { describe, expect, test } from "bun:test";

import { createCanvasRenderIndex } from "../src/lib/canvas/canvas-render-index";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

const node = (id: string, x: number, y: number, type = CanvasNodeType.Image, width = 120, height = 90): CanvasNodeData => ({
    id,
    type,
    title: id,
    position: { x, y },
    width,
    height,
});

describe("canvas render index", () => {
    test("queries a 10,000-node graph with stable frame-first ordering and media tiers", () => {
        const frame = node("frame-1", 0, 0, CanvasNodeType.Frame, 500, 400);
        const visibleOne = node("node-1", 100, 100);
        const visibleTwo = node("node-2", 1_360, 820);
        const near = node("node-3", 1_520, 300);
        const boundary = node("node-boundary", 1_440, 450);
        const farNodes = Array.from({ length: 9_995 }, (_, index) => node(`far-${index}`, 10_000 + (index % 100) * 220, 10_000 + Math.floor(index / 100) * 180));
        const index = createCanvasRenderIndex([visibleOne, visibleTwo, near, boundary, frame, ...farNodes], [], new Set());

        const result = index.query({ visible: { left: 0, top: 0, right: 1_440, bottom: 900 }, nearPadding: 480 });

        expect(result.nodeIds).toEqual(["frame-1", "node-1", "node-2", "node-3", "node-boundary"]);
        expect([...result.mediaTierByNodeId]).toEqual([
            ["frame-1", "visible"],
            ["node-1", "visible"],
            ["node-2", "visible"],
            ["node-3", "near"],
            ["node-boundary", "near"],
        ]);
    });

    test("excludes collapsed structure and projects collapsed-frame connections to the frame", () => {
        const collapsedBatchRoot: CanvasNodeData = { ...node("batch-root", 100, 100), metadata: { isBatchRoot: true, batchExpanded: false, batchChildIds: ["batch-child"] } };
        const hiddenBatchChild: CanvasNodeData = { ...node("batch-child", 120, 120), metadata: { batchRootId: collapsedBatchRoot.id } };
        const collapsedFrame: CanvasNodeData = { ...node("frame", 300, 100, CanvasNodeType.Frame, 500, 400), metadata: { frame: { collapsed: true } } };
        const hiddenFrameChild: CanvasNodeData = { ...node("frame-child", 360, 180), parentId: collapsedFrame.id };
        const visible = node("visible", 900, 200);
        const connections: CanvasConnection[] = [
            { id: "batch-edge", fromNodeId: hiddenBatchChild.id, toNodeId: visible.id },
            { id: "frame-edge", fromNodeId: hiddenFrameChild.id, toNodeId: visible.id },
        ];
        const hidden = new Set([hiddenBatchChild.id, hiddenFrameChild.id]);

        const result = createCanvasRenderIndex([collapsedBatchRoot, hiddenBatchChild, collapsedFrame, hiddenFrameChild, visible], connections, hidden).query({ visible: { left: 0, top: 0, right: 1_200, bottom: 700 }, nearPadding: 0 });

        expect(result.nodeIds).toEqual(["frame", "batch-root", "visible"]);
        expect(result.connectionIds).toEqual(["frame-edge"]);
    });

    test("returns only inside and crossing connections without duplicate ids", () => {
        const insideLeft = node("inside-left", 100, 200);
        const insideRight = node("inside-right", 900, 300);
        const crossingLeft = node("crossing-left", -1_000, 400);
        const crossingRight = node("crossing-right", 2_500, 400);
        const nearLeft = node("near-left", 1_600, 100);
        const nearRight = node("near-right", 1_850, 200);
        const farLeft = node("far-left", 6_000, 6_000);
        const farRight = node("far-right", 6_500, 6_500);
        const connections: CanvasConnection[] = [
            { id: "edge-inside", fromNodeId: insideLeft.id, toNodeId: insideRight.id },
            { id: "edge-crossing", fromNodeId: crossingLeft.id, toNodeId: crossingRight.id },
            { id: "edge-near", fromNodeId: nearLeft.id, toNodeId: nearRight.id },
            { id: "edge-far", fromNodeId: farLeft.id, toNodeId: farRight.id },
        ];
        const index = createCanvasRenderIndex([insideLeft, insideRight, crossingLeft, crossingRight, nearLeft, nearRight, farLeft, farRight], connections, new Set());

        const result = index.query({ visible: { left: 0, top: 0, right: 1_440, bottom: 900 }, nearPadding: 480 });

        expect(result.connectionIds).toEqual(["edge-inside", "edge-crossing"]);
        expect(new Set(result.connectionIds).size).toBe(result.connectionIds.length);
    });

    test("rejects invalid query geometry instead of returning misleading results", () => {
        const index = createCanvasRenderIndex([], [], new Set());
        expect(() => index.query({ visible: { left: 0, top: 0, right: Number.NaN, bottom: 900 }, nearPadding: 0 })).toThrow("画布可见区域必须是有限矩形");
        expect(() => index.query({ visible: { left: 10, top: 0, right: 0, bottom: 900 }, nearPadding: 0 })).toThrow("画布可见区域边界无效");
    });
});
