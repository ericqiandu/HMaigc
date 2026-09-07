import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { CanvasNodeType, type CanvasNodeData, type CanvasWorkspaceMode } from "@/types/canvas";
import { resolveCanvasTools, type CanvasToolId } from "./canvas-tool-registry";

function node(type: CanvasNodeType, content = "", metadata: NonNullable<CanvasNodeData["metadata"]> = {}): CanvasNodeData {
    return {
        id: `${type}-1`,
        type,
        title: `${type} node`,
        position: { x: 0, y: 0 },
        width: 320,
        height: 180,
        metadata: { ...metadata, content },
    };
}

function visibleToolIds(target: CanvasNodeData, workspaceMode: CanvasWorkspaceMode = "professional", extractingVideoFrame = false): CanvasToolId[] {
    return resolveCanvasTools({ node: target, workspaceMode, extractingVideoFrame }).filter((tool) => tool.visible).map((tool) => tool.id);
}

describe("resolveCanvasTools", () => {
    test("keeps empty image limited to upload and node controls", () => {
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Image)), ["info", "delete", "edit", "uploadImage"]);
    });

    test("keeps generated image actions in the current toolbar order", () => {
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Image, "https://assets.example.com/image.png")), [
            "info", "delete", "saveAsset", "download", "edit", "copyPrompt", "reversePrompt", "replace", "resize",
            "annotation", "maskEdit", "emotion", "crop", "split", "upscale", "superResolve", "angle", "view",
        ]);
    });

    test("keeps professional video actions and disables frame extraction while busy", () => {
        const target = node(CanvasNodeType.Video, "https://assets.example.com/video.mp4");
        assert.deepEqual(visibleToolIds(target), ["info", "delete", "extractLastFrame", "saveAsset", "download", "edit", "uploadVideo"]);
        assert.deepEqual(resolveCanvasTools({ node: target, workspaceMode: "professional", extractingVideoFrame: true }).find((tool) => tool.id === "extractLastFrame"), {
            id: "extractLastFrame", group: "edit", order: 30, visible: true, disabled: true, disabledReason: "正在截取尾帧",
        });
        assert.deepEqual(visibleToolIds(target, "simple"), ["info", "delete", "saveAsset", "download", "edit", "uploadVideo"]);
    });

    test("keeps audio text and config actions separated by node type and mode", () => {
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Audio, "https://assets.example.com/audio.mp3")), ["info", "delete", "download", "uploadAudio"]);
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Text, "draft")), ["info", "delete", "saveAsset", "edit", "editText", "generateImage", "decreaseFont", "increaseFont"]);
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Config)), ["info", "delete", "config"]);
        assert.deepEqual(visibleToolIds(node(CanvasNodeType.Config), "simple"), ["info", "delete"]);
    });

    test("only exposes retry for retryable errors", () => {
        assert.ok(visibleToolIds(node(CanvasNodeType.Text, "draft", { status: "error" })).includes("retry"));
        assert.ok(!visibleToolIds(node(CanvasNodeType.Text, "draft", { status: "error", generationErrorCode: "sensitive_words_detected" })).includes("retry"));
    });
});
