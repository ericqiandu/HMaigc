import type { AgentSessionDetail } from "@/services/api/task-center";
import type { CanvasAgentLaunchRequest, CanvasAssistantSession } from "@/types/canvas";

const PROJECT_TITLE_LIMIT = 24;

export function createCanvasAgentLaunchRequest(input: { prompt: string; id: string; createdAt: string }): CanvasAgentLaunchRequest {
    const { prompt, id, createdAt } = input;
    const normalizedPrompt = prompt.trim();
    if (!normalizedPrompt) throw new Error("创作描述不能为空");
    return {
        id,
        source: "home",
        prompt: normalizedPrompt,
        createdAt,
    };
}

export function canvasAgentProjectTitle(prompt: string) {
    const normalized = prompt.trim().replace(/\s+/g, " ");
    if (!normalized) throw new Error("创作描述不能为空");
    return normalized.length > PROJECT_TITLE_LIMIT ? `${normalized.slice(0, PROJECT_TITLE_LIMIT)}…` : normalized;
}

export function hasCanvasAgentLaunchRecord(sessions: CanvasAssistantSession[], launchRequestId: string) {
    return sessions.some((session) => session.pendingBackendSession?.launchRequestId === launchRequestId || session.messages.some((message) => objectField(message.detail, "launchRequestId") === launchRequestId));
}

export function hasPendingCinematicAgentWork(sessions: CanvasAssistantSession[]) {
    return sessions.some((session) => session.pendingBackendSession?.kind === "cinematic" || session.messages.some((message) => objectField(message.detail, "kind") === "cinematic-proposal" && objectField(message.detail, "status") === "pending"));
}

export type CinematicAgentProgress = {
    progress?: number;
    stage?: string;
    taskCount: number;
    completedTaskCount: number;
    text: string;
};

export function cinematicAgentProgress(detail: AgentSessionDetail): CinematicAgentProgress {
    const tasks = detail.tasks;
    const completedTaskCount = tasks.filter((task) => task.status === "succeeded").length;
    const activeTask = [...tasks].reverse().find((task) => task.status === "running" || task.status === "queued");
    const progress = typeof activeTask?.progress === "number" ? Math.max(0, Math.min(100, Math.round(activeTask.progress))) : undefined;
    const stage = activeTask?.stage?.trim() || undefined;
    const progressText = progress === undefined ? "" : ` ${progress}%`;
    const taskText = tasks.length ? `（已完成 ${completedTaskCount}/${tasks.length} 个任务）` : "";
    const text = stage ? `影视 Agent 正在处理：${stage}${progressText}${taskText}` : `影视 Agent 正在处理${progressText}${taskText}`;
    return { progress, stage, taskCount: tasks.length, completedTaskCount, text };
}

function objectField(value: unknown, key: string) {
    return value && typeof value === "object" && key in value ? (value as Record<string, unknown>)[key] : undefined;
}
