import { describe, expect, test } from "bun:test";

import { selectVideoGenerationContext, shouldRestoreStoredVideoReferenceImages, videoGenerationModeConflictReason, videoModeMetadataPatch } from "../src/lib/canvas/canvas-video-generation-mode";

const image = (id: string) => ({ id, name: `${id}.png`, type: "image/png", dataUrl: `https://cdn.example.com/${id}.png` });
const video = { id: "video-1", name: "reference.mp4", type: "video/mp4", url: "https://cdn.example.com/reference.mp4" };
const audio = { id: "audio-1", name: "voice.mp3", type: "audio/mpeg", url: "https://cdn.example.com/voice.mp3" };
const context = {
    prompt: "生成镜头",
    referenceImages: [image("image-1"), image("image-2"), image("image-3")],
    referenceVideos: [video],
    referenceAudios: [audio],
    characterReferences: [],
    resolvedCharacterVersions: [],
    resolvedCharacterVoices: [],
    textCount: 0,
    imageCount: 3,
    videoCount: 1,
    audioCount: 1,
};

describe("video generation mode contract", () => {
    test("text mode strips every media reference", () => {
        const selected = selectVideoGenerationContext(
            { videoGenerationMode: "text" },
            { ...context, referenceImages: [], imageCount: 0 },
        );
        expect(selected.referenceImages).toHaveLength(0);
        expect(selected.referenceVideos).toHaveLength(0);
        expect(selected.referenceAudios).toHaveLength(0);
    });

    test("text mode rejects a connected image instead of silently discarding it", () => {
        expect(() => selectVideoGenerationContext({ videoGenerationMode: "text" }, context)).toThrow("已连接图片，断开后可使用文生视频");
    });

    test("submission conflict exists only while text mode has a connected image", () => {
        expect(videoGenerationModeConflictReason({ videoGenerationMode: "text" }, { image: 1, video: 0, audio: 0 })).toBe("已连接图片，断开后可使用文生视频");
        expect(videoGenerationModeConflictReason({ videoGenerationMode: "text" }, { image: 0, video: 0, audio: 0 })).toBeUndefined();
        expect(videoGenerationModeConflictReason({ videoGenerationMode: "image_reference" }, { image: 1, video: 0, audio: 0 })).toBeUndefined();
    });

    test("first and last frame mode preserves only the ordered frame images", () => {
        const selected = selectVideoGenerationContext({ videoGenerationMode: "first_last_frame", videoStartFrameNodeId: "image-2", videoEndFrameNodeId: "image-1" }, context);
        expect(selected.referenceImages.map((item) => item.id)).toEqual(["image-2", "image-1"]);
        expect(selected.referenceVideos).toHaveLength(0);
        expect(selected.referenceAudios).toHaveLength(0);
    });

    test("image reference excludes video and audio while omni reference preserves all media", () => {
        const imageReference = selectVideoGenerationContext({ videoGenerationMode: "image_reference" }, context);
        expect(imageReference.referenceImages).toHaveLength(3);
        expect(imageReference.referenceVideos).toHaveLength(0);
        expect(imageReference.referenceAudios).toHaveLength(0);
        expect(selectVideoGenerationContext({ videoGenerationMode: "omni_reference" }, context)).toEqual(context);
    });

    test("mode patch stores explicit intent and frame roles", () => {
        expect(videoModeMetadataPatch({ mode: "first_last_frame", frameNodeIds: ["image-1", "image-2"], counts: { image: 2, video: 0, audio: 0 } })).toEqual({
            videoGenerationMode: "first_last_frame",
            videoEditOperation: "image_to_video",
            videoStartFrameNodeId: "image-1",
            videoEndFrameNodeId: "image-2",
        });
    });

    test("missing required references fail explicitly", () => {
        expect(() => selectVideoGenerationContext({ videoGenerationMode: "image_reference" }, { ...context, referenceImages: [], imageCount: 0 })).toThrow("图片参考模式需要至少一张已连接的参考图片");
        expect(() => videoModeMetadataPatch({ mode: "first_last_frame", frameNodeIds: ["image-1"], counts: { image: 1, video: 0, audio: 0 } })).toThrow("首尾帧模式需要两张不同的参考图片");
    });

    test("omni reference retry never reclassifies stored video URLs as images", () => {
        expect(shouldRestoreStoredVideoReferenceImages({ videoGenerationMode: "omni_reference" }, 0)).toBe(false);
        expect(shouldRestoreStoredVideoReferenceImages({ videoGenerationMode: "image_reference" }, 0)).toBe(true);
        expect(shouldRestoreStoredVideoReferenceImages({ videoGenerationMode: "image_reference" }, 1)).toBe(false);
    });
});
