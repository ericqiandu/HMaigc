import { describe, expect, test } from "bun:test";

import { assertCanvasImagePromptTaskEvidence, buildPortraitTextureImageCommand } from "@/lib/canvas/canvas-image-prompt-command";
import type { CanvasImagePromptTaskEvidenceV1 } from "@/types/canvas";

function evidence(): CanvasImagePromptTaskEvidenceV1 {
    return {
        schemaVersion: "image-prompt/v1",
        command: buildPortraitTextureImageCommand("source-1", 7),
        response: {
            schemaVersion: "image-prompt/v1",
            imagePrompt: "保持人物身份与构图，提升真实皮肤、光影和材质细节。",
            trace: {
                requestId: "request-1",
                correlationId: "task-1",
                specialist: "image_prompt_specialist",
                evidenceDigest: "1".repeat(64),
                model: {
                    catalogRecordId: "model-record-1",
                    modelKey: "deepseek-chat",
                    configurationRevision: "2026-09-07T10:00:00Z",
                    providerEndpointVersionId: "endpoint-version-1",
                    providerCredentialVersionId: "credential-version-1",
                    effectiveModel: "deepseek-chat",
                },
                usage: { inputTokens: 20, outputTokens: 10, totalTokens: 30 },
                startedAt: "2026-09-07T10:00:00Z",
                completedAt: "2026-09-07T10:00:01Z",
            },
        },
        responseDigest: "2".repeat(64),
    };
}

describe("portrait texture image command", () => {
    test("builds a structured command without a browser-generated final prompt", () => {
        const command = buildPortraitTextureImageCommand(" source-1 ", 7);
        expect(command).toEqual({
            schemaVersion: "canvas-image-command/v1",
            command: "portraitTexture",
            objective: "优化人像质感",
            parameters: { preserveIdentity: true, preserveComposition: true, preservePose: true },
            sourceNodeId: "source-1",
            nodeRevision: 7,
        });
        expect(Object.prototype.hasOwnProperty.call(command, "prompt")).toBe(false);
    });

    test("rejects invalid source identity and canvas revision", () => {
        expect(() => buildPortraitTextureImageCommand("", 1)).toThrow("缺少来源节点");
        expect(() => buildPortraitTextureImageCommand("source-1", -1)).toThrow("画布修订无效");
        expect(() => buildPortraitTextureImageCommand("source-1", 1.5)).toThrow("画布修订无效");
    });

    test("requires complete prompt, trace, model, usage and digest evidence", () => {
        expect(assertCanvasImagePromptTaskEvidence(evidence()).response.imagePrompt).toContain("皮肤");
        expect(() => assertCanvasImagePromptTaskEvidence({ ...evidence(), responseDigest: "invalid" })).toThrow("摘要无效");
        expect(() => assertCanvasImagePromptTaskEvidence({ ...evidence(), response: { ...evidence().response, imagePrompt: "" } })).toThrow("缺少有效");
        expect(() => assertCanvasImagePromptTaskEvidence({ ...evidence(), response: { ...evidence().response, trace: { ...evidence().response.trace, usage: { inputTokens: 20, outputTokens: 10, totalTokens: 31 } } } })).toThrow("用量证据无效");
        expect(() => assertCanvasImagePromptTaskEvidence({ schemaVersion: "image-prompt/v1" })).toThrow("缺少人像质感提示词证据");

        const missingEndpointVersion = {
            ...evidence(),
            response: {
                ...evidence().response,
                trace: {
                    ...evidence().response.trace,
                    model: {
                        ...evidence().response.trace.model,
                        providerEndpointVersionId: "",
                    },
                },
            },
        };
        expect(() => assertCanvasImagePromptTaskEvidence(missingEndpointVersion)).toThrow("模型证据无效");
    });
});
