import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { act, createElement, createRef } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { CanvasImageEmotionPayload } from "../src/components/canvas/canvas-node-emotion-panel";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

const resolvedSource = "data:image/png;base64,resolved-oss-image";
const detectionInputs: string[] = [];

mock.module("@/services/image-storage", () => ({
    imageToDataUrl: async (image: { url?: string; storageKey?: string }) => {
        if (image.storageKey !== "resource:oss-image-1") throw new Error("missing resource storage key");
        return resolvedSource;
    },
}));

mock.module("@/lib/canvas/canvas-face-detection", () => ({
    detectCanvasFaces: async (source: string) => {
        detectionInputs.push(source);
        if (source !== resolvedSource) throw new Error("raw resource URL is not browser-readable");
        return {
            faces: [{ id: "face-1", x: 20, y: 20, width: 80, height: 80, source: "detected" as const }],
            imageWidth: 320,
            imageHeight: 180,
        };
    },
}));

mock.module("@/lib/canvas/canvas-emotion", () => ({
    neutralEmotionPreset: { id: "neutral", intimacy: 0, arousal: 0, label: "中性", prompt: "保持中性表情" },
    canvasEmotionPresets: Array.from({ length: 25 }, (_, index) => ({
        id: index === 12 ? "neutral" : `emotion-${index}`,
        intimacy: 2 - (index % 5),
        arousal: 2 - Math.floor(index / 5),
        label: index === 12 ? "中性" : `情绪${index}`,
        prompt: index === 12 ? "保持中性表情" : `情绪提示${index}`,
    })),
    buildEmotionImageArtifacts: async () => ({
        sourceDataUrl: "data:image/png;base64,face-region",
        maskDataUrl: "data:image/png;base64,face-mask",
        characterDataUrl: "data:image/jpeg;base64,character-face",
        editRegion: { x: 0, y: 0, width: 160, height: 160 },
        imageWidth: 320,
        imageHeight: 180,
    }),
    buildEmotionPrompt: () => "调整人物情绪",
    compositeEmotionImage: async () => "data:image/png;base64,composited-emotion",
    emotionGenerationSize: () => "1024x1024",
    findEmotionPreset: () => undefined,
    emotionBlendshapes: () => ({}),
    clampAxis: (value: number) => value,
    clampFaceBox: (box: { id: string; x: number; y: number; width: number; height: number; source: "detected" | "manual" }) => box,
}));

const { CanvasEmotionWorkspace } = await import("../src/components/canvas/canvas-emotion-workspace");

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    detectionInputs.length = 0;
    document.body.replaceChildren();
});

describe("CanvasEmotionWorkspace", () => {
    test("resolves an OSS-backed image before face detection", async () => {
        await renderWorkspace(() => undefined);

        expect(detectionInputs).toEqual([resolvedSource]);
        expect(document.body.textContent).toContain("识别到 1 张人脸，请选择人物");
    });

    test("keeps the resolved OSS source for final emotion compositing", async () => {
        const payloads: CanvasImageEmotionPayload[] = [];
        await renderWorkspace((payload) => payloads.push(payload));

        const faceButton = requiredButton("选择此人脸");
        await act(async () => faceButton.click());
        const confirmButton = requiredButton("生成");
        await act(async () => confirmButton.click());

        expect(payloads).toHaveLength(1);
        expect(payloads[0]?.fullSourceDataUrl).toBe(resolvedSource);
    });
});

async function renderWorkspace(onConfirm: (payload: CanvasImageEmotionPayload) => void) {
    const host = document.createElement("div");
    const canvas = document.createElement("div");
    document.body.append(host, canvas);
    const containerRef = createRef<HTMLDivElement>();
    containerRef.current = canvas;
    root = createRoot(host);
    await act(async () => {
        root?.render(createElement(CanvasEmotionWorkspace, {
            node: ossImageNode(),
            viewport: { x: 0, y: 0, k: 1 },
            containerRef,
            onClose: () => undefined,
            onConfirm,
        }));
        await new Promise((resolve) => setTimeout(resolve, 30));
    });
}

function requiredButton(name: string) {
    const button = Array.from(document.querySelectorAll("button")).find((item) => item.getAttribute("aria-label") === name || item.textContent?.trim() === name);
    if (!button) throw new Error(`缺少按钮：${name}`);
    return button;
}

function ossImageNode(): CanvasNodeData {
    return {
        id: "image-node-1",
        type: CanvasNodeType.Image,
        title: "OSS 人物图片",
        position: { x: 40, y: 60 },
        width: 320,
        height: 180,
        metadata: {
            content: "https://expired-provider.example/image.png",
            storageKey: "resource:oss-image-1",
        },
    };
}
