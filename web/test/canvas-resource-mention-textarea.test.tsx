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
});

async function renderEditor(
    initialValue: string,
    onValue: (value: string) => void,
    handleRef = createRef<CanvasResourceMentionTextareaHandle>(),
) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    function Harness() {
        const [value, setValue] = useState(initialValue);
        return createElement(CanvasResourceMentionTextarea, {
            value,
            references: [imageReference],
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
