import { describe, expect, test } from "bun:test";

import { selectVideoReferenceCandidates, type CanvasResourceReference } from "../src/lib/canvas/canvas-resource-references";

const reference = (nodeId: string, kind: CanvasResourceReference["kind"], previewUrl?: string): CanvasResourceReference => ({
    id: nodeId,
    nodeId,
    kind,
    label: nodeId,
    title: nodeId,
    previewUrl,
    active: false,
});

describe("canvas video reference candidates", () => {
    test("offers generated image, video, and audio resources while excluding unrelated or unusable nodes", () => {
        const candidates = selectVideoReferenceCandidates(
            [
                reference("target-video", "video", "/api/resources/target/file"),
                reference("image-1", "image", "/api/resources/image-1/file"),
                reference("video-1", "video", "/api/resources/video-1/file"),
                reference("audio-1", "audio", "/api/resources/audio-1/file"),
                reference("video-without-asset", "video"),
                reference("text-1", "text"),
            ],
            "target-video",
        );

        expect(candidates.map((item) => item.nodeId)).toEqual(["image-1", "video-1", "audio-1"]);
    });
});
