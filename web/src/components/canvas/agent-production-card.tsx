import { useMemo } from "react";
import { Check, CircleAlert, LoaderCircle } from "lucide-react";
import { nanoid } from "nanoid";
import {
    parseAgentProductionTimelineContent,
    type AgentProductionClient,
    type AgentMediaAssemblyContent,
    type AgentProductionTimelineContent,
    type AgentStageReviewResolutionContent,
} from "@/services/api/agent-production";
import { agentProductionClient } from "@/services/api/agent-production-client";
import type { AgentRuntimeEvent, AgentThreadHistoryTurn } from "@/services/api/agent-runtime";
import { resourceFileUrl } from "@/services/api/resources";
import {
    AgentProductionReviewCard,
    agentProductionCategoryLabel,
    agentProductionDecisionLabel,
} from "./agent-production-review-card";

type ProductionTimelineEntry = {
    itemId: string;
    sequence: number;
    content?: AgentProductionTimelineContent;
    error?: string;
};

type AgentProductionTimelineProps = {
    runId: string;
    turns: AgentThreadHistoryTurn[];
    events: AgentRuntimeEvent[];
    onRefresh: () => Promise<void>;
    client?: AgentProductionClient;
    createClientRequestId?: () => string;
};

export function AgentProductionTimeline({
    runId,
    turns,
    events,
    onRefresh,
    client = agentProductionClient,
    createClientRequestId = nanoid,
}: AgentProductionTimelineProps) {
    const entries = useMemo(() => collectProductionTimelineEntries(runId, turns, events), [events, runId, turns]);
    const resolutions = useMemo(
        () => entries.flatMap((entry) => (entry.content?.contentType === "stage_review_resolution" ? [entry.content] : [])),
        [entries],
    );
    const reviewKeys = useMemo(
        () => new Set(entries.flatMap((entry) => (entry.content?.contentType === "artifact_review" ? [`${entry.content.stageId}:${entry.content.revisionId}`] : []))),
        [entries],
    );

    if (!entries.length) return null;
    return (
        <div className="agent-production-timeline" aria-label="Agent 生产时间线">
            {entries.map((entry) => {
                if (entry.error) {
                    return (
                        <div key={entry.itemId} className="agent-production-protocol-error" role="alert">
                            <CircleAlert className="agent-production-status-icon" />
                            <span className="agent-production-status-copy">{entry.error}</span>
                        </div>
                    );
                }
                const content = entry.content;
                if (!content) return null;
                if (content.contentType === "artifact_review") {
                    const resolution = resolutions.find((candidate) => candidate.stageId === content.stageId && candidate.revisionId === content.revisionId);
                    return (
                        <AgentProductionReviewCard
                            key={entry.itemId}
                            runId={runId}
                            content={content}
                            resolution={resolution}
                            client={client}
                            onRefresh={onRefresh}
                            createClientRequestId={createClientRequestId}
                        />
                    );
                }
                if (content.contentType === "stage_review_resolution") {
                    if (reviewKeys.has(`${content.stageId}:${content.revisionId}`)) return null;
                    return <StageResolutionNotice key={entry.itemId} content={content} />;
                }
                if (content.contentType === "asset_publication") {
                    return (
                        <div key={entry.itemId} className="agent-production-status-card" data-status="succeeded">
                            <Check className="agent-production-status-icon" />
                            <div className="agent-production-status-content">
                                <strong className="agent-production-status-title">已加入项目资产库</strong>
                                <span className="agent-production-status-copy">
                                    {agentProductionCategoryLabel(content.targetCategory)} · {content.targetBindingKey} · {content.publicationPurpose}
                                </span>
                            </div>
                        </div>
                    );
                }
                if (content.contentType === "media_assembly") {
                    return <MediaAssemblyCard key={entry.itemId} content={content} />;
                }
                return (
                    <div key={entry.itemId} className="agent-production-status-card" data-status="failed" role="alert">
                        <CircleAlert className="agent-production-status-icon" />
                        <div className="agent-production-status-content">
                            <strong className="agent-production-status-title">资产入库失败</strong>
                            <span className="agent-production-status-copy">{content.errorCode}</span>
                        </div>
                    </div>
                );
            })}
        </div>
    );
}

function MediaAssemblyCard({ content }: { content: AgentMediaAssemblyContent }) {
    const terminalFailure = content.taskStatus === "failed" || content.taskStatus === "cancelled";
    const succeeded = content.taskStatus === "succeeded";
    const StatusIcon = succeeded ? Check : terminalFailure ? CircleAlert : LoaderCircle;
    const statusLabel = succeeded ? "最终视频已装配" : content.taskStatus === "cancelled" ? "最终装配已停止" : content.taskStatus === "failed" ? "最终装配失败" : "最终视频装配中";
    return (
        <div className="agent-production-card agent-production-assembly-card" aria-label="最终视频装配" data-status={content.taskStatus}>
            <div className="agent-production-card-header">
                <div className="agent-production-card-heading">
                    <span className="agent-production-card-kicker">最终视频装配</span>
                    <strong className="agent-production-card-title">{statusLabel}</strong>
                </div>
                <StatusIcon className={succeeded || terminalFailure ? "agent-production-status-icon" : "agent-production-loading-icon"} />
            </div>
            <div className="agent-production-artifact-body">
                <FactRow label="当前阶段" value={content.stage} />
                <FactRow label="输入" value={`${content.clipCount} 个片段`} />
                <FactRow label="输出" value={`${content.output.width}×${content.output.height} · ${content.output.frameRate}fps · ${content.output.container.toUpperCase()}`} />
                <FactRow label="声音" value={assemblyAudioModeLabel(content.audioMode)} />
                {content.errorCode ? <FactRow label="结果" value={content.errorCode} /> : null}
                {content.taskStatus === "cancelled" && content.final ? <FactRow label="采用状态" value="任务停止后完成，结果未采用" /> : null}
            </div>
            {content.final ? (
                <video
                    className="agent-production-candidate-media agent-production-assembly-video"
                    src={resourceFileUrl(content.final.resourceId)}
                    controls
                    preload="metadata"
                    aria-label="最终装配视频"
                />
            ) : null}
        </div>
    );
}

function FactRow({ label, value }: { label: string; value: string }) {
    return <div className="agent-production-fact-row"><span className="agent-production-fact-label">{label}</span><span className="agent-production-fact-value">{value}</span></div>;
}

function assemblyAudioModeLabel(mode: AgentMediaAssemblyContent["audioMode"]): string {
    if (mode === "native") return "视频原声";
    if (mode === "independent") return "独立音轨";
    return "无音轨";
}

function StageResolutionNotice({ content }: { content: AgentStageReviewResolutionContent }) {
    return (
        <div className="agent-production-status-card" data-status={content.decision}>
            <Check className="agent-production-status-icon" />
            <div className="agent-production-status-content">
                <strong className="agent-production-status-title">阶段{agentProductionDecisionLabel(content.decision)}</strong>
                <span className="agent-production-status-copy">版本 {content.stageVersion} → {content.resultStageVersion}</span>
            </div>
        </div>
    );
}

function collectProductionTimelineEntries(runId: string, turns: AgentThreadHistoryTurn[], events: AgentRuntimeEvent[]): ProductionTimelineEntry[] {
    const byItemId = new Map<string, { itemId: string; sequence: number; payload: Record<string, unknown> }>();
    turns.filter((turn) => turn.run.id === runId).forEach((turn) => {
        turn.items.filter((item) => isProductionTimelineItem(item.kind, item.content)).forEach((item) => {
            byItemId.set(item.id, { itemId: item.id, sequence: item.sourceEventSequence, payload: item.content });
        });
    });
    events.filter((event) => event.runId === runId).forEach((event) => {
        const artifact = artifactPayloadFromEvent(event);
        if (!artifact) return;
        const current = byItemId.get(artifact.itemId);
        if (!current || event.sequence >= current.sequence) byItemId.set(artifact.itemId, { itemId: artifact.itemId, sequence: event.sequence, payload: artifact.payload });
    });
    return [...byItemId.values()]
        .sort((left, right) => left.sequence - right.sequence || left.itemId.localeCompare(right.itemId))
        .map((entry) => {
            try {
                return { itemId: entry.itemId, sequence: entry.sequence, content: parseAgentProductionTimelineContent(entry.payload) };
            } catch (cause) {
                return { itemId: entry.itemId, sequence: entry.sequence, error: timelineErrorMessage(cause, "Agent 生产时间线协议无效") };
            }
        });
}

function artifactPayloadFromEvent(event: AgentRuntimeEvent): { itemId: string; payload: Record<string, unknown> } | null {
    if ((event.kind === "run.completed" || event.kind === "run.failed" || event.kind === "run.interrupted") &&
        isProductionTimelineItem(event.payload.item.kind, event.payload.item.content)) {
        return { itemId: event.itemId, payload: event.payload.item.content };
    }
    if ((event.kind === "item.started" || event.kind === "item.delta" || event.kind === "item.completed" || event.kind === "item.failed" || event.kind === "approval.requested" || event.kind === "approval.resolved") &&
        isProductionTimelineItem(event.itemKind, event.payload)) {
        return { itemId: event.itemId, payload: event.payload };
    }
    return null;
}

function hasContentType(value: Record<string, unknown>): boolean {
    return typeof value.contentType === "string";
}

function isProductionTimelineItem(kind: string, value: Record<string, unknown>): boolean {
    if (!hasContentType(value)) return false;
    return kind === "artifact" || kind === "approval" || (kind === "tool_call" && value.contentType === "media_assembly");
}

function timelineErrorMessage(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
