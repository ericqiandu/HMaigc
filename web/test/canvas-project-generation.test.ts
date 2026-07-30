import { describe, expect, test } from "bun:test";

import { resetInterruptedGeneration } from "../src/lib/canvas/canvas-project-generation";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

describe("resetInterruptedGeneration", () => {
    test("migrates audio nodes to the production audio presentation minimum size", () => {
        const node = audioNode({ width: 340, height: 120 });

        const [normalized] = resetInterruptedGeneration([node]);

        expect(normalized.width).toBe(340);
        expect(normalized.height).toBe(220);
    });

    test("preserves user dimensions when an audio node is already larger than the minimum", () => {
        const node = audioNode({ width: 480, height: 300 });

        const [normalized] = resetInterruptedGeneration([node]);

        expect(normalized.width).toBe(480);
        expect(normalized.height).toBe(300);
    });
});

function audioNode(size: Pick<CanvasNodeData, "width" | "height">): CanvasNodeData {
    return {
        id: "audio-node",
        type: CanvasNodeType.Audio,
        title: "音频节点",
        position: { x: 0, y: 0 },
        width: size.width,
        height: size.height,
        metadata: { status: "idle" },
    };
}
