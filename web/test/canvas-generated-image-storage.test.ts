import { describe, expect, test } from "bun:test";

import { buildCanvasImageCommandEvidence } from "../src/lib/canvas/canvas-image-command-evidence";
import { buildGenerationTaskNodeResult, storeBackendGeneratedImage } from "../src/lib/canvas/canvas-generation-task-sync";
import type { GenerationTask } from "../src/services/api/task-center";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

const readSource = (relativePath: string) => Bun.file(new URL(relativePath, import.meta.url)).text();

describe("backend generated image storage", () => {
    test("preserves the backend resource identity without uploading the image again", async () => {
        const image = await storeBackendGeneratedImage({
            dataUrl: "/api/resources/image-resource-1/file",
            storageKey: "resource:image-resource-1",
            width: 1672,
            height: 940,
            bytes: 2298618,
            mimeType: "image/png",
        });

        expect(image).toEqual({
            url: "/api/resources/image-resource-1/file?direct=1",
            storageKey: "resource:image-resource-1",
            width: 1672,
            height: 940,
            bytes: 2298618,
            mimeType: "image/png",
        });
    });

    test("rejects a backend image result without a durable resource identity", async () => {
        await expect(
            storeBackendGeneratedImage({
                dataUrl: "data:image/png;base64,aW1hZ2U=",
                width: 1672,
                height: 940,
                bytes: 5,
                mimeType: "image/png",
            }),
        ).rejects.toThrow("后端图片任务结果缺少资源身份");
    });

    test("routes every backend image result through resource-aware storage", async () => {
        const sources = await Promise.all([readSource("../src/pages/canvas/canvas-image-generation-executor.ts"), readSource("../src/pages/canvas/use-canvas-generation-retry.ts"), readSource("../src/pages/canvas/use-canvas-media-tools.ts")]);

        for (const source of sources) {
            expect(source).not.toContain("uploadImage(image.dataUrl)");
        }
    });

    test("clears command evidence when a regular generation replaces a command result", async () => {
        const node: CanvasNodeData = {
            id: "image-node-1",
            type: CanvasNodeType.Image,
            title: "人像质感",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: {
                content: "/api/resources/portrait-result/file?direct=1",
                storageKey: "resource:portrait-result",
                status: "error",
                imageCommandEvidence: buildCanvasImageCommandEvidence({
                    sourceNodeId: "source-image",
                    command: "crop",
                    parameters: {},
                    prompt: "旧图片命令",
                    resourceId: "portrait-result",
                    width: 1672,
                    height: 940,
                    createdAt: "2026-09-08T08:00:01.000Z",
                }),
            },
        };
        const task: GenerationTask = {
            id: "regular-task",
            type: "canvas_image",
            status: "succeeded",
            prompt: "普通图片生成",
            inputJson: JSON.stringify({ mode: "image", metadata: { nodeId: node.id } }),
            resultJson: JSON.stringify({
                mode: "image",
                images: [
                    {
                        dataUrl: "/api/resources/regular-result/file",
                        storageKey: "resource:regular-result",
                        width: 1672,
                        height: 940,
                        bytes: 1024,
                        mimeType: "image/png",
                    },
                ],
            }),
            attempts: 1,
            createdAt: "2026-09-08T08:01:00.000Z",
            updatedAt: "2026-09-08T08:02:00.000Z",
        };

        const updated = await buildGenerationTaskNodeResult(node, task);

        expect(updated.metadata?.status).toBe("success");
        expect(updated.metadata?.storageKey).toBe("resource:regular-result");
        expect(updated.metadata?.imageCommandEvidence).toBeUndefined();
    });
});
