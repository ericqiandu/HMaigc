import type { CanvasImagePromptCommandV1, CanvasImagePromptTaskEvidenceV1 } from "@/types/canvas";

const sha256Pattern = /^[a-f0-9]{64}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isImagePromptScalar(value: unknown): value is string | number | boolean | null {
    return value === null || typeof value === "string" || typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value));
}

export function buildPortraitTextureImageCommand(sourceNodeId: string, nodeRevision: number): CanvasImagePromptCommandV1 {
    const normalizedSourceNodeId = sourceNodeId.trim();
    if (!normalizedSourceNodeId) throw new Error("人像质感命令缺少来源节点");
    if (!Number.isSafeInteger(nodeRevision) || nodeRevision < 0) throw new Error("人像质感命令的画布修订无效");
    return {
        schemaVersion: "canvas-image-command/v1",
        command: "portraitTexture",
        objective: "优化人像质感",
        parameters: {
            preserveIdentity: true,
            preserveComposition: true,
            preservePose: true,
        },
        sourceNodeId: normalizedSourceNodeId,
        nodeRevision,
    };
}

export function assertCanvasImagePromptTaskEvidence(value: unknown): CanvasImagePromptTaskEvidenceV1 {
    if (!isRecord(value) || value.schemaVersion !== "image-prompt/v1" || !isRecord(value.command) || value.command.schemaVersion !== "canvas-image-command/v1" || value.command.command !== "portraitTexture") {
        throw new Error("后端任务缺少人像质感提示词证据");
    }
    const parameters = value.command.parameters;
    if (
        typeof value.command.objective !== "string" || !value.command.objective.trim() ||
        typeof value.command.sourceNodeId !== "string" || !value.command.sourceNodeId.trim() ||
        !Number.isSafeInteger(value.command.nodeRevision) || Number(value.command.nodeRevision) < 0 ||
        !isRecord(parameters) || !Object.values(parameters).every(isImagePromptScalar)
    ) throw new Error("后端任务的人像质感命令证据无效");
    if (!isRecord(value.response) || value.response.schemaVersion !== "image-prompt/v1" || typeof value.response.imagePrompt !== "string" || !value.response.imagePrompt.trim() || !isRecord(value.response.trace)) {
        throw new Error("后端任务缺少有效的人像质感提示词");
    }
    const trace = value.response.trace;
    if (typeof value.responseDigest !== "string" || typeof trace.evidenceDigest !== "string" || !sha256Pattern.test(value.responseDigest) || !sha256Pattern.test(trace.evidenceDigest)) throw new Error("后端任务的人像质感摘要无效");
    if (trace.specialist !== "image_prompt_specialist" || typeof trace.requestId !== "string" || !trace.requestId.trim() || typeof trace.correlationId !== "string" || !trace.correlationId.trim()) throw new Error("后端任务的人像质感执行身份无效");
    if (!isRecord(trace.model) || typeof trace.model.catalogRecordId !== "string" || !trace.model.catalogRecordId.trim() || typeof trace.model.modelKey !== "string" || !trace.model.modelKey.trim() || typeof trace.model.configurationRevision !== "string" || !trace.model.configurationRevision.trim() || typeof trace.model.providerEndpointVersionId !== "string" || !trace.model.providerEndpointVersionId.trim() || typeof trace.model.providerCredentialVersionId !== "string" || !trace.model.providerCredentialVersionId.trim() || trace.model.effectiveModel !== trace.model.modelKey) throw new Error("后端任务的人像质感模型证据无效");
    if (!isRecord(trace.usage)) throw new Error("后端任务的人像质感用量证据无效");
    const { inputTokens, outputTokens, totalTokens } = trace.usage;
    if (typeof inputTokens !== "number" || typeof outputTokens !== "number" || typeof totalTokens !== "number" || ![inputTokens, outputTokens, totalTokens].every((tokens) => Number.isSafeInteger(tokens) && tokens >= 0) || totalTokens !== inputTokens + outputTokens) throw new Error("后端任务的人像质感用量证据无效");
    if (typeof trace.startedAt !== "string" || typeof trace.completedAt !== "string" || !Number.isFinite(Date.parse(trace.startedAt)) || !Number.isFinite(Date.parse(trace.completedAt)) || Date.parse(trace.completedAt) < Date.parse(trace.startedAt)) throw new Error("后端任务的人像质感时间证据无效");
    return value as CanvasImagePromptTaskEvidenceV1;
}
