import { describe, expect, test } from "bun:test";

import {
    canvasUsesRevisionedMutations,
    remoteCanvasCreationRequired,
} from "@/lib/canvas/canvas-persistence-policy";

describe("canvas persistence policy", () => {
    test("personal and team canvases both use revisioned mutations after loading", () => {
        expect(canvasUsesRevisionedMutations(false, "personal-canvas")).toBe(false);
        expect(canvasUsesRevisionedMutations(true, "")).toBe(false);
        expect(canvasUsesRevisionedMutations(true, "personal-canvas")).toBe(true);
        expect(canvasUsesRevisionedMutations(true, "team-canvas")).toBe(true);
    });

    test("the legacy full-document endpoint is used only to create a missing remote canvas", () => {
        const remoteVersions = new Map([["existing-canvas", "2026-08-18T00:00:00Z"]]);
        expect(remoteCanvasCreationRequired(remoteVersions, "new-canvas")).toBe(true);
        expect(remoteCanvasCreationRequired(remoteVersions, "existing-canvas")).toBe(false);
    });
});
