import { generationFailureMetadata } from "@/lib/generation-error";
import { generationTaskMetadata } from "@/lib/canvas/canvas-project-generation";
import type { GenerationTask, TaskStatus } from "@/services/api/task-center";
import type { CanvasNodeData } from "@/types/canvas";

type GenerationTaskCancellationApi = {
    cancel: (taskId: string) => Promise<GenerationTask>;
    query: (taskId: string) => Promise<GenerationTask>;
};

type GenerationTaskRetryApi = {
    retry: (taskId: string) => Promise<GenerationTask>;
};

const terminalTaskStatuses = new Set<TaskStatus>(["succeeded", "failed", "cancelled"]);

export type GenerationStopRequest = {
    targetNodeId: string;
    runningNodeId: string;
    taskId?: string;
    stopping?: boolean;
};

export type GenerationStopPlan = {
    boundTasks: Array<{ targetNodeId: string; taskId: string }>;
    hasLocalRequests: boolean;
};

export type GenerationStopTaskResult = PromiseSettledResult<{ targetNodeId: string; taskId: string; task: GenerationTask }>;

export function generationStopPlan(requests: GenerationStopRequest[], runningNodeId: string): GenerationStopPlan {
    const matching = requests.filter((request) => request.runningNodeId === runningNodeId);
    return {
        boundTasks: matching.flatMap((request) => {
            const taskId = request.taskId?.trim();
            return taskId ? [{ targetNodeId: request.targetNodeId, taskId }] : [];
        }),
        hasLocalRequests: matching.some((request) => !request.taskId?.trim()),
    };
}

export function freezeGenerationRequests<T extends GenerationStopRequest>(requests: Map<string, T>, runningNodeId: string): T[] {
    const frozen: T[] = [];
    requests.forEach((request, key) => {
        if (request.runningNodeId !== runningNodeId) return;
        const stopping = { ...request, stopping: true };
        requests.set(key, stopping);
        frozen.push(stopping);
    });
    return frozen;
}

export function settleGenerationStopTasks(
    bindings: GenerationStopPlan["boundTasks"],
    settle: (taskId: string) => Promise<GenerationTask>,
): Promise<GenerationStopTaskResult[]> {
    return Promise.allSettled(bindings.map(async (binding) => ({ ...binding, task: await settle(binding.taskId) })));
}

export function isTerminalGenerationTask(task: Pick<GenerationTask, "status">) {
    return terminalTaskStatuses.has(task.status);
}

export function hasUsableGenerationTaskResult(task: Pick<GenerationTask, "resultJson">) {
    return Boolean(task.resultJson?.trim());
}

export function mergeGenerationTaskSnapshot(node: CanvasNodeData, task: GenerationTask): CanvasNodeData {
    const currentTaskId = node.metadata?.taskId;
    const currentTaskStatus = node.metadata?.taskStatus;
    const sameTask = currentTaskId === task.id;
    const currentIsTerminal = Boolean(currentTaskStatus && terminalTaskStatuses.has(currentTaskStatus as TaskStatus));
    const incomingIsTerminal = isTerminalGenerationTask(task);
    if (sameTask && currentIsTerminal && !incomingIsTerminal && !isNewerTaskSnapshot(task.updatedAt || task.updated_at, node.metadata?.taskUpdatedAt)) return node;
    if (sameTask && currentIsTerminal === incomingIsTerminal && isOlderTaskSnapshot(task.updatedAt || task.updated_at, node.metadata?.taskUpdatedAt)) return node;

    const cancelledWithResult = task.status === "cancelled" && hasUsableGenerationTaskResult(task);
    const failed = task.status === "failed" || (task.status === "cancelled" && !cancelledWithResult);
    const hasCompletedContent = (task.status === "succeeded" || cancelledWithResult) && Boolean(node.metadata?.content);
    const failure = failed ? generationFailureMetadata(task.error || (task.status === "cancelled" ? "任务已取消" : "任务失败"), node.metadata?.composerContent || node.metadata?.prompt || task.prompt || "") : undefined;
    return {
        ...node,
        metadata: {
            ...node.metadata,
            ...generationTaskMetadata(task),
            status: failed ? "error" : hasCompletedContent ? "success" : "loading",
            ...(failure || { errorDetails: undefined, generationErrorCode: undefined, failedPromptFingerprint: undefined }),
        },
    };
}

export async function convergeGenerationTaskCancellation(taskId: string, api: GenerationTaskCancellationApi): Promise<GenerationTask> {
    try {
        return await api.cancel(taskId);
    } catch (cancelError) {
        let latest: GenerationTask;
        try {
            latest = await api.query(taskId);
        } catch {
            throw cancelError;
        }
        if (isTerminalGenerationTask(latest)) return latest;
        throw cancelError;
    }
}

export function retryBoundGenerationTask(node: CanvasNodeData, api: GenerationTaskRetryApi): Promise<GenerationTask> {
    const taskId = node.metadata?.taskId?.trim();
    if (!taskId) throw new Error("失败节点没有可续查的后端任务");
    return api.retry(taskId);
}

function isOlderTaskSnapshot(incomingUpdatedAt: string | undefined, currentUpdatedAt: string | undefined) {
    if (!incomingUpdatedAt || !currentUpdatedAt) return false;
    const incoming = Date.parse(incomingUpdatedAt);
    const current = Date.parse(currentUpdatedAt);
    return Number.isFinite(incoming) && Number.isFinite(current) && incoming < current;
}

function isNewerTaskSnapshot(incomingUpdatedAt: string | undefined, currentUpdatedAt: string | undefined) {
    if (!incomingUpdatedAt || !currentUpdatedAt) return false;
    const incoming = Date.parse(incomingUpdatedAt);
    const current = Date.parse(currentUpdatedAt);
    return Number.isFinite(incoming) && Number.isFinite(current) && incoming > current;
}
