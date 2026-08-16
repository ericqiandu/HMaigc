import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { App } from "antd";

import { applyGenerationTaskResultToNodes, generationTaskNodeId } from "@/lib/canvas/canvas-generation-task-sync";
import { convergeGenerationTaskCancellation, freezeGenerationRequests, generationStopPlan, hasUsableGenerationTaskResult, isTerminalGenerationTask, mergeGenerationTaskSnapshot, settleGenerationStopTasks, type GenerationStopRequest } from "@/lib/canvas/canvas-generation-task-state";
import { ensureCanvasNodeAsset } from "@/services/project-asset-sync";
import { cancelGenerationTask, listGenerationTasks, listTaskLogs, queryGenerationTask, waitForGenerationTask, type GenerationTask, type TaskLog } from "@/services/api/task-center";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import { cinematicStoryboardColumns, storyboardRowsFromTask } from "@/lib/canvas/canvas-project-domain";
import { generationTaskMetadata } from "@/lib/canvas/canvas-project-generation";
import { generationFailureMetadata } from "@/lib/generation-error";

type CanvasGenerationRequest = GenerationStopRequest & {
    originNodeId: string;
    controller: AbortController;
};

type UseCanvasGenerationOptions = {
    projectId: string;
    domainProjectId?: string;
    projectLoaded: boolean;
    nodes: CanvasNodeData[];
    nodesRef: { current: CanvasNodeData[] };
    setNodes: Dispatch<SetStateAction<CanvasNodeData[]>>;
};

const NODE_STATUS_IDLE = "idle" as const;
const NODE_STATUS_LOADING = "loading" as const;
const NODE_STATUS_SUCCESS = "success" as const;
const NODE_STATUS_ERROR = "error" as const;

export function useCanvasGeneration({ projectId, domainProjectId, projectLoaded, nodes, nodesRef, setNodes }: UseCanvasGenerationOptions) {
    const { message, modal } = App.useApp();
    const queryClient = useQueryClient();
    const generationRequestsRef = useRef(new Map<string, CanvasGenerationRequest>());
    const recoveringTaskIdsRef = useRef(new Set<string>());
    const autoSavedTaskIdsRef = useRef(new Set<string>());
    const [runningNodeId, setRunningNodeId] = useState<string | null>(null);
    const [taskDetail, setTaskDetail] = useState<GenerationTask | null>(null);
    const [taskDetailLogs, setTaskDetailLogs] = useState<TaskLog[]>([]);
    const [taskDetailLoading, setTaskDetailLoading] = useState(false);

    const startGenerationRequest = useCallback((targetNodeId: string, originNodeId: string, runningId = originNodeId, controller = new AbortController()) => {
        const previous = generationRequestsRef.current.get(targetNodeId);
        if (previous?.controller !== controller) previous?.controller.abort();
        generationRequestsRef.current.set(targetNodeId, { targetNodeId, originNodeId, runningNodeId: runningId, controller });
        return controller;
    }, []);

    const finishGenerationRequest = useCallback((targetNodeId: string, controller: AbortController) => {
        const request = generationRequestsRef.current.get(targetNodeId);
        if (request?.controller !== controller) return;
        generationRequestsRef.current.delete(targetNodeId);
        if (request.stopping && !request.taskId) {
            const affectedNodeIds = new Set([request.targetNodeId, request.originNodeId]);
            setNodes((current) => current.map((node) => (affectedNodeIds.has(node.id) && node.metadata?.status === NODE_STATUS_LOADING ? { ...node, metadata: { ...node.metadata, status: NODE_STATUS_IDLE, errorDetails: undefined } } : node)));
        }
        const hasRunningRequest = Array.from(generationRequestsRef.current.values()).some((candidate) => candidate.runningNodeId === request.runningNodeId);
        if (!hasRunningRequest) setRunningNodeId((current) => (current === request.runningNodeId ? null : current));
    }, [setNodes]);

    const freezeGenerationByRunningId = useCallback((runningId: string) => {
        return freezeGenerationRequests(generationRequestsRef.current, runningId);
    }, []);

    const confirmStopGeneration = useCallback(
        (nodeId: string) => {
            modal.confirm({
                title: "停止生成？",
                content: "当前生成请求会被中断，已经生成完成的内容会保留。",
                okText: "停止",
                cancelText: "继续生成",
                okButtonProps: { danger: true },
                onOk: () => {
                    freezeGenerationByRunningId(nodeId);
                },
            });
        },
        [freezeGenerationByRunningId, modal],
    );

    const applyGenerationTaskResult = useCallback(
        async (nodeId: string, task: GenerationTask) => {
            const applied = await applyGenerationTaskResultToNodes(nodesRef.current, task, nodeId);
            if (!applied.updated || !applied.node) throw new Error("画布中找不到对应任务节点");
            setNodes((current) => current.map((node) => (node.id === applied.nodeId ? applied.node! : node)));
        },
        [nodesRef, setNodes],
    );

    const cancelNodeTask = useCallback(
        (node: CanvasNodeData) => {
            const taskId = node.metadata?.taskId;
            if (!taskId) {
                confirmStopGeneration(node.id);
                return;
            }
            modal.confirm({
                title: "取消后台任务？",
                content: "任务会在后端停止，已生成完成的内容仍会保留。",
                okText: "取消任务",
                cancelText: "继续生成",
                okButtonProps: { danger: true },
                onOk: async () => {
                    const task = await convergeGenerationTaskCancellation(taskId, { cancel: cancelGenerationTask, query: queryGenerationTask });
                    if (task.status === "succeeded" || hasUsableGenerationTaskResult(task)) {
                        await applyGenerationTaskResult(node.id, task);
                        message.info(task.status === "cancelled" ? "任务已取消，已生成结果已保留" : "任务已完成，生成结果已保留");
                        return;
                    }
                    setNodes((current) => current.map((item) => (item.id === node.id ? mergeGenerationTaskSnapshot(item, task) : item)));
                    if (task.status === "cancelled") message.success("任务已取消");
                    else if (task.status === "failed") message.info("任务已结束，已同步最新状态");
                    else message.info("取消请求已提交，任务状态正在核对");
                },
            });
        },
        [applyGenerationTaskResult, confirmStopGeneration, message, modal, setNodes],
    );

    const stopNodeGeneration = useCallback(
        (nodeId: string) => {
            const initialPlan = generationStopPlan(Array.from(generationRequestsRef.current.values()), nodeId);
            if (!initialPlan.boundTasks.length) {
                confirmStopGeneration(nodeId);
                return;
            }
            modal.confirm({
                title: initialPlan.boundTasks.length > 1 ? "取消后台生成任务？" : "取消后台任务？",
                content: initialPlan.hasLocalRequests ? "本轮已提交的后台任务将被取消，尚未提交的生成请求也会停止；已生成完成的内容仍会保留。" : "任务会在后端停止，已生成完成的内容仍会保留。",
                okText: initialPlan.boundTasks.length > 1 ? "取消全部任务" : "取消任务",
                cancelText: "继续生成",
                okButtonProps: { danger: true },
                onOk: async () => {
                    const currentPlan = generationStopPlan(Array.from(generationRequestsRef.current.values()), nodeId);
                    const boundTaskById = new Map([...initialPlan.boundTasks, ...currentPlan.boundTasks].map((binding) => [binding.taskId, binding]));
                    const boundTasks = Array.from(boundTaskById.values());
                    freezeGenerationByRunningId(nodeId);
                    const results = await settleGenerationStopTasks(boundTasks, (taskId) =>
                        convergeGenerationTaskCancellation(taskId, { cancel: cancelGenerationTask, query: queryGenerationTask }),
                    );
                    for (const result of results) {
                        if (result.status === "rejected") continue;
                        if (result.value.task.status === "succeeded" || hasUsableGenerationTaskResult(result.value.task)) await applyGenerationTaskResult(result.value.targetNodeId, result.value.task);
                        else setNodes((current) => current.map((item) => (item.id === result.value.targetNodeId ? mergeGenerationTaskSnapshot(item, result.value.task) : item)));
                        if (isTerminalGenerationTask(result.value.task)) {
                            const current = generationRequestsRef.current.get(result.value.targetNodeId);
                            if (current?.runningNodeId === nodeId && current.taskId === result.value.taskId) generationRequestsRef.current.delete(result.value.targetNodeId);
                        }
                    }
                    const fulfilled = results.flatMap((result) => (result.status === "fulfilled" ? [result.value] : []));
                    const rejectedCount = results.length - fulfilled.length;
                    const hasActiveRequest = Array.from(generationRequestsRef.current.values()).some((request) => request.runningNodeId === nodeId);
                    if (!hasActiveRequest) setRunningNodeId((current) => (current === nodeId ? null : current));
                    if (rejectedCount > 0) message.error(`${rejectedCount} 个后台任务取消失败，已保留运行状态，请稍后重试`);
                    else if (fulfilled.some((result) => result.task.status === "succeeded" || hasUsableGenerationTaskResult(result.task))) message.info("任务已停止，已生成结果已保留");
                    else if (fulfilled.length > 0 && fulfilled.every((result) => result.task.status === "cancelled")) message.success(fulfilled.length > 1 ? "后台任务已全部取消" : "任务已取消");
                    else message.info("取消请求已提交，任务状态正在核对");
                },
            });
        },
        [applyGenerationTaskResult, confirmStopGeneration, freezeGenerationByRunningId, message, modal, setNodes],
    );

    const openNodeTaskDetails = useCallback(
        async (node: CanvasNodeData) => {
            const taskId = node.metadata?.taskId;
            if (!taskId) return;
            setTaskDetailLoading(true);
            setTaskDetailLogs([]);
            setTaskDetail({
                id: taskId,
                type: "",
                status: (node.metadata?.taskStatus as GenerationTask["status"]) || "running",
                stage: node.metadata?.taskStage,
                progress: node.metadata?.taskProgress,
                prompt: node.metadata?.prompt || "",
                attempts: 1,
                createdAt: node.metadata?.taskCreatedAt || new Date().toISOString(),
                updatedAt: node.metadata?.taskUpdatedAt || new Date().toISOString(),
            });
            try {
                const [task, logs] = await Promise.all([queryGenerationTask(taskId), listTaskLogs(taskId)]);
                setTaskDetail(task);
                setTaskDetailLogs(logs);
            } catch (error) {
                message.error(error instanceof Error ? error.message : "任务详情加载失败");
            } finally {
                setTaskDetailLoading(false);
            }
        },
        [message],
    );

    const bindGenerationTask = useCallback(
        (targetNodeId: string, task: GenerationTask) => {
            const request = generationRequestsRef.current.get(targetNodeId);
            if (request?.stopping) {
                generationRequestsRef.current.set(targetNodeId, { ...request, taskId: task.id });
                setNodes((current) => current.map((node) => (node.id === targetNodeId ? mergeGenerationTaskSnapshot(node, task) : node)));
                void convergeGenerationTaskCancellation(task.id, { cancel: cancelGenerationTask, query: queryGenerationTask })
                    .then(async (settled) => {
                        if (settled.status === "succeeded" || hasUsableGenerationTaskResult(settled)) await applyGenerationTaskResult(targetNodeId, settled);
                        else setNodes((current) => current.map((node) => (node.id === targetNodeId ? mergeGenerationTaskSnapshot(node, settled) : node)));
                        if (isTerminalGenerationTask(settled)) {
                            const current = generationRequestsRef.current.get(targetNodeId);
                            if (current?.controller === request.controller && current.taskId === task.id) generationRequestsRef.current.delete(targetNodeId);
                            const hasRunningRequest = Array.from(generationRequestsRef.current.values()).some((candidate) => candidate.runningNodeId === request.runningNodeId);
                            if (!hasRunningRequest) setRunningNodeId((currentRunning) => (currentRunning === request.runningNodeId ? null : currentRunning));
                        }
                    })
                    .catch((error: unknown) => message.error(error instanceof Error ? `后台任务取消失败：${error.message}` : "后台任务取消失败"));
                return;
            }
            if (request) generationRequestsRef.current.set(targetNodeId, { ...request, taskId: task.id });
            setNodes((current) => current.map((node) => (node.id === targetNodeId ? mergeGenerationTaskSnapshot(node, task) : node)));
        },
        [applyGenerationTaskResult, message, setNodes],
    );

    const saveGeneratedAsset = useCallback(
        async (node: CanvasNodeData, taskId: string) => {
            const result = await ensureCanvasNodeAsset({ canvasId: projectId, domainProjectId, node, source: "canvas-generation", taskId });
            setNodes((current) => current.map((item) => (item.id === node.id ? { ...item, metadata: { ...item.metadata, assetId: result.assetId } } : item)));
            if (domainProjectId) await queryClient.invalidateQueries({ queryKey: ["project", domainProjectId] });
        },
        [domainProjectId, projectId, queryClient, setNodes],
    );

    const recoverInterruptedGenerationTasks = useCallback(async () => {
        const recoveryNodes = nodesRef.current.filter(
            (node) => node.metadata?.status === NODE_STATUS_LOADING || node.metadata?.errorDetails === "页面刷新后生成已中断，请重新生成。" || Boolean(node.metadata?.taskId && node.metadata.status !== NODE_STATUS_SUCCESS),
        );
        if (!recoveryNodes.length) return;
        const taskIds = Array.from(new Set(recoveryNodes.map((node) => node.metadata?.taskId).filter((id): id is string => Boolean(id))));
        const tasks = (await Promise.all(taskIds.map((id) => queryGenerationTask(id).catch(() => undefined)))).filter((task): task is GenerationTask => Boolean(task));
        if (recoveryNodes.some((node) => !node.metadata?.taskId)) {
            const recentTasks = await listGenerationTasks(30).catch(() => []);
            tasks.push(...recentTasks.filter((task) => !tasks.some((item) => item.id === task.id)));
        }
        const projectTasks = tasks.filter((task) => task.projectId === projectId && (task.type.startsWith("canvas_") || task.type === "agent_storyboard_rows"));
        await Promise.all(
            recoveryNodes.map(async (node) => {
                let task = projectTasks.find((item) => item.id === node.metadata?.taskId) || projectTasks.find((item) => generationTaskNodeId(item) === node.id);
                if (!task && node.metadata?.taskId) task = await queryGenerationTask(node.metadata.taskId).catch(() => undefined);
                if (!task) {
                    setNodes((current) => current.map((item) => (item.id === node.id ? { ...item, metadata: { ...item.metadata, status: NODE_STATUS_ERROR, errorDetails: "页面刷新后找不到对应任务，请重新生成。" } } : item)));
                    return;
                }
                if (recoveringTaskIdsRef.current.has(task.id)) return;
                recoveringTaskIdsRef.current.add(task.id);
                bindGenerationTask(node.id, task);
                try {
                    const completed = task.status === "succeeded" || hasUsableGenerationTaskResult(task) ? task : await waitForGenerationTask(task.id, { initialTask: task });
                    if (node.type === CanvasNodeType.Script && completed.type === "agent_storyboard_rows") {
                        const result = storyboardRowsFromTask(completed);
                        setNodes((current) =>
                            current.map((item) =>
                                item.id === node.id
                                    ? {
                                          ...item,
                                          title: result.title || item.title,
                                          metadata: {
                                              ...item.metadata,
                                              ...generationTaskMetadata(completed),
                                              status: NODE_STATUS_SUCCESS,
                                              errorDetails: undefined,
                                              generationErrorCode: undefined,
                                              failedPromptFingerprint: undefined,
                                              storyboard: { rows: result.rows, visibleColumns: cinematicStoryboardColumns(item.metadata?.storyboard?.visibleColumns), referenceNodeIds: item.metadata?.storyboard?.referenceNodeIds || [] },
                                          },
                                      }
                                    : item,
                            ),
                        );
                    } else {
                        await applyGenerationTaskResult(node.id, completed);
                    }
                } catch (error) {
                    const failure = generationFailureMetadata(error, node.metadata?.composerContent || node.metadata?.prompt || task.prompt || "");
                    setNodes((current) => current.map((item) => (item.id === node.id ? { ...item, metadata: { ...item.metadata, status: NODE_STATUS_ERROR, ...failure } } : item)));
                } finally {
                    recoveringTaskIdsRef.current.delete(task.id);
                }
            }),
        );
    }, [applyGenerationTaskResult, bindGenerationTask, nodesRef, projectId, setNodes]);

    useEffect(() => {
        if (!projectLoaded) return;
        void recoverInterruptedGenerationTasks();
    }, [projectLoaded, recoverInterruptedGenerationTasks]);

    useEffect(() => {
        if (!projectLoaded) return;
        nodes.forEach((node) => {
            const taskId = node.metadata?.taskId;
            if (!taskId || !node.metadata?.content || node.metadata.status !== NODE_STATUS_SUCCESS || (node.type !== CanvasNodeType.Image && node.type !== CanvasNodeType.Video && node.type !== CanvasNodeType.Audio)) return;
            const saveKey = `${taskId}:${node.id}:${domainProjectId || "personal"}`;
            if (autoSavedTaskIdsRef.current.has(saveKey)) return;
            autoSavedTaskIdsRef.current.add(saveKey);
            void saveGeneratedAsset(node, taskId).catch((error) => {
                autoSavedTaskIdsRef.current.delete(saveKey);
                message.warning(error instanceof Error ? `生成结果已保留，但项目资产同步失败：${error.message}` : "生成结果已保留，但项目资产同步失败");
            });
        });
    }, [domainProjectId, message, nodes, projectLoaded, saveGeneratedAsset]);

    return {
        bindGenerationTask,
        cancelNodeTask,
        finishGenerationRequest,
        openNodeTaskDetails,
        runningNodeId,
        setRunningNodeId,
        setTaskDetail,
        startGenerationRequest,
        stopNodeGeneration,
        taskDetail,
        taskDetailLoading,
        taskDetailLogs,
    };
}
