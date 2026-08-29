import { useCallback, useEffect, useRef, useState } from "react";
import { Check, LoaderCircle, RefreshCw, Square, Undo2 } from "lucide-react";
import {
    type AgentArtifactReviewContent,
    type AgentArtifactRevision,
    type AgentAssetCategory,
    type AgentMediaCandidatePayload,
    type AgentProductionClient,
    type AgentPublicationIntent,
    type AgentStageReviewDecision,
    type AgentStageReviewResolutionContent,
    type AgentVisualConsistencyReviewPayload,
} from "@/services/api/agent-production";
import { AgentProductionArtifactDetails, AgentProductionCandidatePreview } from "./agent-production-artifact-details";

const assetCategories: Array<{ value: AgentAssetCategory; label: string }> = [
    { value: "character", label: "角色" },
    { value: "environment", label: "环境" },
    { value: "wardrobe", label: "服装" },
    { value: "prop", label: "道具" },
    { value: "weapon", label: "武器" },
    { value: "style", label: "风格" },
    { value: "other", label: "其他" },
];

export function AgentProductionReviewCard({
    runId,
    content,
    resolution,
    client,
    onRefresh,
    createClientRequestId,
}: {
    runId: string;
    content: AgentArtifactReviewContent;
    resolution?: AgentStageReviewResolutionContent;
    client: AgentProductionClient;
    onRefresh: () => Promise<void>;
    createClientRequestId: () => string;
}) {
    const [revision, setRevision] = useState<AgentArtifactRevision | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [actionError, setActionError] = useState("");
    const [comment, setComment] = useState("");
    const [busyDecision, setBusyDecision] = useState<AgentStageReviewDecision | "">("");
    const [localDecision, setLocalDecision] = useState<AgentStageReviewDecision | undefined>();
    const requestIdentityRef = useRef<{ fingerprint: string; id: string } | null>(null);
    const resolvedDecision = resolution?.decision ?? localDecision;

    const loadRevision = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const next = await client.getArtifactRevision(runId, content.artifactId, content.revisionId);
            if (next.artifactId !== content.artifactId || next.revisionId !== content.revisionId || `${next.kind}.v${next.schemaVersion}` !== content.artifactSchema) {
                throw new Error("阶段审核产物身份与精确版本响应不一致");
            }
            setRevision(next);
        } catch (cause) {
            setRevision(null);
            setLoadError(reviewErrorMessage(cause, "阶段产物读取失败"));
        } finally {
            setLoading(false);
        }
    }, [client, content.artifactId, content.artifactSchema, content.revisionId, runId]);

    useEffect(() => {
        void loadRevision();
    }, [loadRevision]);

    const submitReview = useCallback(
        async (decision: AgentStageReviewDecision, selectedCandidateRevisionId?: string, publicationIntent?: AgentPublicationIntent) => {
            if (!revision || resolvedDecision || busyDecision) return;
            const normalizedComment = comment.trim();
            if (decision === "revision_requested" && !normalizedComment) {
                setActionError("请填写需要修改的具体内容");
                return;
            }
            if (selectedCandidateRevisionId && normalizedComment) {
                setActionError("确认候选前请清空修改要求；需要调整时请使用“要求修改”");
                return;
            }
            const fingerprint = JSON.stringify({ decision, selectedCandidateRevisionId, publicationIntent, comment: normalizedComment });
            const clientRequestId = requestIdentityRef.current?.fingerprint === fingerprint ? requestIdentityRef.current.id : createClientRequestId();
            requestIdentityRef.current = { fingerprint, id: clientRequestId };
            setBusyDecision(decision);
            setActionError("");
            try {
                const result = await client.reviewStage(runId, content.stageId, {
                    stageVersion: content.stageVersion,
                    revisionId: content.revisionId,
                    decision,
                    ...(selectedCandidateRevisionId ? { selectedCandidateRevisionId } : {}),
                    clientRequestId,
                    comment: normalizedComment,
                    ...(publicationIntent ? { publicationIntent } : {}),
                });
                const expectedStatus = decision === "approved" ? "approved" : decision === "revision_requested" ? "running" : "stopped";
                if (result.stage.id !== content.stageId || result.stage.version !== content.stageVersion + 1 || result.stage.status !== expectedStatus) {
                    throw new Error("阶段审核响应与提交决议不一致");
                }
                const expectedReviewRevisionId = decision === "revision_requested" ? undefined : content.revisionId;
                if (result.stage.reviewRevisionId !== expectedReviewRevisionId) {
                    throw new Error("阶段审核响应的审核版本与提交决议不一致");
                }
                if (Boolean(result.selectedCandidateRevisionId) !== Boolean(selectedCandidateRevisionId) ||
                    (selectedCandidateRevisionId && result.selectedCandidateRevisionId !== selectedCandidateRevisionId)) {
                    throw new Error("阶段审核响应的候选版本与用户选择不一致");
                }
                if (Boolean(result.publication) !== Boolean(publicationIntent) ||
                    (publicationIntent && result.publication?.artifactRevisionId !== selectedCandidateRevisionId)) {
                    throw new Error("阶段审核响应缺少与候选版本一致的资产入库事实");
                }
                requestIdentityRef.current = null;
                setLocalDecision(decision);
                try {
                    await onRefresh();
                } catch (cause) {
                    setActionError(`审核已提交，但运行事实刷新失败：${reviewErrorMessage(cause, "未知错误")}`);
                }
            } catch (cause) {
                setActionError(reviewErrorMessage(cause, "阶段审核提交失败"));
            } finally {
                setBusyDecision("");
            }
        },
        [busyDecision, client, comment, content.revisionId, content.stageId, content.stageVersion, createClientRequestId, onRefresh, resolvedDecision, revision, runId],
    );

    return (
        <article className="agent-production-card" aria-label="阶段产物审核" data-schema={content.artifactSchema}>
            <header className="agent-production-card-header">
                <div className="agent-production-card-heading">
                    <span className="agent-production-card-kicker">阶段产物 · v{content.stageVersion}</span>
                    <strong className="agent-production-card-title">{schemaLabel(content.artifactSchema)}</strong>
                </div>
                <span className="agent-production-review-state" data-decision={resolvedDecision || "pending"}>
                    {resolvedDecision ? agentProductionDecisionLabel(resolvedDecision) : "待确认"}
                </span>
            </header>
            <p className="agent-production-card-summary">{content.summary}</p>
            {loading ? (
                <div className="agent-production-card-loading" role="status">
                    <LoaderCircle className="agent-production-loading-icon" />
                    正在读取精确产物版本
                </div>
            ) : loadError ? (
                <div className="agent-production-card-error" role="alert">
                    <span className="agent-production-card-error-copy">{loadError}</span>
                    <button type="button" className="agent-production-inline-action" onClick={() => void loadRevision()}>
                        <RefreshCw className="agent-production-action-icon" />
                        重试
                    </button>
                </div>
            ) : revision ? (
                content.artifactSchema === "visual_consistency_review.v1" ? (
                    <VisualCandidateReview
                        runId={runId}
                        reviewRevision={revision}
                        disabled={Boolean(resolvedDecision || busyDecision)}
                        onApprove={(candidateRevisionId, publicationIntent) => submitReview("approved", candidateRevisionId, publicationIntent)}
                        client={client}
                    />
                ) : (
                    <AgentProductionArtifactDetails schema={content.artifactSchema} revision={revision} />
                )
            ) : null}
            {!resolvedDecision ? (
                <div className="agent-production-review-controls">
                    <textarea
                        className="agent-production-review-comment"
                        aria-label="修改要求"
                        value={comment}
                        onChange={(event) => setComment(event.target.value)}
                        placeholder="如需修改，请说明具体调整内容"
                        disabled={Boolean(busyDecision)}
                    />
                    {actionError ? <p className="agent-production-action-error" role="alert">{actionError}</p> : null}
                    <div className="agent-production-review-actions">
                        {content.artifactSchema !== "visual_consistency_review.v1" ? (
                            <button type="button" className="agent-production-primary-action" disabled={!revision || Boolean(busyDecision)} onClick={() => void submitReview("approved")}>
                                <Check className="agent-production-action-icon" />
                                {busyDecision === "approved" ? "提交中" : "确认并继续"}
                            </button>
                        ) : null}
                        <button type="button" className="agent-production-secondary-action" disabled={!revision || Boolean(busyDecision)} onClick={() => void submitReview("revision_requested")}>
                            <Undo2 className="agent-production-action-icon" />
                            {busyDecision === "revision_requested" ? "提交中" : "要求修改"}
                        </button>
                        <button type="button" className="agent-production-stop-action" disabled={!revision || Boolean(busyDecision)} onClick={() => void submitReview("stopped")}>
                            <Square className="agent-production-action-icon" />
                            {busyDecision === "stopped" ? "停止中" : "停止"}
                        </button>
                    </div>
                </div>
            ) : (
                <div className="agent-production-resolved-copy">该阶段已按持久审核事实锁定，刷新后不会重复提交。</div>
            )}
        </article>
    );
}

function VisualCandidateReview({
    runId,
    reviewRevision,
    disabled,
    client,
    onApprove,
}: {
    runId: string;
    reviewRevision: AgentArtifactRevision;
    disabled: boolean;
    client: AgentProductionClient;
    onApprove: (candidateRevisionId: string, publicationIntent?: AgentPublicationIntent) => Promise<void>;
}) {
    const review = reviewRevision.payload as AgentVisualConsistencyReviewPayload;
    const [candidates, setCandidates] = useState<AgentArtifactRevision[]>([]);
    const [selectedRevisionId, setSelectedRevisionId] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [publicationPurpose, setPublicationPurpose] = useState("");
    const [targetCategory, setTargetCategory] = useState<AgentAssetCategory | "">("");
    const [targetBindingKey, setTargetBindingKey] = useState("");

    const loadCandidates = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const byRevisionId = new Map(review.candidateRevisions.map((reference) => [reference.revisionId, reference]));
            if (byRevisionId.size !== review.candidateRevisions.length || review.rankedCandidateRevisionIds.length !== review.candidateRevisions.length) {
                throw new Error("视觉审核候选排名与精确候选版本不一致");
            }
            const orderedReferences = review.rankedCandidateRevisionIds.map((revisionId) => {
                const reference = byRevisionId.get(revisionId);
                if (!reference) throw new Error("视觉审核排名引用了未声明的候选版本");
                return reference;
            });
            const loaded = await Promise.all(orderedReferences.map((reference) => client.getArtifactRevision(runId, reference.artifactId, reference.revisionId)));
            loaded.forEach((candidate, index) => {
                const expected = orderedReferences[index];
                const payload = candidate.payload as AgentMediaCandidatePayload;
                if (!expected || candidate.artifactId !== expected.artifactId || candidate.revisionId !== expected.revisionId || `${candidate.kind}.v${candidate.schemaVersion}` !== "media_candidate.v1") {
                    throw new Error("候选媒体身份与视觉审核引用不一致");
                }
                if (!candidate.resourceId || candidate.resourceId !== payload.resourceId) throw new Error("候选媒体 Resource 身份不一致");
            });
            setCandidates(loaded);
        } catch (cause) {
            setCandidates([]);
            setError(reviewErrorMessage(cause, "候选媒体读取失败"));
        } finally {
            setLoading(false);
        }
    }, [client, review.candidateRevisions, review.rankedCandidateRevisionIds, runId]);

    useEffect(() => {
        void loadCandidates();
    }, [loadCandidates]);

    const selected = candidates.find((candidate) => candidate.revisionId === selectedRevisionId);
    const selectedPayload = selected?.payload as AgentMediaCandidatePayload | undefined;
    const requiresPublication = selectedPayload?.mediaKind === "image" || selectedPayload?.mediaKind === "audio";
    const approve = async () => {
        if (!selected || !selectedPayload) {
            setError("请先选择一个精确候选版本");
            return;
        }
        let publicationIntent: AgentPublicationIntent | undefined;
        if (requiresPublication) {
            if (!publicationPurpose.trim() || !targetCategory || !targetBindingKey.trim()) {
                setError("图片或独立音频候选必须填写完整的入库用途、分类和绑定键");
                return;
            }
            publicationIntent = {
                publicationPurpose: publicationPurpose.trim(),
                targetCategory,
                targetBindingKey: targetBindingKey.trim(),
            };
        }
        setError("");
        await onApprove(selected.revisionId, publicationIntent);
    };

    if (loading) {
        return (
            <div className="agent-production-card-loading" role="status">
                <LoaderCircle className="agent-production-loading-icon" />
                正在读取候选版本
            </div>
        );
    }
    if (error && !candidates.length) {
        return (
            <div className="agent-production-card-error" role="alert">
                <span className="agent-production-card-error-copy">{error}</span>
                <button type="button" className="agent-production-inline-action" onClick={() => void loadCandidates()}>
                    <RefreshCw className="agent-production-action-icon" />
                    重试
                </button>
            </div>
        );
    }
    return (
        <div className="agent-production-candidate-review">
            <div className="agent-production-candidate-grid">
                {candidates.map((candidate, index) => {
                    const payload = candidate.payload as AgentMediaCandidatePayload;
                    const selectedCandidate = candidate.revisionId === selectedRevisionId;
                    return (
                        <div key={candidate.revisionId} className="agent-production-candidate" data-selected={selectedCandidate}>
                            <AgentProductionCandidatePreview resourceId={payload.resourceId} mediaKind={payload.mediaKind} rank={index + 1} />
                            <button
                                type="button"
                                className="agent-production-candidate-select"
                                aria-label={`选择候选 ${index + 1}`}
                                aria-pressed={selectedCandidate}
                                disabled={disabled}
                                onClick={() => {
                                    setSelectedRevisionId(candidate.revisionId);
                                    setError("");
                                }}
                            >
                                {selectedCandidate ? "已选择" : `选择候选 ${index + 1}`}
                            </button>
                        </div>
                    );
                })}
            </div>
            {requiresPublication ? (
                <div className="agent-production-publication-form" aria-label="候选资产入库目标">
                    <input className="agent-production-publication-input" aria-label="入库用途" value={publicationPurpose} onChange={(event) => setPublicationPurpose(event.target.value)} placeholder="入库用途" disabled={disabled} />
                    <select className="agent-production-publication-select" aria-label="资产分类" value={targetCategory} onChange={(event) => setTargetCategory(event.target.value as AgentAssetCategory | "")} disabled={disabled}>
                        <option className="agent-production-publication-option" value="">选择资产分类</option>
                        {assetCategories.map((category) => <option key={category.value} className="agent-production-publication-option" value={category.value}>{category.label}</option>)}
                    </select>
                    <input className="agent-production-publication-input" aria-label="资产绑定键" value={targetBindingKey} onChange={(event) => setTargetBindingKey(event.target.value)} placeholder="资产绑定键" disabled={disabled} />
                </div>
            ) : null}
            {error ? <p className="agent-production-action-error" role="alert">{error}</p> : null}
            <button type="button" className="agent-production-primary-action" disabled={disabled || !candidates.length} onClick={() => void approve()}>
                <Check className="agent-production-action-icon" />
                确认并继续
            </button>
        </div>
    );
}

export function agentProductionDecisionLabel(decision: AgentStageReviewDecision): string {
    if (decision === "approved") return "已确认";
    if (decision === "revision_requested") return "已要求修改";
    return "已停止";
}

export function agentProductionCategoryLabel(category: AgentAssetCategory): string {
    return assetCategories.find((candidate) => candidate.value === category)?.label ?? category;
}

function schemaLabel(schema: AgentArtifactReviewContent["artifactSchema"]): string {
    const labels: Record<AgentArtifactReviewContent["artifactSchema"], string> = {
        "script_bundle.v1": "剧本",
        "asset_binding.v1": "素材绑定",
        "visual_evidence.v1": "图片理解证据",
        "character_visual_bible.v1": "角色视觉圣经",
        "storyboard_plan.v1": "分镜计划",
        "camera_tree.v1": "镜头树",
        "first_motion_last_frame.v1": "首帧·运动·尾帧",
        "media_candidate.v1": "媒体候选",
        "visual_consistency_review.v1": "视觉一致性审核",
        "media_candidate_selection.v1": "候选选择",
        "video_plan.v1": "视频计划",
        "audio_plan.v1": "音频计划",
        "assembly_plan.v1": "装配计划",
        "assembly_plan.v2": "最终装配计划",
    };
    return labels[schema];
}

function reviewErrorMessage(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
