import "./setup-happy-dom";

import { describe, expect, test } from "bun:test";

import { canvasResourceDisplayUrl } from "../src/lib/canvas-media-playback";
import { resolveMediaUrl } from "../src/services/file-storage";
import { resolveImageUrl } from "../src/services/image-storage";
import { persistMediaByUserScope } from "../src/services/media-upload-policy";
import { resolveResourceUrl } from "../src/services/api/resources";

describe("media storage contract", () => {
    test("resource-backed canvas media renders from the authenticated direct endpoint immediately", async () => {
        expect(canvasResourceDisplayUrl("resource:cover-1", "https://expired.example/cover.png")).toBe("/api/resources/cover-1/file?direct=1");
        expect(await resolveResourceUrl("resource:cover-1", "https://expired.example/cover.png")).toBe("/api/resources/cover-1/file?direct=1");
        expect(await resolveImageUrl("resource:cover-1", "https://expired.example/cover.png")).toBe("/api/resources/cover-1/file?direct=1");
        expect(await resolveMediaUrl("resource:video-1", "https://expired.example/video.mp4")).toBe("/api/resources/video-1/file?direct=1");
    });

    test("authenticated uploads surface remote persistence failures without writing a local shadow copy", async () => {
        let localWrites = 0;

        await expect(
            persistMediaByUserScope("user-1", async () => {
                throw new Error("remote upload failed");
            }, async () => {
                localWrites += 1;
                return "local";
            }),
        ).rejects.toThrow("remote upload failed");
        expect(localWrites).toBe(0);
    });

    test("guest uploads use the explicit local persistence branch", async () => {
        let remoteWrites = 0;

        const result = await persistMediaByUserScope("guest", async () => {
            remoteWrites += 1;
            return "remote";
        }, async () => "local");

        expect(result).toBe("local");
        expect(remoteWrites).toBe(0);
    });
});
