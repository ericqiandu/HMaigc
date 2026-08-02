import { describe, expect, test } from "bun:test";

import {
    getCompositionSourceVideos,
    isVideoCompositionNode,
    normalizeVideoCompositionNode,
    supportsVideoAssetUpload,
    VIDEO_COMPOSITION_NODE_SIZE,
} from "../src/lib/canvas/canvas-video-composition";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

const node = (id: string, type: CanvasNodeType, content?: string, shotIndex?: number): CanvasNodeData => ({
    id,
    type,
    title: id,
    position: { x: id.charCodeAt(0), y: 0 },
    width: 320,
    height: 180,
    metadata: { content, shotIndex },
});

describe("video composition node", () => {
    test("recognizes only concat video nodes", () => {
        expect(isVideoCompositionNode({ ...node("target", CanvasNodeType.Video), metadata: { videoEditOperation: "concat" } })).toBe(true);
        expect(isVideoCompositionNode(node("video", CanvasNodeType.Video))).toBe(false);
        expect(isVideoCompositionNode(node("image", CanvasNodeType.Image))).toBe(false);
    });

    test("does not expose source video upload or replacement on composition nodes", () => {
        const composition = { ...node("target", CanvasNodeType.Video), metadata: { videoEditOperation: "concat" as const } };

        expect(supportsVideoAssetUpload(composition)).toBe(false);
        expect(supportsVideoAssetUpload(node("video", CanvasNodeType.Video))).toBe(true);
        expect(supportsVideoAssetUpload(node("image", CanvasNodeType.Image))).toBe(false);
    });

    test("normalizes every composition node to the shared square geometry", () => {
        const legacyNode = {
            ...node("target", CanvasNodeType.Video),
            width: 420,
            height: 340,
            metadata: { videoEditOperation: "concat" as const },
        };

        expect(normalizeVideoCompositionNode(legacyNode)).toMatchObject({
            width: VIDEO_COMPOSITION_NODE_SIZE,
            height: VIDEO_COMPOSITION_NODE_SIZE,
        });
        expect(normalizeVideoCompositionNode(node("video", CanvasNodeType.Video))).toEqual(node("video", CanvasNodeType.Video));
    });

    test("returns connected completed videos in deterministic shot order", () => {
        const target = { ...node("target", CanvasNodeType.Video), metadata: { videoEditOperation: "concat" as const } };
        const nodes = [
            target,
            node("later", CanvasNodeType.Video, "https://cdn.test/later.mp4", 2),
            node("first", CanvasNodeType.Video, "https://cdn.test/first.mp4", 1),
            node("empty", CanvasNodeType.Video),
            node("image", CanvasNodeType.Image, "https://cdn.test/image.png"),
        ];
        const connections: CanvasConnection[] = nodes.slice(1).map((source) => ({ id: `edge-${source.id}`, fromNodeId: source.id, toNodeId: target.id }));

        expect(getCompositionSourceVideos(target.id, nodes, connections).map((source) => source.id)).toEqual(["first", "later"]);
    });

    test("does not accept another composition output as a source", () => {
        const source = { ...node("source", CanvasNodeType.Video, "https://cdn.test/source.mp4"), metadata: { content: "https://cdn.test/source.mp4", videoEditOperation: "concat" as const } };
        const target = { ...node("target", CanvasNodeType.Video), metadata: { videoEditOperation: "concat" as const } };
        expect(getCompositionSourceVideos(target.id, [source, target], [{ id: "edge", fromNodeId: source.id, toNodeId: target.id }])).toEqual([]);
    });
});
