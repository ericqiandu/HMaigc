import { useMemo } from "react";
import { Check, CircleAlert } from "lucide-react";
import { nanoid } from "nanoid";
import {
    parseAgentProductionTimelineContent,
    type AgentProductionClient,
    type AgentProductionTimelineContent,
    type AgentStageReviewResolutionContent,
} from "@/services/api/agent-production";
import { agentProductionClient } from "@/services/api/agent-production-client";
import type { AgentRuntimeEvent, AgentThreadHistoryTurn } from "@/services/api/agent-runtime";
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
        turn.items.filter((item) => (item.kind === "artifact" || item.kind === "approval") && hasContentType(item.content)).forEach((item) => {
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
        (event.payload.item.kind === "artifact" || event.payload.item.kind === "approval") && hasContentType(event.payload.item.content)) {
        return { itemId: event.itemId, payload: event.payload.item.content };
    }
    if ((event.kind === "item.started" || event.kind === "item.delta" || event.kind === "item.completed" || event.kind === "item.failed" || event.kind === "approval.requested" || event.kind === "approval.resolved") &&
        (event.itemKind === "artifact" || event.itemKind === "approval") && hasContentType(event.payload)) {
        return { itemId: event.itemId, payload: event.payload };
    }
    return null;
}

function hasContentType(value: Record<string, unknown>): boolean {
    return typeof value.contentType === "string";
}

function timelineErrorMessage(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
