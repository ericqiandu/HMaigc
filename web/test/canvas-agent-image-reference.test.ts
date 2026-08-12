import { describe, expect, test } from "bun:test";

import { resolveCanvasAgentImageInput } from "../src/lib/canvas/canvas-agent-image-reference";

describe("canvas Agent image references", () => {
    test("uses a short-lived OSS URL for uploaded resources instead of expanding the image to base64", async () => {
        const calls: string[] = [];
        const result = await resolveCanvasAgentImageInput(
            { id: "image-1", type: "image", title: "图片1", dataUrl: "blob:http://localhost/large-image", storageKey: "resource:resource-1" },
            {
                getResourceOSSUrl: async (storageKey) => {
                    calls.push(`oss:${storageKey}`);
                    return "https://oss.example.com/resource-1?signature=short-lived";
                },
                imageToDataUrl: async () => {
                    calls.push("inline");
                    return "data:image/png;base64,very-large-payload";
                },
            },
        );

        expect(result).toBe("https://oss.example.com/resource-1?signature=short-lived");
        expect(calls).toEqual(["oss:resource:resource-1"]);
    });

    test("keeps inline conversion for images that have not been uploaded as backend resources", async () => {
        const result = await resolveCanvasAgentImageInput(
            { id: "image-local", type: "image", title: "本地图片", dataUrl: "blob:http://localhost/local-image", storageKey: "image:local-1" },
            {
                getResourceOSSUrl: async () => {
                    throw new Error("should not request an OSS URL");
                },
                imageToDataUrl: async () => "data:image/webp;base64,small-payload",
            },
        );

        expect(result).toBe("data:image/webp;base64,small-payload");
    });
});
