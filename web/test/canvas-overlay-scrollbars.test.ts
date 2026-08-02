import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const overlayStyles = readFileSync(new URL("../src/styles/canvas-overlays.css", import.meta.url), "utf8");
const hoverToolbar = readFileSync(
    new URL("../src/components/canvas/canvas-node-hover-toolbar.tsx", import.meta.url),
    "utf8",
);
const compositionEditorStyles = readFileSync(
    new URL("../src/components/canvas/canvas-video-composition-editor.css", import.meta.url),
    "utf8",
);

describe("canvas overlay scrollbar contract", () => {
    test("keeps canvas overlays scrollable without rendering native scrollbar tracks", () => {
        expect(overlayStyles).toContain(".canvas-overlay-scroll-surface");
        expect(overlayStyles).toContain("scrollbar-width: none");
        expect(overlayStyles).toContain("::-webkit-scrollbar");
        expect(overlayStyles).toContain("display: none");
    });

    test("does not opt the node hover toolbar into the visible thin scrollbar style", () => {
        const toolbarClass = hoverToolbar.match(/className=\{`aceternity-floating-dock ([^`]+)`\}/)?.[1] ?? "";

        expect(toolbarClass).toContain("canvas-overlay-scroll-surface");
        expect(toolbarClass).not.toContain("thin-scrollbar");
    });

    test("does not render a visible scrollbar in the video composition dialog timeline", () => {
        expect(compositionEditorStyles).not.toContain("scrollbar-width: thin");
        expect(compositionEditorStyles).not.toContain("scrollbar-color:");
    });
});
