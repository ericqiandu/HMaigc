import { expect, test } from "bun:test";

import { buildNodeMentionReferences } from "../src/lib/canvas/canvas-resource-references";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "../src/types/canvas";

test("resource-backed upload uses the authoritative resource endpoint for its dialog cover", () => {
    const uploadedImage = {
        id: "uploaded-image",
        type: CanvasNodeType.Image,
        title: "自定义上传图",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: {
            content: "https://expired-oss.example.com/upload.png?signature=expired",
            storageKey: "resource:upload-resource",
        },
    } satisfies CanvasNodeData;
    const videoNode = {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "视频",
        position: { x: 400, y: 0 },
        width: 320,
        height: 180,
    } satisfies CanvasNodeData;
    const connection = {
        id: "image-to-video",
        fromNodeId: uploadedImage.id,
        toNodeId: videoNode.id,
    } satisfies CanvasConnection;

    const [reference] = buildNodeMentionReferences(videoNode, [uploadedImage, videoNode], [connection]);

    expect(reference.previewUrl).toBe("/api/resources/upload-resource/file?direct=1");
});
