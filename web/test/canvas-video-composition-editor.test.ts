import { describe, expect, test } from "bun:test";

import { reconcileCompositionClips, resolveCompositionClips, splitCompositionClip } from "../src/lib/canvas/canvas-video-composition-editor";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

const video = (id: string, durationMs: number): CanvasNodeData => ({
    id,
    type: CanvasNodeType.Video,
    title: id,
    position: { x: 0, y: 0 },
    width: 320,
    height: 180,
    metadata: { content: `https://cdn.test/${id}.mp4`, durationMs },
});

describe("video composition editor domain", () => {
    test("retains edits and appends newly connected sources", () => {
        const saved = [{ id: "clip-a", sourceNodeId: "a", trimStartMs: 500, trimEndMs: 2500 }];
        const reconciled = reconcileCompositionClips([video("a", 3000), video("b", 4000)], saved);
        expect(reconciled[0]).toEqual(saved[0]);
        expect(reconciled[1]?.sourceNodeId).toBe("b");
    });

    test("resolves effective durations from source metadata", () => {
        const clips = [{ id: "clip", sourceNodeId: "a", trimStartMs: 1000, trimEndMs: 3500 }];
        expect(resolveCompositionClips(clips, [video("a", 5000)])[0]?.durationMs).toBe(2500);
    });

    test("splits one source into two renderable ranges", () => {
        const clips = [{ id: "clip", sourceNodeId: "a", trimStartMs: 1000, trimEndMs: 5000 }];
        const result = splitCompositionClip(clips, "clip", 3000);
        expect(result).toHaveLength(2);
        expect(result[0]).toMatchObject({ trimStartMs: 1000, trimEndMs: 3000 });
        expect(result[1]).toMatchObject({ trimStartMs: 3000, trimEndMs: 5000 });
    });
});
