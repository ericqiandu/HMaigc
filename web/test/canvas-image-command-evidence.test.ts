import { describe, expect, test } from "bun:test";

import { buildCanvasImageCommandEvidence, buildCanvasImagePromptResultMetadata } from "@/lib/canvas/canvas-image-command-evidence";
import type { CanvasImagePromptTaskEvidenceV1 } from "@/types/canvas";

describe("canvas image command evidence", () => {
    test("records deterministic command source, parameters, resource, dimensions, and time", () => {
        expect(buildCanvasImageCommandEvidence({
            sourceNodeId: "source-1",
            command: "crop",
            parameters: { x: 4, y: 8, width: 320, height: 180 },
            prompt: "",
            resourceId: "resource-1",
            width: 320,
            height: 180,
            createdAt: "2026-09-07T10:00:00.000Z",
        })).toEqual({
            version: 1,
            sourceNodeId: "source-1",
            command: "crop",
            parameters: { x: 4, y: 8, width: 320, height: 180 },
            prompt: "",
            resourceId: "resource-1",
            width: 320,
            height: 180,
            createdAt: "2026-09-07T10:00:00.000Z",
        });
    });

    test("records paid task, order, and mask evidence", () => {
        const evidence = buildCanvasImageCommandEvidence({
            sourceNodeId: "source-1",
            command: "maskEdit",
            parameters: { maskStorageKey: "resources/mask-1/file.png" },
            prompt: "replace the shirt",
            taskId: "task-1",
            billingOrderId: "order-1",
            resourceId: "resource-2",
            width: 1024,
            height: 1024,
            createdAt: "2026-09-07T10:00:00.000Z",
        });
        expect(evidence.taskId).toBe("task-1");
        expect(evidence.billingOrderId).toBe("order-1");
        expect(evidence.parameters).toEqual({ maskStorageKey: "resources/mask-1/file.png" });
    });

    test("fails explicitly when required identity or dimensions are missing", () => {
        expect(() => buildCanvasImageCommandEvidence({ sourceNodeId: "", command: "crop", parameters: {}, prompt: "", resourceId: "resource-1", width: 1, height: 1, createdAt: "2026-09-07T10:00:00.000Z" })).toThrow("图片命令证据缺少来源节点");
        expect(() => buildCanvasImageCommandEvidence({ sourceNodeId: "source-1", command: "crop", parameters: {}, prompt: "", resourceId: "", width: 1, height: 1, createdAt: "2026-09-07T10:00:00.000Z" })).toThrow("图片命令证据缺少资源");
        expect(() => buildCanvasImageCommandEvidence({ sourceNodeId: "source-1", command: "crop", parameters: {}, prompt: "", resourceId: "resource-1", width: 0, height: 1, createdAt: "2026-09-07T10:00:00.000Z" })).toThrow("图片命令证据尺寸无效");
        expect(() => buildCanvasImageCommandEvidence({ sourceNodeId: "source-1", command: "angle", parameters: {}, prompt: "turn", resourceId: "resource-1", width: 1, height: 1, createdAt: "2026-09-07T10:00:00.000Z" })).toThrow("付费图片命令证据缺少任务");
    });

    test("restores the specialist prompt and deterministic evidence after refresh", () => {
        const promptTaskEvidence: CanvasImagePromptTaskEvidenceV1 = {
            schemaVersion: "image-prompt/v1",
            command: {
                schemaVersion: "canvas-image-command/v1",
                command: "portraitTexture",
                objective: "优化人像质感",
                parameters: { preserveIdentity: true },
                sourceNodeId: "source-1",
                nodeRevision: 7,
            },
            response: {
                schemaVersion: "image-prompt/v1",
                imagePrompt: "保留人物身份与构图，改善真实皮肤质感",
                trace: {
                    requestId: "request-1",
                    correlationId: "task-1",
                    specialist: "image_prompt_specialist",
                    evidenceDigest: "1".repeat(64),
                    model: {
                        catalogRecordId: "model-record-1",
                        modelKey: "deepseek-chat",
                        configurationRevision: "2026-09-07T09:59:59.000Z",
                        providerEndpointVersionId: "endpoint-version-1",
                        providerCredentialVersionId: "credential-version-1",
                        effectiveModel: "deepseek-chat",
                    },
                    usage: { inputTokens: 10, outputTokens: 20, totalTokens: 30 },
                    startedAt: "2026-09-07T10:00:00.000Z",
                    completedAt: "2026-09-07T10:00:01.000Z",
                },
            },
            responseDigest: "2".repeat(64),
        };

        expect(buildCanvasImagePromptResultMetadata({
            promptTaskEvidence,
            taskId: "task-1",
            billingOrderId: "order-1",
            resourceId: "resource-2",
            width: 1024,
            height: 1536,
        })).toEqual({
            prompt: "保留人物身份与构图，改善真实皮肤质感",
            imageCommandEvidence: {
                version: 1,
                sourceNodeId: "source-1",
                command: "portraitTexture",
                parameters: { preserveIdentity: true },
                prompt: "保留人物身份与构图，改善真实皮肤质感",
                taskId: "task-1",
                billingOrderId: "order-1",
                specialist: {
                    responseDigest: "2".repeat(64),
                    requestId: "request-1",
                    correlationId: "task-1",
                    evidenceDigest: "1".repeat(64),
                    model: promptTaskEvidence.response.trace.model,
                    usage: { inputTokens: 10, outputTokens: 20, totalTokens: 30 },
                },
                resourceId: "resource-2",
                width: 1024,
                height: 1536,
                createdAt: "2026-09-07T10:00:01.000Z",
            },
        });
    });
});
