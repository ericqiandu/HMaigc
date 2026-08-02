import { describe, expect, test } from "vitest";

import { waitForCanvasGeneration } from "@/lib/canvas/canvas-agent-generation-wait";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import type { CanvasAgentSnapshot } from "@/lib/canvas/canvas-agent-ops";

function snapshot(node: CanvasNodeData): CanvasAgentSnapshot {
    return { title: "test", viewport: { x: 0, y: 0, k: 1 }, selectedNodeIds: [], nodes: [node], connections: [] };
}

function videoNode(metadata: CanvasNodeData["metadata"]): CanvasNodeData {
    return { id: "video-1", type: CanvasNodeType.Video, title: "video", position: { x: 0, y: 0 }, width: 420, height: 236, metadata };
}

describe("waitForCanvasGeneration", () => {
    test("does not complete until the requested media node has a real asset", async () => {
        let current = snapshot(videoNode({ status: "loading", taskStatus: "running", taskProgress: 35 }));
        const pending = waitForCanvasGeneration(() => current, ["video-1"], { timeoutMs: 500, pollIntervalMs: 5 });
        setTimeout(() => {
            current = snapshot(videoNode({ status: "success", taskStatus: "succeeded", taskProgress: 100, content: "/api/resources/video-1/file", storageKey: "resource:video-1" }));
        }, 10);

        await expect(pending).resolves.toMatchObject({ completed: true, nodes: [{ id: "video-1", hasAsset: true, taskStatus: "succeeded" }] });
    });

    test("fails explicitly when generation reaches a terminal failure", async () => {
        const current = snapshot(videoNode({ status: "error", taskStatus: "failed", errorDetails: "provider rejected request" }));
        await expect(waitForCanvasGeneration(() => current, ["video-1"], { timeoutMs: 500, pollIntervalMs: 10 })).rejects.toThrow("provider rejected request");
    });
});
