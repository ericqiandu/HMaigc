import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasNodeEmotionPanel } from "../src/components/canvas/canvas-node-emotion-panel";
import { neutralEmotionPreset } from "../src/lib/canvas/canvas-emotion";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("CanvasNodeEmotionPanel", () => {
    test("shows the selected person without allocating a WebGL preview canvas", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(createElement(CanvasNodeEmotionPanel, {
                dataUrl: "data:image/png;base64,selected-person",
                imageWidth: 320,
                imageHeight: 180,
                characters: [{ id: "character-1", name: "角色1", faceBox: { id: "face-1", x: 80, y: 20, width: 80, height: 80, source: "detected" } }],
                activeCharacterId: "character-1",
                preset: neutralEmotionPreset,
                generating: false,
                onSelectCharacter: () => undefined,
                onManualSelect: () => undefined,
                onPresetChange: () => undefined,
                onClose: () => undefined,
                onConfirm: () => undefined,
            }));
            await new Promise((resolve) => setTimeout(resolve, 20));
        });

        const preview = document.querySelector<HTMLImageElement>('img[alt="角色1人物参考"]');
        expect(preview?.src).toBe("data:image/png;base64,selected-person");
        expect(document.body.textContent).toContain("人物参考 · 中性");
        expect(document.querySelector("canvas")).toBeNull();
    });
});
