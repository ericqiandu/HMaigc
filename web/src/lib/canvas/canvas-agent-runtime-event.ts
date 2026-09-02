import { parseAgentCapabilityResult, type AgentRuntimeEvent, type AgentVisionAnalyzeResult } from "@/services/api/agent-runtime";

export type AgentCanvasApplyOpsEvidence = {
    addedNodeIds: string[];
    updatedNodeIds: string[];
    deletedNodeIds: string[];
    upsertedConnectionIds: string[];
    deletedConnectionIds: string[];
    selectedNodeIds: string[];
    viewportApplied: boolean;
};

export type AgentCanvasApplyOpsReceipt = {
    canvasId: string;
    baseRevision: number;
    committedRevision: number;
    clientMutationId: string;
    proposalHash: string;
    appliedOperationIds: string[];
    evidence: AgentCanvasApplyOpsEvidence;
};

export type AgentAuthoritativeToolResult = {
    toolName: string;
    succeeded: boolean;
    output: unknown;
};

const receiptKeys = ["canvasId", "baseRevision", "committedRevision", "clientMutationId", "proposalHash", "appliedOperationIds", "evidence"] as const;
const evidenceKeys = ["addedNodeIds", "updatedNodeIds", "deletedNodeIds", "upsertedConnectionIds", "deletedConnectionIds", "selectedNodeIds", "viewportApplied"] as const;

export function agentCanvasCommittedReceipt(event: AgentRuntimeEvent, canvasId: string): AgentCanvasApplyOpsReceipt | undefined {
    if (event.kind !== "item.completed" || event.itemKind !== "tool_call") return undefined;
    if (typeof event.payload.toolName !== "string") return undefined;
    return agentCanvasCommittedReceiptFromToolResult({ toolName: event.payload.toolName, succeeded: event.payload.succeeded === true, output: event.payload.output }, canvasId);
}

export function agentCanvasCommittedReceiptFromToolResult(result: AgentAuthoritativeToolResult, canvasId: string): AgentCanvasApplyOpsReceipt | undefined {
    if (result.toolName !== "canvas.apply_ops" || !result.succeeded) return undefined;
    const output = exactObject(result.output, receiptKeys);
    if (!output || output.canvasId !== canvasId || !validIdentifier(output.canvasId) || !validIdentifier(output.clientMutationId) || !validSHA256(output.proposalHash)) return undefined;
    if (!nonNegativeInteger(output.baseRevision) || !positiveInteger(output.committedRevision) || output.committedRevision !== output.baseRevision + 1) return undefined;
    const appliedOperationIds = identifierList(output.appliedOperationIds, false);
    const evidence = exactObject(output.evidence, evidenceKeys);
    if (!appliedOperationIds || !evidence || typeof evidence.viewportApplied !== "boolean") return undefined;
    const addedNodeIds = identifierList(evidence.addedNodeIds, true);
    const updatedNodeIds = identifierList(evidence.updatedNodeIds, true);
    const deletedNodeIds = identifierList(evidence.deletedNodeIds, true);
    const upsertedConnectionIds = identifierList(evidence.upsertedConnectionIds, true);
    const deletedConnectionIds = identifierList(evidence.deletedConnectionIds, true);
    const selectedNodeIds = identifierList(evidence.selectedNodeIds, true);
    if (!addedNodeIds || !updatedNodeIds || !deletedNodeIds || !upsertedConnectionIds || !deletedConnectionIds || !selectedNodeIds) return undefined;
    return {
        canvasId: output.canvasId,
        baseRevision: output.baseRevision,
        committedRevision: output.committedRevision,
        clientMutationId: output.clientMutationId,
        proposalHash: output.proposalHash,
        appliedOperationIds,
        evidence: { addedNodeIds, updatedNodeIds, deletedNodeIds, upsertedConnectionIds, deletedConnectionIds, selectedNodeIds, viewportApplied: evidence.viewportApplied },
    };
}

export function agentVisionAnalyzeResultFromToolResult(result: AgentAuthoritativeToolResult): AgentVisionAnalyzeResult | undefined {
    if (result.toolName !== "vision.analyze" || !result.succeeded) return undefined;
    try {
        return parseAgentCapabilityResult("vision.analyze", result.output);
    } catch {
        return undefined;
    }
}

export function agentRuntimeUserErrorMessage(errorCode: string): string | undefined {
    if (errorCode === "insufficient_credits") return "余额不足，请充值后重试。";
    if (errorCode === "canvas_revision_conflict") return "画布版本已经更新，请同步最新内容后重试。";
    if (errorCode === "vision_analysis_failed" || errorCode === "vision_analysis_cancelled") return "图片理解未完成，预留积分已退回。";
    if (errorCode === "vision_settlement_uncertain" || errorCode === "vision_settlement_incomplete") return "账务状态需要核对，请勿重复提交。";
    if (errorCode === "vision_facts_changed" || errorCode === "vision_quote_changed" || errorCode === "vision_quote_missing") return "视觉模型或报价已经变化，请重新发起。";
    if (errorCode === "vision_result_invalid" || errorCode === "vision_receipt_invalid") return "图片理解结果校验失败，请联系管理员核对本次任务。";
    if (errorCode === "vision_task_failed" || errorCode === "vision_task_invalid") return "图片理解任务未能启动，请稍后重试。";
    return undefined;
}

function exactObject<const Keys extends readonly string[]>(value: unknown, keys: Keys): Record<Keys[number], unknown> | undefined {
    if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
    const entries = Object.keys(value);
    if (entries.length !== keys.length || entries.some((key) => !keys.includes(key))) return undefined;
    return value as Record<Keys[number], unknown>;
}

function identifierList(value: unknown, allowEmpty: boolean): string[] | undefined {
    if (!Array.isArray(value) || (!allowEmpty && value.length === 0)) return undefined;
    if (!value.every(validIdentifier) || new Set(value).size !== value.length) return undefined;
    return [...value];
}

function validIdentifier(value: unknown): value is string {
    return typeof value === "string" && value.trim() === value && value.length > 0 && value.length <= 160;
}

function validSHA256(value: unknown): value is string {
    return typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
}

function nonNegativeInteger(value: unknown): value is number {
    return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function positiveInteger(value: unknown): value is number {
    return typeof value === "number" && Number.isInteger(value) && value > 0;
}
