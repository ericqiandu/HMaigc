import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import type { CanvasAgentSnapshot } from "@/lib/canvas/canvas-agent-ops";

const TERMINAL_FAILURE_STATUSES = new Set(["failed", "cancelled", "error"]);
const MEDIA_NODE_TYPES = new Set<CanvasNodeType>([CanvasNodeType.Image, CanvasNodeType.Video, CanvasNodeType.Audio]);

export type CanvasGenerationNodeFact = {
    id: string;
    type: CanvasNodeType;
    status: string;
    taskStatus: string;
    taskProgress: number;
    taskStage: string;
    hasAsset: boolean;
    content: string;
    storageKey: string;
    errorDetails: string;
};

export type CanvasGenerationWaitResult = {
    completed: true;
    nodes: CanvasGenerationNodeFact[];
};

type CanvasGenerationWaitOptions = {
    timeoutMs?: number;
    pollIntervalMs?: number;
};

export function canvasGenerationNodeFacts(snapshot: CanvasAgentSnapshot, nodeIds: string[]): CanvasGenerationNodeFact[] {
    const nodeById = new Map(snapshot.nodes.map((node) => [node.id, node]));
    return nodeIds.map((nodeId) => {
        const node = nodeById.get(nodeId);
        if (!node) throw new Error(`等待生成失败：画布中不存在节点 ${nodeId}`);
        return canvasGenerationNodeFact(node);
    });
}

export async function waitForCanvasGeneration(
    readSnapshot: () => CanvasAgentSnapshot,
    nodeIds: string[],
    options: CanvasGenerationWaitOptions = {},
): Promise<CanvasGenerationWaitResult> {
    if (!nodeIds.length) throw new Error("等待生成失败：至少需要一个节点 ID");
    const timeoutMs = Math.max(1, options.timeoutMs ?? 300_000);
    const pollIntervalMs = Math.max(1, options.pollIntervalMs ?? 750);
    const startedAt = Date.now();

    while (true) {
        const nodes = canvasGenerationNodeFacts(readSnapshot(), nodeIds);
        const failed = nodes.find(isTerminalFailure);
        if (failed) throw new Error(failed.errorDetails || `节点 ${failed.id} 生成失败（${failed.taskStatus || failed.status}）`);
        if (nodes.every(isGenerationComplete)) return { completed: true, nodes };
        if (Date.now() - startedAt >= timeoutMs) {
            const pending = nodes.map((node) => `${node.id}:${node.taskStatus || node.status || "unknown"}:${node.taskProgress}%`).join("，");
            throw new Error(`等待生成超时：${pending}`);
        }
        await delay(pollIntervalMs);
    }
}

function canvasGenerationNodeFact(node: CanvasNodeData): CanvasGenerationNodeFact {
    const metadata = node.metadata || {};
    const content = String(metadata.content || "").trim();
    const storageKey = String(metadata.storageKey || "").trim();
    return {
        id: node.id,
        type: node.type,
        status: String(metadata.status || ""),
        taskStatus: String(metadata.taskStatus || ""),
        taskProgress: Number(metadata.taskProgress) || 0,
        taskStage: String(metadata.taskStage || ""),
        hasAsset: MEDIA_NODE_TYPES.has(node.type) ? Boolean(content || storageKey) : Boolean(content),
        content,
        storageKey,
        errorDetails: String(metadata.errorDetails || ""),
    };
}

function isTerminalFailure(node: CanvasGenerationNodeFact) {
    return TERMINAL_FAILURE_STATUSES.has(node.status.toLowerCase()) || TERMINAL_FAILURE_STATUSES.has(node.taskStatus.toLowerCase());
}

function isGenerationComplete(node: CanvasGenerationNodeFact) {
    if (!node.hasAsset) return false;
    const taskStatus = node.taskStatus.toLowerCase();
    return taskStatus ? taskStatus === "succeeded" : node.status.toLowerCase() === "success";
}

function delay(milliseconds: number) {
    return new Promise<void>((resolve) => setTimeout(resolve, milliseconds));
}
