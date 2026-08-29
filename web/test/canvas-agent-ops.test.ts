import { describe, expect, test } from "bun:test";

import { applyCanvasAgentOps, type CanvasAgentSnapshot } from "../src/lib/canvas/canvas-agent-ops";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

function node(id: string, type: CanvasNodeType): CanvasNodeData {
    return { id, type, title: id, position: { x: 0, y: 0 }, width: 320, height: 180 };
}

function snapshot(): CanvasAgentSnapshot {
    return {
        projectId: "canvas-1",
        title: "测试画布",
        nodes: [node("video", CanvasNodeType.Video), node("image", CanvasNodeType.Image), node("audio", CanvasNodeType.Audio)],
        connections: [],
        selectedNodeIds: [],
        viewport: { x: 0, y: 0, k: 1 },
    };
}

describe("canvas agent connection operations", () => {
    test("rejects an illegal video output instead of silently adding or skipping it", () => {
        expect(() => applyCanvasAgentOps(snapshot(), [{ type: "connect_nodes", fromNodeId: "video", toNodeId: "image" }])).toThrow(
            "视频节点的输出不能连接图片或音频节点",
        );
    });

    test("adds a legal image input to a video node", () => {
        const result = applyCanvasAgentOps(snapshot(), [{ type: "connect_nodes", id: "edge-1", fromNodeId: "image", toNodeId: "video" }]);
        expect(result.connections).toEqual([{ id: "edge-1", fromNodeId: "image", toNodeId: "video", fromHandleId: undefined, toHandleId: undefined }]);
    });
});
