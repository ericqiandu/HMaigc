import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { CanvasNodeSurface } from "../src/components/canvas/canvas-node-surface";
import { canvasThemes } from "../src/lib/canvas-theme";

describe("CanvasNodeSurface", () => {
    test("renders one flat clipped surface with explicit visual state", () => {
        const markup = renderToStaticMarkup(
            <CanvasNodeSurface state="selected" theme={canvasThemes.dark} flushContent>
                <span className="test-node-content">内容</span>
            </CanvasNodeSurface>,
        );

        expect(markup).toContain("data-canvas-node-surface");
        expect(markup).toContain('data-state="selected"');
        expect(markup).toContain("canvas-node-outline");
        expect(markup).toContain("overflow-clip");
        expect(markup).toContain("rounded-[12px]");
        expect(markup).not.toContain("aceternity-comet");
    });
});
