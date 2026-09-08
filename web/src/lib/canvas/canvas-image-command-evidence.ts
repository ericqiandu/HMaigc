import type { CanvasImageCommand, CanvasImageCommandEvidence, CanvasImagePromptTaskEvidenceV1 } from "@/types/canvas";

type BuildCanvasImageCommandEvidenceInput = {
    sourceNodeId: string;
    command: CanvasImageCommand;
    parameters: Record<string, unknown>;
    prompt: string;
    taskId?: string;
    billingOrderId?: string;
    promptTaskEvidence?: CanvasImagePromptTaskEvidenceV1;
    resourceId: string;
    width: number;
    height: number;
    createdAt?: string;
};

export function buildCanvasImageCommandEvidence(input: BuildCanvasImageCommandEvidenceInput): CanvasImageCommandEvidence {
    const sourceNodeId = input.sourceNodeId.trim();
    if (!sourceNodeId) throw new Error("图片命令证据缺少来源节点");
    const resourceId = input.resourceId.trim();
    if (!resourceId) throw new Error("图片命令证据缺少资源");
    if (!Number.isFinite(input.width) || !Number.isFinite(input.height) || input.width <= 0 || input.height <= 0) throw new Error("图片命令证据尺寸无效");
    const createdAt = input.createdAt || new Date().toISOString();
    if (!Number.isFinite(Date.parse(createdAt))) throw new Error("图片命令证据时间无效");
    const paidCommand = input.command === "maskEdit" || input.command === "emotion" || input.command === "portraitTexture" || input.command === "angle" || input.command === "superResolve";
    if (paidCommand && !input.taskId?.trim()) throw new Error("付费图片命令证据缺少任务");
    if (paidCommand && !input.billingOrderId?.trim()) throw new Error("付费图片命令证据缺少计费订单");
    if (input.command === "portraitTexture" && !input.promptTaskEvidence) throw new Error("人像质感命令证据缺少 Specialist trace");
    const trace = input.promptTaskEvidence?.response.trace;
    return {
        version: 1,
        sourceNodeId,
        command: input.command,
        parameters: input.parameters,
        prompt: input.prompt,
        ...(input.taskId?.trim() ? { taskId: input.taskId.trim() } : {}),
        ...(input.billingOrderId?.trim() ? { billingOrderId: input.billingOrderId.trim() } : {}),
        ...(trace && input.promptTaskEvidence ? {
            specialist: {
                responseDigest: input.promptTaskEvidence.responseDigest,
                requestId: trace.requestId,
                correlationId: trace.correlationId,
                evidenceDigest: trace.evidenceDigest,
                model: trace.model,
                usage: trace.usage,
            },
        } : {}),
        resourceId,
        width: input.width,
        height: input.height,
        createdAt,
    };
}

export function buildCanvasImagePromptResultMetadata(input: {
    promptTaskEvidence: CanvasImagePromptTaskEvidenceV1;
    taskId: string;
    billingOrderId: string;
    resourceId: string;
    width: number;
    height: number;
}) {
    const { command, response } = input.promptTaskEvidence;
    return {
        prompt: response.imagePrompt,
        imageCommandEvidence: buildCanvasImageCommandEvidence({
            sourceNodeId: command.sourceNodeId,
            command: command.command,
            parameters: command.parameters,
            prompt: response.imagePrompt,
            taskId: input.taskId,
            billingOrderId: input.billingOrderId,
            promptTaskEvidence: input.promptTaskEvidence,
            resourceId: input.resourceId,
            width: input.width,
            height: input.height,
            createdAt: response.trace.completedAt,
        }),
    };
}
