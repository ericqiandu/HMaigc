import { describe, expect, test } from "bun:test";

import { convergeGenerationTaskCancellation, hasUsableGenerationTaskResult, mergeGenerationTaskSnapshot, retryBoundGenerationTask } from "../src/lib/canvas/canvas-generation-task-state";
import type { GenerationTask } from "../src/services/api/task-center";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

describe("canvas generation task state", () => {
    test("keeps a cancelled task terminal when an older running response arrives late", () => {
        const cancelled = mergeGenerationTaskSnapshot(node(), task({ status: "cancelled", stage: "任务已取消", updatedAt: "2026-08-12T11:14:54Z" }));

        const result = mergeGenerationTaskSnapshot(cancelled, task({ status: "running", stage: "调用生成模型", updatedAt: "2026-08-12T11:14:53Z" }));

        expect(result.metadata).toMatchObject({
            status: "error",
            taskStatus: "cancelled",
            taskStage: "任务已取消",
            errorDetails: "任务已取消",
        });
    });

    test("accepts a newer queued generation when the user explicitly retries the same task", () => {
        const cancelled = mergeGenerationTaskSnapshot(node(), task({ status: "cancelled", stage: "任务已取消", updatedAt: "2026-08-12T11:14:54Z" }));

        const result = mergeGenerationTaskSnapshot(cancelled, task({ status: "queued", stage: "等待执行", progress: 0, updatedAt: "2026-08-12T11:15:54Z" }));

        expect(result.metadata).toMatchObject({
            status: "loading",
            taskStatus: "queued",
            taskStage: "等待执行",
            taskProgress: 0,
        });
        expect(result.metadata?.errorDetails).toBeUndefined();
    });

    test("replaces a stale loading presentation with a failed server fact", () => {
        const result = mergeGenerationTaskSnapshot(
            node({ status: "loading", taskStatus: "running", taskStage: "调用生成模型", taskProgress: 15 }),
            task({ status: "failed", stage: "任务失败", progress: 35, error: "参考素材需要公网可访问地址", updatedAt: "2026-08-12T11:07:37Z" }),
        );

        expect(result.metadata).toMatchObject({
            status: "error",
            taskStatus: "failed",
            taskStage: "任务失败",
            taskProgress: 35,
            errorDetails: "参考素材需要公网可访问地址",
        });
    });

    test("treats a cancelled task with a persisted provider result as usable content", () => {
        const resultTask = task({ status: "cancelled", resultJson: `{"video":{"url":"/api/resources/result/file"}}` });
        const result = mergeGenerationTaskSnapshot(node({ content: { videoUrl: "/api/resources/result/file" } }), resultTask);

        expect(hasUsableGenerationTaskResult(resultTask)).toBe(true);
        expect(result.metadata).toMatchObject({ status: "success", taskStatus: "cancelled" });
        expect(result.metadata?.errorDetails).toBeUndefined();
    });

    test("reads and returns a terminal server fact when cancellation reports a conflict", async () => {
        const cancelError = new Error("当前任务已结束，不能取消");
        const result = await convergeGenerationTaskCancellation("task-1", {
            cancel: async () => {
                throw cancelError;
            },
            query: async () => task({ status: "failed", stage: "任务失败", error: "参考素材需要公网可访问地址" }),
        });

        expect(result.status).toBe("failed");
        expect(result.error).toBe("参考素材需要公网可访问地址");
    });

    test("preserves the cancellation error when the server still reports an active task", async () => {
        const cancelError = new Error("取消请求失败");
        const operation = convergeGenerationTaskCancellation("task-1", {
            cancel: async () => {
                throw cancelError;
            },
            query: async () => task({ status: "running", stage: "调用生成模型" }),
        });

        await expect(operation).rejects.toBe(cancelError);
    });

    test("retries the bound backend task instead of creating another provider task", async () => {
        const calls: string[] = [];
        const retried = task({ status: "queued", stage: "等待队列调度", updatedAt: "2026-08-12T11:16:54Z" });

        const result = await retryBoundGenerationTask(node({ taskStatus: "failed" }), {
            retry: async (taskId) => {
                calls.push(taskId);
                return retried;
            },
        });

        expect(calls).toEqual(["task-1"]);
        expect(result).toBe(retried);
    });
});

function node(metadata: CanvasNodeData["metadata"] = {}): CanvasNodeData {
    return {
        id: "video-node-1",
        type: CanvasNodeType.Video,
        title: "视频节点",
        position: { x: 0, y: 0 },
        width: 236,
        height: 236,
        metadata: {
            taskId: "task-1",
            taskUpdatedAt: "2026-08-12T11:14:54Z",
            ...metadata,
        },
    };
}

function task(overrides: Partial<GenerationTask>): GenerationTask {
    return {
        id: "task-1",
        type: "canvas_video",
        status: "running",
        prompt: "测试提示词",
        attempts: 1,
        createdAt: "2026-08-12T11:14:05Z",
        updatedAt: "2026-08-12T11:14:54Z",
        ...overrides,
    };
}
