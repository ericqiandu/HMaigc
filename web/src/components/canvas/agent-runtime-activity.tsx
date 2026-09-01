import { CircleAlert, CircleCheck, LoaderCircle } from "lucide-react";

import { isAgentToolName, type AgentToolName } from "@/services/api/agent-capabilities";
import type { AgentRuntimeEvent, AgentThreadHistoryTurn, AgentTimelineItemStatus } from "@/services/api/agent-runtime";

type AgentActivityStatus = AgentTimelineItemStatus | "invalid";

type AgentActivityFact = {
    id: string;
    sequence: number;
    status: AgentActivityStatus;
    toolName?: AgentToolName;
    toolCallId?: string;
    output?: Record<string, unknown>;
    errorCode?: string;
    protocolError?: string;
};

type AgentGeneratedResource = {
    resourceId: string;
    kind: "image" | "video" | "audio";
    url: string;
};

export function AgentRuntimeActivity({ runId, turns, events, muted }: { runId: string; turns: AgentThreadHistoryTurn[]; events: AgentRuntimeEvent[]; muted: string }) {
    const activities = collectAgentActivityFacts(runId, turns, events);
    if (!activities.length) return null;
    return (
        <section className="canvas-agent-runtime-activity" aria-label="Agent 工具活动">
            {activities.map((activity) => (
                <AgentActivityRow key={activity.id} activity={activity} muted={muted} />
            ))}
        </section>
    );
}

export function collectAgentActivityFacts(runId: string, turns: AgentThreadHistoryTurn[], events: AgentRuntimeEvent[]): AgentActivityFact[] {
    const byItemId = new Map<string, AgentActivityFact>();
    const turn = turns.find((candidate) => candidate.run.id === runId);
    turn?.items
        .filter((item) => item.kind === "tool_call")
        .forEach((item) => byItemId.set(item.id, activityFact(item.id, item.sourceEventSequence, item.status, item.content)));

    events.forEach((event) => {
        if (event.runId !== runId || !("itemKind" in event) || event.itemKind !== "tool_call") return;
        if (event.kind !== "item.started" && event.kind !== "item.completed" && event.kind !== "item.failed") return;
        const current = byItemId.get(event.itemId);
        if (current && current.sequence > event.sequence) return;
        const status: AgentTimelineItemStatus = event.kind === "item.started" ? "in_progress" : event.kind === "item.completed" ? "completed" : "failed";
        byItemId.set(event.itemId, activityFact(event.itemId, event.sequence, status, event.payload));
    });

    return Array.from(byItemId.values()).sort((left, right) => left.sequence - right.sequence || left.id.localeCompare(right.id));
}

function activityFact(id: string, sequence: number, status: AgentTimelineItemStatus, content: Record<string, unknown>): AgentActivityFact {
    const toolName = content.toolName;
    const toolCallId = content.toolCallId;
    if (!isAgentToolName(toolName) || typeof toolCallId !== "string" || !toolCallId.trim()) {
        return { id, sequence, status: "invalid", protocolError: "工具执行事实缺少有效的 toolName 或 toolCallId" };
    }
    const output = isRecord(content.output) ? content.output : undefined;
    const errorCode = typeof content.errorCode === "string" && content.errorCode.trim() ? content.errorCode : undefined;
    if (status === "completed" && content.succeeded !== true) {
        return { id, sequence, status: "invalid", toolName, toolCallId, ...(output ? { output } : {}), protocolError: "工具完成事实缺少成功标记" };
    }
    if (status === "failed" && content.succeeded !== false) {
        return { id, sequence, status: "invalid", toolName, toolCallId, ...(output ? { output } : {}), protocolError: "工具失败事实缺少失败标记" };
    }
    if (status === "completed" && toolName === "media.generate" && !hasValidGeneratedResources(output)) {
        return { id, sequence, status: "invalid", toolName, toolCallId, ...(output ? { output } : {}), protocolError: "媒体生成完成事实缺少有效资源" };
    }
    return { id, sequence, status, toolName, toolCallId, ...(output ? { output } : {}), ...(errorCode ? { errorCode } : {}) };
}

function AgentActivityRow({ activity, muted }: { activity: AgentActivityFact; muted: string }) {
    const resources = generatedResources(activity.output);
    return (
        <article className="canvas-agent-runtime-activity-item" data-agent-activity-id={activity.id} data-status={activity.status}>
            <span className="canvas-agent-runtime-activity-icon" aria-hidden="true">
                {activity.status === "in_progress" ? <LoaderCircle className="canvas-agent-runtime-activity-icon-svg animate-spin" /> : activity.status === "completed" ? <CircleCheck className="canvas-agent-runtime-activity-icon-svg" /> : <CircleAlert className="canvas-agent-runtime-activity-icon-svg" />}
            </span>
            <div className="canvas-agent-runtime-activity-copy">
                <div className="canvas-agent-runtime-activity-heading">
                    <strong className="canvas-agent-runtime-activity-title">{activityTitle(activity)}</strong>
                    {activity.toolName ? (
                        <code className="canvas-agent-runtime-activity-tool" style={{ color: muted }}>
                            {activity.toolName}
                        </code>
                    ) : null}
                </div>
                {activity.protocolError ? (
                    <span className="canvas-agent-runtime-activity-error" role="alert">
                        {activity.protocolError}
                    </span>
                ) : null}
                {activity.errorCode ? <code className="canvas-agent-runtime-activity-error-code">{activity.errorCode}</code> : null}
                {typeof activity.output?.taskId === "string" ? <span className="canvas-agent-runtime-activity-task" style={{ color: muted }}>任务 {activity.output.taskId}</span> : null}
                {resources.length ? (
                    <div className="canvas-agent-runtime-activity-resources">
                        {resources.map((resource) => (
                            <AgentGeneratedResourcePreview key={resource.resourceId} resource={resource} />
                        ))}
                    </div>
                ) : null}
            </div>
        </article>
    );
}

function AgentGeneratedResourcePreview({ resource }: { resource: AgentGeneratedResource }) {
    if (resource.kind === "image") {
        return (
            <a className="canvas-agent-runtime-resource-link" href={resource.url} target="_blank" rel="noreferrer" aria-label={`查看图片资源 ${resource.resourceId}`}>
                <img className="canvas-agent-runtime-resource-image" src={resource.url} alt="Agent 生成的图片" loading="lazy" />
            </a>
        );
    }
    if (resource.kind === "video") {
        return <video className="canvas-agent-runtime-resource-video" src={resource.url} controls preload="metadata" aria-label={`视频资源 ${resource.resourceId}`} />;
    }
    return <audio className="canvas-agent-runtime-resource-audio" src={resource.url} controls preload="metadata" aria-label={`音频资源 ${resource.resourceId}`} />;
}

function activityTitle(activity: AgentActivityFact): string {
    if (activity.status === "invalid") return "工具事实不可读取";
    if (activity.status === "in_progress") return "工具正在执行";
    if (activity.status !== "completed") return activity.toolName === "canvas.apply_ops" ? "画布写入失败" : "工具执行失败";
    if (activity.toolName === "canvas.read") return "已读取画布";
    if (activity.toolName === "assets.read") return "已读取资产";
    if (activity.toolName === "skills.load") return "已加载 Skill";
    if (activity.toolName === "canvas.apply_ops") return "画布变更已提交";
    if (activity.toolName === "assets.publish") return "资产已发布";
    return "媒体已生成";
}

function generatedResources(output: Record<string, unknown> | undefined): AgentGeneratedResource[] {
    if (!output || !Array.isArray(output.resources)) return [];
    return output.resources.flatMap((value) => {
        if (!isRecord(value)) return [];
        const { resourceId, kind, url } = value;
        if (typeof resourceId !== "string" || !resourceId.trim() || (kind !== "image" && kind !== "video" && kind !== "audio") || typeof url !== "string" || !url.trim()) return [];
        return [{ resourceId, kind, url }];
    });
}

function hasValidGeneratedResources(output: Record<string, unknown> | undefined): boolean {
    if (!output || !Array.isArray(output.resources) || output.resources.length === 0) return false;
    return generatedResources(output).length === output.resources.length;
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
