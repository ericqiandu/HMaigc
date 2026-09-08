import { describe, expect, test } from "bun:test";

import { createShortDramaPipeline } from "../src/lib/canvas/canvas-short-drama";

describe("short drama node lifecycle ownership", () => {
    test("keeps creative titles stable while lifecycle stays in metadata", () => {
        const result = createShortDramaPipeline({ x: 0, y: 0 });
        const styleNode = result.nodes.find((node) => node.metadata?.workflowKind === "styleboard");
        const scriptNode = result.nodes.find((node) => node.metadata?.workflowKind === "storyboard");

        expect(styleNode?.title).toBe("项目画风");
        expect(styleNode?.metadata?.status).toBe("idle");
        expect(scriptNode?.title).toBe("分镜脚本");
        expect(scriptNode?.metadata?.status).toBe("idle");
    });
});
