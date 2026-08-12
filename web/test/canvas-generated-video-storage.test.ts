import { describe, expect, test } from "bun:test";

import { storeBackendGeneratedVideo } from "../src/lib/canvas/canvas-generation-task-sync";

describe("backend generated video storage", () => {
    test("preserves the backend resource identity instead of presenting the video as unsynchronized", async () => {
        const video = await storeBackendGeneratedVideo({
            dataUrl: "/api/resources/video-resource-1/file",
            storageKey: "resource:video-resource-1",
            width: 854,
            height: 480,
            durationMs: 4064,
            bytes: 123456,
            mimeType: "video/mp4",
        });

        expect(video).toEqual({
            url: "/api/resources/video-resource-1/file",
            storageKey: "resource:video-resource-1",
            width: 854,
            height: 480,
            durationMs: 4064,
            bytes: 123456,
            mimeType: "video/mp4",
        });
    });
});
