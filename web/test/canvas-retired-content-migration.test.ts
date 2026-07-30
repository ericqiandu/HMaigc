import { describe, expect, test } from "bun:test";

import {
    removeRetiredCanvasNodes,
    removeRetiredCanvasProjectContent,
} from "../src/lib/canvas/canvas-retired-content-migration";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

describe("retired canvas content migration", () => {
    test("removes legacy drawing nodes and every attached connection", () => {
        const textNode = canvasNode("text-node", CanvasNodeType.Text);
        const imageNode = canvasNode("image-node", CanvasNodeType.Image);
        const drawingNode = {
            ...canvasNode("drawing-node", CanvasNodeType.Image),
            type: "drawing",
        } as unknown as CanvasNodeData;
        const graph = {
            nodes: [textNode, drawingNode, imageNode],
            connections: [
                connection("text-to-drawing", textNode.id, drawingNode.id),
                connection("drawing-to-image", drawingNode.id, imageNode.id),
                connection("text-to-image", textNode.id, imageNode.id),
            ],
        };

        const migrated = removeRetiredCanvasNodes(graph);

        expect(migrated.nodes.map((node) => node.id)).toEqual(["text-node", "image-node"]);
        expect(migrated.connections.map((item) => item.id)).toEqual(["text-to-image"]);
        expect(graph.nodes).toHaveLength(3);
        expect(graph.connections).toHaveLength(3);
    });

    test("preserves the project version while cleaning before remote conflict resolution", () => {
        const drawingNode = {
            ...canvasNode("drawing-node", CanvasNodeType.Image),
            type: "drawing",
        } as unknown as CanvasNodeData;
        const project = {
            id: "project-1",
            updatedAt: "2026-07-30T00:00:00.000Z",
            nodes: [drawingNode],
            connections: [],
        };

        const migrated = removeRetiredCanvasProjectContent(project);

        expect(migrated.nodes).toEqual([]);
        expect(migrated.updatedAt).toBe(project.updatedAt);
    });

    test("preserves project identity when no retired node exists", () => {
        const project = {
            id: "project-1",
            updatedAt: "2026-07-30T00:00:00.000Z",
            nodes: [canvasNode("text-node", CanvasNodeType.Text)],
            connections: [],
        };

        expect(removeRetiredCanvasProjectContent(project)).toBe(project);
    });
});

function canvasNode(id: string, type: CanvasNodeType): CanvasNodeData {
    return {
        id,
        type,
        title: id,
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: { status: "idle" },
    };
}

function connection(id: string, fromNodeId: string, toNodeId: string): CanvasConnection {
    return { id, fromNodeId, toNodeId };
}
