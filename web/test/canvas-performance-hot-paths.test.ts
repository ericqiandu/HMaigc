import { describe, expect, test } from "bun:test";

import { applyCanvasLiveNodeDrag, clearCanvasLiveNodeDrag, createFrameDropContext, findFrameDropTargetFromContext, resolveCanvasLiveNodeDragTargets, shouldSyncCanvasDragPreview } from "../src/lib/canvas/canvas-drag-performance";
import { buildCanvasResourceReferencesFromGraph, buildNodeMentionReferencesByNodeId, createCanvasResourceGraph } from "../src/lib/canvas/canvas-resource-references";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

const node = (id: string, type: CanvasNodeType, x: number, y: number, content?: string): CanvasNodeData => ({
    id,
    type,
    title: id,
    position: { x, y },
    width: type === CanvasNodeType.Frame ? 500 : 100,
    height: type === CanvasNodeType.Frame ? 400 : 100,
    metadata: content ? { content } : undefined,
});

describe("canvas performance hot paths", () => {
    test("indexes resource graph once while preserving config and direct reference semantics", () => {
        const image = node("image", CanvasNodeType.Image, 0, 0, "/image.png");
        const text = node("text", CanvasNodeType.Text, 120, 0, "reference text");
        const config = node("config", CanvasNodeType.Config, 240, 0);
        const target = node("target", CanvasNodeType.Video, 360, 0);
        const nodes = [image, text, config, target];
        const connections: CanvasConnection[] = [
            { id: "image-config", fromNodeId: image.id, toNodeId: config.id },
            { id: "text-config", fromNodeId: text.id, toNodeId: config.id },
            { id: "target-config", fromNodeId: target.id, toNodeId: config.id },
        ];

        const graph = createCanvasResourceGraph(nodes, connections);
        const referencesByNodeId = buildNodeMentionReferencesByNodeId(nodes, graph);
        const globalReferences = buildCanvasResourceReferencesFromGraph(graph, target.id);

        expect(graph.nodeById.size).toBe(nodes.length);
        expect(referencesByNodeId.get(target.id)?.map((item) => item.nodeId)).toEqual([image.id, text.id]);
        expect(globalReferences.filter((item) => item.active).map((item) => item.nodeId)).toEqual([image.id, text.id]);
    });

    test("computes frame drop targets from a drag-start snapshot without rebuilding every node", () => {
        const dragged = node("dragged", CanvasNodeType.Image, 20, 20, "/image.png");
        const frame = node("frame", CanvasNodeType.Frame, 300, 100);
        const untouched = Array.from({ length: 1_000 }, (_, index) => node(`text-${index}`, CanvasNodeType.Text, index * 10, 900, "text"));
        const context = createFrameDropContext([dragged, frame, ...untouched], new Set([dragged.id]));

        expect(context?.draggedNodes).toHaveLength(1);
        expect(context?.frames).toHaveLength(1);
        expect(findFrameDropTargetFromContext(context, { x: 360, y: 180 })).toBe(frame.id);
        expect(findFrameDropTargetFromContext(context, { x: 0, y: 0 })).toBeNull();
    });

    test("keeps node movement on CSS variables and rate-limits React connection previews", () => {
        const properties = new Map<string, string>();
        const surface = {
            dataset: {} as DOMStringMap,
            style: {
                setProperty: (name: string, value: string) => properties.set(name, value),
                removeProperty: (name: string) => properties.delete(name),
                getPropertyValue: (name: string) => properties.get(name) || "",
            },
        } as HTMLElement;

        applyCanvasLiveNodeDrag([surface], { x: 18.5, y: -7 });
        expect(surface.style.getPropertyValue("--canvas-live-drag-x")).toBe("18.5px");
        expect(surface.style.getPropertyValue("--canvas-live-drag-y")).toBe("-7px");
        expect(surface.dataset.canvasNodeDragging).toBe("true");
        expect(shouldSyncCanvasDragPreview(120, 100)).toBeFalse();
        expect(shouldSyncCanvasDragPreview(150, 100)).toBeTrue();

        clearCanvasLiveNodeDrag([surface]);
        expect(surface.style.getPropertyValue("--canvas-live-drag-x")).toBe("");
        expect(surface.dataset.canvasNodeDragging).toBeUndefined();
    });

    test("resolves drag variables to selected nodes instead of invalidating the canvas ancestry", () => {
        const selected = { dataset: { nodeId: "selected" } } as unknown as HTMLElement;
        const untouched = { dataset: { nodeId: "untouched" } } as unknown as HTMLElement;
        const world = {
            querySelectorAll: () => [selected, untouched],
        } as unknown as HTMLElement;

        expect(resolveCanvasLiveNodeDragTargets(world, new Set(["selected"]))).toEqual([selected]);
        expect(resolveCanvasLiveNodeDragTargets(null, new Set(["selected"]))).toEqual([]);
    });

    test("keeps the drag offset prop stable while CSS variables carry frame-by-frame movement", async () => {
        const source = await Bun.file(new URL("../src/pages/canvas/canvas-project-world-layers.tsx", import.meta.url)).text();

        expect(source).toContain("dragOffset={props.dragPreview?.nodeIds.has(node.id) ? LIVE_DRAG_OFFSET : undefined}");
        expect(source.match(/const LIVE_DRAG_OFFSET/g)?.length).toBe(1);
    });
});
