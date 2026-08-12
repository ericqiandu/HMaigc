import { describe, expect, test } from "bun:test";

import { updateStoryboardRowsAndLinkedVideos } from "../src/lib/canvas/canvas-storyboard-video-sync";
import { CanvasNodeType, type CanvasNodeData, type StoryboardRow } from "../src/types/canvas";

function storyboardRow(patch: Partial<StoryboardRow> = {}): StoryboardRow {
    return {
        id: "shot-1",
        shotNumber: 1,
        durationSeconds: 6,
        plotDescription: "旧画面描述",
        dialogue: "",
        characters: [],
        shotSize: "",
        emotion: "",
        lightingAndAtmosphere: "",
        audioEffects: "",
        camera: "",
        motion: "",
        timeBeats: "",
        imageGenerationPrompt: "",
        videoMotionPrompt: "",
        negativePrompt: "",
        referenceNodeIds: [],
        videoNodeId: "video-1",
        status: "idle",
        ...patch,
    };
}

function node(id: string, type: CanvasNodeType, metadata: CanvasNodeData["metadata"]): CanvasNodeData {
    return {
        id,
        type,
        title: type === CanvasNodeType.Script ? "分镜脚本" : "镜头 1 · 视频",
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata,
    };
}

describe("storyboard linked video synchronization", () => {
    test("updates the existing linked video when the storyboard row changes", () => {
        const rows = [storyboardRow()];
        const script = node("script-1", CanvasNodeType.Script, {
            storyboard: { rows, visibleColumns: ["plotDescription"], referenceNodeIds: [] },
        });
        const video = node("video-1", CanvasNodeType.Video, {
            prompt: "旧提示词",
            composerContent: "旧提示词",
            content: "https://assets.example.com/existing.mp4",
            status: "success",
            videoEditOperation: "text_to_video",
        });

        const result = updateStoryboardRowsAndLinkedVideos([script, video], "script-1", (current) =>
            current.map((row) => ({
                ...row,
                durationSeconds: 9,
                plotDescription: "新的画面描述",
                videoMotionPrompt: "新的运动提示词",
            })),
        );
        const synchronized = result.find((candidate) => candidate.id === "video-1");
        const updatedScript = result.find((candidate) => candidate.id === "script-1");

        expect(updatedScript?.metadata?.storyboard?.rows[0]?.videoMotionPrompt).toBe("新的运动提示词");
        expect(synchronized?.title).toBe("镜头 1 · 视频");
        expect(synchronized?.metadata?.prompt).toBe("新的运动提示词");
        expect(synchronized?.metadata?.composerContent).toBe("新的运动提示词");
        expect(synchronized?.metadata?.seconds).toBe("9");
        expect(synchronized?.metadata?.content).toBe("https://assets.example.com/existing.mp4");
    });

    test("does not rewrite unrelated video nodes", () => {
        const unrelated = node("video-2", CanvasNodeType.Video, { prompt: "独立视频提示词", composerContent: "独立视频提示词" });

        const [result] = updateStoryboardRowsAndLinkedVideos([unrelated], "script-1", (current) => current);

        expect(result).toBe(unrelated);
    });
});
