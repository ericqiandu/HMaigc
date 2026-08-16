import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { CanvasAssistantReference } from "../src/types/canvas";

let AssistantReferenceChip: typeof import("../src/components/canvas/canvas-assistant-reference-chip").CanvasAssistantReferenceChip;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasAssistantReferenceChip: AssistantReferenceChip } = await import("../src/components/canvas/canvas-assistant-reference-chip"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("文本节点引用显示可辨识的完整标题而不是单字符圆点", async () => {
    const reference: CanvasAssistantReference = {
        id: "script-1",
        type: "text",
        title: "冰点可乐 · 30秒TVC完整剧本",
        text: "冰点一到，夏天不燥",
    };

    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(AssistantReferenceChip, { item: reference, onRemove: () => undefined })));

    expect(host.textContent).toContain("冰点可乐 · 30秒TVC完整剧本");
    expect(host.querySelector('[title="冰点可乐 · 30秒TVC完整剧本"]')).not.toBeNull();
    expect(host.querySelector('button[aria-label="移除引用：冰点可乐 · 30秒TVC完整剧本"]')).not.toBeNull();
});
