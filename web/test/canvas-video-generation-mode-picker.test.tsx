import "./setup-happy-dom";

import { act } from "react";
import { afterEach, beforeAll, expect, test } from "bun:test";
import type { Root } from "react-dom/client";

let Picker: typeof import("../src/components/canvas/canvas-video-generation-mode-picker").CanvasVideoGenerationModePicker;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasVideoGenerationModePicker: Picker } = await import("../src/components/canvas/canvas-video-generation-mode-picker"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("connected image disables text-to-video until the image is disconnected", async () => {
    await renderPicker(1);
    await act(async () => trigger().click());

    const connectedOption = textModeOption();
    expect(connectedOption.disabled).toBe(true);
    expect(connectedOption.getAttribute("aria-label")).toBe("文生视频：已连接图片，断开后可使用文生视频");

    await renderPicker(0);

    const disconnectedOption = textModeOption();
    expect(disconnectedOption.disabled).toBe(false);
    expect(disconnectedOption.getAttribute("aria-label")).toBe("文生视频");
});

async function renderPicker(imageCount: number) {
    if (!root) {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
    }
    await act(async () => {
        root?.render(
            <Picker
                metadata={{ videoGenerationMode: "text" }}
                frameOptions={[]}
                referenceCounts={{ image: imageCount, video: 0, audio: 0 }}
                onMetadataChange={() => undefined}
            />,
        );
    });
}

function trigger(): HTMLButtonElement {
    const element = document.querySelector<HTMLButtonElement>('[aria-label="视频生成模式：文生视频"]');
    if (!element) throw new Error("未找到视频生成模式按钮");
    return element;
}

function textModeOption(): HTMLButtonElement {
    const element = [...document.querySelectorAll<HTMLButtonElement>(".canvas-video-mode-option")].find((button) => button.textContent?.includes("文生视频"));
    if (!element) throw new Error("未找到文生视频选项");
    return element;
}
