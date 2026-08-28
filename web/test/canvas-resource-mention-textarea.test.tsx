import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, createRef, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import {
    CanvasResourceMentionTextarea,
    type CanvasResourceMentionTextareaHandle,
} from "../src/components/canvas/canvas-resource-mention-textarea";
import type { CanvasResourceReference } from "../src/lib/canvas/canvas-resource-references";

const imageReference: CanvasResourceReference = {
    id: "image-1",
    nodeId: "image-1",
    kind: "image",
    label: "图片1",
    title: "小明角色图",
    active: true,
};

const unconnectedVideoReference: CanvasResourceReference = {
    id: "video-2",
    nodeId: "video-2",
    kind: "video",
    label: "视频1",
    title: "未连接的视频素材",
    previewUrl: "https://example.com/video.mp4",
    active: false,
};

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("CanvasResourceMentionTextarea", () => {
    test("opens resource suggestions for an inline at sign and inserts the mention at the caret", async () => {
        let latestValue = "这是小明，这是小亮";
        const editor = await renderEditor(latestValue, (value) => {
            latestValue = value;
        });

        await act(async () => {
            editor.textContent = "这是小明@，这是小亮";
            setEditorCaret(editor, 5);
            editor.dispatchEvent(new Event("input", { bubbles: true }));
        });

        expect(document.querySelector("[data-canvas-resource-mention-menu='true']")).not.toBeNull();

        await act(async () => {
            editor.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Enter" }));
        });

        expect(latestValue).toBe("这是小明@[node:image-1] ，这是小亮");
    });

    test("uses the last editor selection after a toolbar takes focus", async () => {
        let latestValue = "这是小明，这是小亮";
        const handleRef = createRef<CanvasResourceMentionTextareaHandle>();
        const editor = await renderEditor(latestValue, (value) => {
            latestValue = value;
        }, handleRef);

        await act(async () => {
            setEditorCaret(editor, 4);
            editor.dispatchEvent(new Event("pointerup", { bubbles: true }));
        });
        document.getSelection()?.removeAllRanges();

        await act(async () => {
            handleRef.current?.replaceSelection("@[node:image-1] ");
        });

        expect(latestValue).toBe("这是小明@[node:image-1] ，这是小亮");
    });

    test("connects an unconnected canvas resource before inserting it at the caret", async () => {
        let latestValue = "这是小明，这是小亮";
        const selectedNodeIds: string[] = [];
        const editor = await renderEditor(
            latestValue,
            (value) => {
                latestValue = value;
            },
            createRef<CanvasResourceMentionTextareaHandle>(),
            {
                references: [imageReference, unconnectedVideoReference],
                onReferenceSelect: (reference) => {
                    selectedNodeIds.push(reference.nodeId);
                    return true;
                },
            },
        );

        await act(async () => {
            editor.textContent = "这是小明@视频，这是小亮";
            setEditorCaret(editor, 7);
            editor.dispatchEvent(new Event("input", { bubbles: true }));
        });
        await act(async () => {
            editor.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Enter" }));
        });

        expect(selectedNodeIds).toEqual(["video-2"]);
        expect(latestValue).toBe("这是小明@[node:video-2] ，这是小亮");
    });

    test("keeps the prompt unchanged when an unconnected resource cannot be connected", async () => {
        let latestValue = "这是小明，这是小亮";
        const editor = await renderEditor(
            latestValue,
            (value) => {
                latestValue = value;
            },
            createRef<CanvasResourceMentionTextareaHandle>(),
            { references: [imageReference, unconnectedVideoReference], onReferenceSelect: () => false },
        );

        await act(async () => {
            editor.textContent = "这是小明@视频，这是小亮";
            setEditorCaret(editor, 7);
            editor.dispatchEvent(new Event("input", { bubbles: true }));
            editor.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Enter" }));
        });

        expect(latestValue).toBe("这是小明@视频，这是小亮");
    });

    test("does not select a mention while the user is composing text", async () => {
        let latestValue = "这是小明，这是小亮";
        const editor = await renderEditor(latestValue, (value) => {
            latestValue = value;
        });

        await act(async () => {
            editor.textContent = "这是小明@，这是小亮";
            setEditorCaret(editor, 5);
            editor.dispatchEvent(new Event("input", { bubbles: true }));
            editor.dispatchEvent(new Event("compositionstart", { bubbles: true }));
            editor.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "Enter" }));
        });

        expect(latestValue).toBe("这是小明@，这是小亮");
    });

    test("replaces a failed media preview with a semantic fallback icon", async () => {
        await renderEditor("", () => undefined, createRef<CanvasResourceMentionTextareaHandle>(), {
            references: [imageReference, unconnectedVideoReference],
        });
        const editor = document.querySelector<HTMLElement>("[role='textbox']");
        if (!editor) throw new Error("素材引用编辑器未渲染");
        await act(async () => {
            editor.textContent = "@视频";
            setEditorCaret(editor, 3);
            editor.dispatchEvent(new Event("input", { bubbles: true }));
        });
        const preview = document.querySelector<HTMLVideoElement>("[data-canvas-resource-preview='video']");
        if (!preview) throw new Error("视频预览未渲染");

        await act(async () => preview.dispatchEvent(new Event("error", { bubbles: true })));

        expect(document.querySelector("[data-canvas-resource-preview-fallback='video']")).not.toBeNull();
    });
});

type RenderEditorOptions = {
    references?: CanvasResourceReference[];
    onReferenceSelect?: (reference: CanvasResourceReference) => boolean;
};

async function renderEditor(
    initialValue: string,
    onValue: (value: string) => void,
    handleRef = createRef<CanvasResourceMentionTextareaHandle>(),
    options: RenderEditorOptions = {},
) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    function Harness() {
        const [value, setValue] = useState(initialValue);
        return createElement(CanvasResourceMentionTextarea, {
            value,
            references: options.references || [imageReference],
            onReferenceSelect: options.onReferenceSelect,
            editorHandleRef: handleRef,
            className: "canvas-resource-mention-test-editor",
            "aria-label": "素材引用提示词",
            onChange: (nextValue: string) => {
                onValue(nextValue);
                setValue(nextValue);
            },
        });
    }

    await act(async () => root?.render(createElement(Harness)));
    const editor = host.querySelector<HTMLElement>("[role='textbox']");
    if (!editor) throw new Error("素材引用编辑器未渲染");
    return editor;
}

function setEditorCaret(editor: HTMLElement, offset: number) {
    const textNode = editor.firstChild;
    if (!textNode) throw new Error("编辑器缺少文本节点");
    const range = document.createRange();
    range.setStart(textNode, offset);
    range.collapse(true);
    const selection = document.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    editor.focus();
}
