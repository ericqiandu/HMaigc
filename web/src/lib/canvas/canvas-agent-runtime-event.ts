import type { AgentRuntimeEvent } from "@/services/api/agent-runtime";

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

const receiptKeys = ["canvasId", "baseRevision", "committedRevision", "clientMutationId", "proposalHash", "appliedOperationIds", "evidence"] as const;
const evidenceKeys = ["addedNodeIds", "updatedNodeIds", "deletedNodeIds", "upsertedConnectionIds", "deletedConnectionIds", "selectedNodeIds", "viewportApplied"] as const;

export function agentCanvasCommittedReceipt(event: AgentRuntimeEvent, canvasId: string): AgentCanvasApplyOpsReceipt | undefined {
    if (event.kind !== "item.completed" || event.itemKind !== "tool_call" || event.payload.toolName !== "canvas.apply_ops" || event.payload.succeeded !== true) return undefined;
    const output = exactObject(event.payload.output, receiptKeys);
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
