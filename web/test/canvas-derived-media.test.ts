import { describe, expect, test } from "bun:test";

import { buildVideoLastFrameMetadata } from "../src/lib/canvas/canvas-derived-media";
import type { CanvasNodeMetadata } from "../src/types/canvas";

describe("canvas derived media provenance", () => {
    test("records the source video without creating a normal media connection", () => {
        const imageMetadata: CanvasNodeMetadata = { content: "/api/resources/frame/file", mimeType: "image/png" };
        expect(buildVideoLastFrameMetadata("video-1", imageMetadata)).toEqual({
            content: "/api/resources/frame/file",
            mimeType: "image/png",
            mediaProvenance: { kind: "video_last_frame", sourceNodeId: "video-1" },
        });
    });
});
