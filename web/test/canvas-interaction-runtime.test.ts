import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasNode } from "../src/components/canvas/canvas-node";
import * as interactionRuntime from "../src/lib/canvas/canvas-interaction-runtime";
import { applyCanvasLiveResize, calculateCanvasResizePreview, clearCanvasLiveResize, createCanvasSelectionIndex } from "../src/lib/canvas/canvas-interaction-runtime";
import { calculateNodeAlignment, createNodeAlignmentContext } from "../src/lib/canvas/canvas-project-domain";
import { CanvasNodeType, type CanvasNodeData, type Position } from "../src/types/canvas";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("canvas interaction runtime", () => {
    test("calculates top-left aspect-ratio resize from an immutable initial geometry", () => {
        expect(
            calculateCanvasResizePreview({
                initial: { x: 100, y: 80, width: 640, height: 360 },
                delta: { x: -64, y: -36 },
                corner: "top-left",
                minWidth: 220,
                minHeight: 160,
                aspectRatio: 16 / 9,
            }),
        ).toEqual({ x: 164, y: 116, width: 576, height: 324 });
    });

    test("rejects non-finite geometry instead of writing an invalid preview", () => {
        expect(() =>
            calculateCanvasResizePreview({
                initial: { x: 0, y: 0, width: 640, height: 360 },
                delta: { x: Number.NaN, y: 0 },
                corner: "bottom-right",
                minWidth: 220,
                minHeight: 160,
            }),
        ).toThrow("画布 resize 输入必须是有限数字");
    });

    test("writes only CSS preview properties and clears every property", () => {
        const target = document.createElement("div");
        for (let index = 0; index < 100; index += 1) {
            applyCanvasLiveResize(target, { x: 100 + index, y: 80, width: 640 + index, height: 360 });
        }

        expect(target.dataset.canvasNodeResizing).toBe("true");
        expect(target.style.getPropertyValue("--canvas-live-resize-x")).toBe("199px");
        expect(target.style.getPropertyValue("--canvas-live-resize-width")).toBe("739px");
        expect(Object.keys(interactionRuntime).some((name) => name.toLocaleLowerCase().includes("commit"))).toBeFalse();

        clearCanvasLiveResize(target);
        expect(target.dataset.canvasNodeResizing).toBeUndefined();
        expect(target.style.getPropertyValue("--canvas-live-resize-x")).toBe("");
        expect(target.style.getPropertyValue("--canvas-live-resize-y")).toBe("");
        expect(target.style.getPropertyValue("--canvas-live-resize-width")).toBe("");
        expect(target.style.getPropertyValue("--canvas-live-resize-height")).toBe("");
    });

    test("keeps 100 resize moves out of document state and commits once on mouseup", async () => {
        const commits: Array<{ nodeId: string; width: number; height: number; position?: Position }> = [];
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () => {
            root?.render(
                createElement(CanvasNode, {
                    data: textNode(),
                    scale: 1,
                    isSelected: true,
                    isRelated: false,
                    isFocusRelated: false,
                    isConnectionTarget: false,
                    isConnecting: false,
                    showImageInfo: false,
                    mediaVisibilityTier: "visible",
                    onMouseDown: () => undefined,
                    onHoverStart: () => undefined,
                    onHoverEnd: () => undefined,
                    onConnectStart: () => undefined,
                    onResizeCommit: (nodeId: string, width: number, height: number, position?: Position) => commits.push({ nodeId, width, height, position }),
                    onContentChange: () => undefined,
                    onContextMenu: () => undefined,
                }),
            );
        });

        const resizeHandle = document.querySelector<HTMLElement>("[data-canvas-resize-corner='bottom-right']");
        if (!resizeHandle) throw new Error("缺少 resize 手柄");
        await act(async () => resizeHandle.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, clientX: 0, clientY: 0 })));
        await act(async () => {
            for (let index = 1; index <= 100; index += 1) window.dispatchEvent(new MouseEvent("mousemove", { clientX: index, clientY: index }));
        });

        expect(commits).toEqual([]);
        const nodeElement = document.querySelector<HTMLElement>("[data-node-id='resize-node']");
        expect(nodeElement?.style.getPropertyValue("--canvas-live-resize-width")).toBe("340px");
        expect(nodeElement?.style.getPropertyValue("--canvas-live-resize-height")).toBe("260px");

        await act(async () => window.dispatchEvent(new MouseEvent("mouseup")));
        expect(commits).toEqual([{ nodeId: "resize-node", width: 340, height: 260, position: { x: 10, y: 20 } }]);
        expect(nodeElement?.style.getPropertyValue("--canvas-live-resize-width")).toBe("");
    });

    test("queries selection candidates through a spatial index", () => {
        const candidates = [
            { id: "inside", left: 20, top: 20, right: 120, bottom: 120 },
            { id: "boundary", left: 190, top: 190, right: 260, bottom: 260 },
            ...Array.from({ length: 10_000 }, (_, index) => ({ id: `far-${index}`, left: 10_000 + index * 200, top: 10_000, right: 10_100 + index * 200, bottom: 10_100 })),
        ];
        const index = createCanvasSelectionIndex(candidates);

        expect(index.query({ left: 0, top: 0, right: 200, bottom: 200 })).toEqual(["inside", "boundary"]);
        expect(index.query({ left: 500, top: 500, right: 700, bottom: 700 })).toEqual([]);
    });

    test("finds the nearest alignment guide from sorted drag-start targets", () => {
        const moving = textNode();
        const context = createNodeAlignmentContext(
            [
                moving,
                { ...textNode(), id: "guide-far", position: { x: 600, y: 400 } },
                { ...textNode(), id: "guide-near", position: { x: 217, y: 20 } },
            ],
            [{ id: moving.id, x: moving.position.x, y: moving.position.y }],
        );

        expect(calculateNodeAlignment(context, { x: 204, y: 0 }, 7)).toEqual({
            offset: { x: 207, y: 0 },
            guides: { vertical: 217, horizontal: 20 },
        });
    });
});

function textNode(): CanvasNodeData {
    return {
        id: "resize-node",
        type: CanvasNodeType.Text,
        title: "Resize node",
        position: { x: 10, y: 20 },
        width: 240,
        height: 160,
        metadata: { content: "Resize without state writes" },
    };
}
