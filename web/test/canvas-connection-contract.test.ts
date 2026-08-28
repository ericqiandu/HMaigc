import { describe, expect, test } from "bun:test";

import { normalizeConnection, validateDirectedCanvasConnection } from "../src/lib/canvas/canvas-project-domain";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

function node(id: string, type: CanvasNodeType): CanvasNodeData {
    return {
        id,
        type,
        title: id,
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
    };
}

describe("canvas connection contract", () => {
    const video = node("video", CanvasNodeType.Video);
    const image = node("image", CanvasNodeType.Image);
    const audio = node("audio", CanvasNodeType.Audio);
    const nodes = [video, image, audio];

    test("rejects image and audio nodes downstream from a video output", () => {
        expect(normalizeConnection(video.id, image.id, nodes, "source")).toBeNull();
        expect(normalizeConnection(video.id, audio.id, nodes, "source")).toBeNull();
        expect(normalizeConnection(image.id, video.id, nodes, "target")).toBeNull();
        expect(normalizeConnection(audio.id, video.id, nodes, "target")).toBeNull();
    });

    test("keeps image and audio inputs to a video node available", () => {
        expect(normalizeConnection(image.id, video.id, nodes, "source")).toEqual({ fromNodeId: image.id, toNodeId: video.id });
        expect(normalizeConnection(video.id, image.id, nodes, "target")).toEqual({ fromNodeId: image.id, toNodeId: video.id });
        expect(normalizeConnection(video.id, audio.id, nodes, "target")).toEqual({ fromNodeId: audio.id, toNodeId: video.id });
    });

    test("returns a stable reason for invalid directed media output connections", () => {
        expect(validateDirectedCanvasConnection(video.id, image.id, nodes)).toEqual({
            ok: false,
            code: "video_output_media_target",
            message: "视频节点的输出不能连接图片或音频节点",
        });
        expect(validateDirectedCanvasConnection(video.id, audio.id, nodes)).toEqual({
            ok: false,
            code: "video_output_media_target",
            message: "视频节点的输出不能连接图片或音频节点",
        });
        expect(validateDirectedCanvasConnection(image.id, video.id, nodes)).toEqual({
            ok: true,
            connection: { fromNodeId: image.id, toNodeId: video.id },
        });
    });
});
