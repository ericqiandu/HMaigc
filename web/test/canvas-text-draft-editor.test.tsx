import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { CanvasTextDraftEditor } from "../src/components/canvas/canvas-text-draft-editor";

let root: Root | null = null;
let host: HTMLDivElement | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    host?.remove();
    root = null;
    host = null;
});

describe("CanvasTextDraftEditor", () => {
    test("keeps rapid typing local and commits the final draft once on blur", async () => {
        const commits: string[] = [];
        host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () => {
            root?.render(
                createElement(CanvasTextDraftEditor, {
                    nodeId: "text-1",
                    value: "old",
                    references: [],
                    className: "text-draft-test",
                    "aria-label": "节点文本草稿",
                    onCommit: (_nodeId: string, value: string) => commits.push(value),
                    onStopEditing: () => undefined,
                }),
            );
        });

        const textarea = host.querySelector<HTMLTextAreaElement>("textarea");
        expect(textarea).not.toBeNull();
        textarea?.focus();
        const setNativeValue = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
        for (const value of ["n", "ne", "new draft"]) {
            await act(async () => {
                if (!textarea) return;
                setNativeValue?.call(textarea, value);
                textarea.dispatchEvent(new Event("input", { bubbles: true }));
            });
        }

        expect(commits).toEqual([]);
        await act(async () => textarea?.blur());
        expect(commits).toEqual(["new draft"]);
    });
});
