import { useState } from "react";
import { Button } from "antd";
import { CircleAlert, ShieldCheck } from "lucide-react";

import type { AgentCanvasOperation } from "@/services/api/agent-capabilities";
import type { AgentApprovalSubmission, AgentPendingApproval, AgentToolCall } from "@/services/api/agent-runtime";

import { AgentApprovalSummary } from "./agent-approval-summary";

export type AgentApprovalDecision = AgentApprovalSubmission;

type AgentApprovalCardProps = {
    call: AgentToolCall;
    approval: AgentPendingApproval;
    busy: boolean;
    muted: string;
    now?: Date;
    onDecision: (decision: AgentApprovalDecision) => Promise<void>;
};

type ApprovalValidation = { valid: true } | { valid: false; reason: string };

const approvalEffectByTool = {
    "canvas.apply_ops": "canvas_mutation",
    "assets.publish": "asset_publish",
    "media.generate": "media_generation",
} as const;

export function AgentApprovalCard({ call, approval, busy, muted, now = new Date(), onDecision }: AgentApprovalCardProps) {
    const [submittingProposalHash, setSubmittingProposalHash] = useState("");
    const validation = validateApproval(call, approval, now);
    const submitting = submittingProposalHash === approval.proposalHash;

    const decide = async (decision: AgentApprovalDecision["decision"]) => {
        if (!validation.valid || busy || submitting) return;
        setSubmittingProposalHash(approval.proposalHash);
        try {
            await onDecision({
                toolCallId: approval.toolCallId,
                actionVersion: approval.actionVersion,
                proposalHash: approval.proposalHash,
                decision,
            });
        } finally {
            setSubmittingProposalHash((current) => (current === approval.proposalHash ? "" : current));
        }
    };

    return (
        <section className="canvas-agent-runtime-approval" aria-label="Agent 执行审批">
            <header className="canvas-agent-runtime-approval-heading">
                <ShieldCheck className="canvas-agent-runtime-approval-icon" aria-hidden="true" />
                <div className="canvas-agent-runtime-approval-copy">
                    <strong className="canvas-agent-runtime-approval-title">等待确认</strong>
                    <span className="canvas-agent-runtime-approval-tool" style={{ color: muted }}>
                        {call.toolName} · 版本 {call.actionVersion}
                    </span>
                </div>
            </header>

            {validation.valid ? (
                <>
                    <AgentApprovalSummary approval={approval} />
                    <section className="canvas-agent-runtime-approval-effect" aria-label="审批影响">
                        <strong className="canvas-agent-runtime-approval-effect-summary">{approval.effect.summary}</strong>
                        <span className="canvas-agent-runtime-approval-effect-targets" style={{ color: muted }}>
                            {approval.effect.targetIds.join(" · ")}
                        </span>
                    </section>
                    <AgentApprovalFacts call={call} approval={approval} muted={muted} />
                    <div className="canvas-agent-runtime-approval-actions">
                        <Button className="canvas-agent-runtime-reject" disabled={busy || submitting} onClick={() => void decide("rejected")}>
                            拒绝执行
                        </Button>
                        <Button className="canvas-agent-runtime-approve" type="primary" disabled={busy || submitting} onClick={() => void decide("approved")}>
                            批准执行
                        </Button>
                    </div>
                </>
            ) : (
                <div className="canvas-agent-runtime-approval-invalid" role="alert">
                    <CircleAlert className="canvas-agent-runtime-approval-invalid-icon" aria-hidden="true" />
                    <div className="canvas-agent-runtime-approval-invalid-copy">
                        <strong className="canvas-agent-runtime-approval-invalid-title">{validation.reason}</strong>
                        <span className="canvas-agent-runtime-approval-invalid-guidance" style={{ color: muted }}>
                            当前提案不可执行，需由 Agent 创建新提案。
                        </span>
                    </div>
                </div>
            )}
        </section>
    );
}

function AgentApprovalFacts({ call, approval, muted }: { call: AgentToolCall; approval: AgentPendingApproval; muted: string }) {
    const argumentsValue = call.arguments;
    if (call.toolName === "canvas.apply_ops" && "operations" in argumentsValue) {
        return (
            <section className="canvas-agent-runtime-approval-details" aria-label="冻结画布操作">
                <span className="canvas-agent-runtime-approval-detail-meta" style={{ color: muted }}>
                    画布版本 {argumentsValue.baseRevision} · 变更标识 {argumentsValue.clientMutationId}
                </span>
                <ol className="canvas-agent-runtime-approval-operation-list">
                    {argumentsValue.operations.map((operation) => (
                        <li key={operation.operationId} className="canvas-agent-runtime-approval-operation">
                            {canvasOperationLabel(operation)}
                        </li>
                    ))}
                </ol>
                <ApprovalExpiry expiresAt={approval.expiresAt} muted={muted} />
            </section>
        );
    }
    if (call.toolName === "media.generate" && "mediaKind" in argumentsValue) {
        return (
            <section className="canvas-agent-runtime-approval-details" aria-label="冻结媒体生成参数">
                <dl className="canvas-agent-runtime-approval-parameter-list">
                    {Object.entries(argumentsValue.parameters).map(([key, value]) => (
                        <div key={key} className="canvas-agent-runtime-approval-parameter">
                            <dt className="canvas-agent-runtime-approval-parameter-label" style={{ color: muted }}>
                                {mediaParameterLabel(key)}
                            </dt>
                            <dd className="canvas-agent-runtime-approval-parameter-value">{formatParameterValue(key, value)}</dd>
                        </div>
                    ))}
                </dl>
                <span className="canvas-agent-runtime-approval-detail-meta" style={{ color: muted }}>
                    目标节点 {argumentsValue.targetCanvasNodeId} · 来源资源 {argumentsValue.sourceResourceIds.length}
                </span>
                <ApprovalExpiry expiresAt={approval.expiresAt} muted={muted} />
            </section>
        );
    }
    if (call.toolName === "assets.publish" && "displayName" in argumentsValue) {
        return (
            <section className="canvas-agent-runtime-approval-details" aria-label="冻结资产发布参数">
                <dl className="canvas-agent-runtime-approval-parameter-list">
                    <div className="canvas-agent-runtime-approval-parameter">
                        <dt className="canvas-agent-runtime-approval-parameter-label" style={{ color: muted }}>
                            资产名称
                        </dt>
                        <dd className="canvas-agent-runtime-approval-parameter-value">{argumentsValue.displayName}</dd>
                    </div>
                    <div className="canvas-agent-runtime-approval-parameter">
                        <dt className="canvas-agent-runtime-approval-parameter-label" style={{ color: muted }}>
                            资源
                        </dt>
                        <dd className="canvas-agent-runtime-approval-parameter-value">{argumentsValue.resourceId}</dd>
                    </div>
                </dl>
                <ApprovalExpiry expiresAt={approval.expiresAt} muted={muted} />
            </section>
        );
    }
    return null;
}

function ApprovalExpiry({ expiresAt, muted }: { expiresAt: string; muted: string }) {
    return (
        <span className="canvas-agent-runtime-approval-expiry" style={{ color: muted }}>
            有效至 {formatApprovalExpiry(expiresAt)}
        </span>
    );
}

function validateApproval(call: AgentToolCall, approval: AgentPendingApproval, now: Date): ApprovalValidation {
    if (call.toolCallId !== approval.toolCallId || call.toolName !== approval.toolName || call.actionVersion !== approval.actionVersion) {
        return { valid: false, reason: "审批事实与当前工具调用不一致" };
    }
    if (!validProposalHash(approval.proposalHash)) return { valid: false, reason: "审批提案身份无效" };
    const expiresAt = Date.parse(approval.expiresAt);
    if (!Number.isFinite(expiresAt)) return { valid: false, reason: "审批有效期无效" };
    if (expiresAt <= now.getTime()) return { valid: false, reason: "审批已过期" };
    if (!(call.toolName in approvalEffectByTool)) return { valid: false, reason: "当前工具不允许人工审批" };
    const expectedEffect = approvalEffectByTool[call.toolName as keyof typeof approvalEffectByTool];
    if (approval.effect.kind !== expectedEffect) return { valid: false, reason: "审批影响与工具能力不一致" };
    if (approval.effect.targetIds.length === 0) return { valid: false, reason: "审批影响目标缺失" };
    if (call.toolName === "media.generate") {
        if (!("mediaKind" in call.arguments) || !approval.quote) return { valid: false, reason: "媒体生成费用未冻结" };
        if (approval.quote.modelRecordId !== call.arguments.modelRecordId || approval.quote.modelKey !== call.arguments.modelKey) {
            return { valid: false, reason: "冻结费用与生成模型不一致" };
        }
    } else if (approval.quote) {
        return { valid: false, reason: "非媒体操作不应包含生成费用" };
    }
    return { valid: true };
}

function validProposalHash(value: string) {
    return value.length === 64 && Array.from(value).every((character) => (character >= "0" && character <= "9") || (character >= "a" && character <= "f"));
}

function canvasOperationLabel(operation: AgentCanvasOperation): string {
    switch (operation.type) {
        case "add_node":
            return `新增节点 · ${operation.node.id}`;
        case "update_node":
            return `更新节点 · ${operation.nodeId}`;
        case "delete_node":
            return `删除节点 · ${operation.nodeId}`;
        case "connect_nodes":
            return `连接节点 · ${operation.connection.fromNodeId} → ${operation.connection.toNodeId}`;
        case "delete_connections":
            return `删除连线 · ${operation.connectionIds.join("、")}`;
        case "set_viewport":
            return `设置视口 · ${operation.viewport.zoom}x`;
        case "select_nodes":
            return `选择节点 · ${operation.nodeIds.join("、")}`;
    }
}

function mediaParameterLabel(key: string): string {
    if (key === "durationSeconds") return "时长";
    if (key === "resolution" || key === "quality") return "清晰度";
    if (key === "aspectRatio") return "画幅";
    if (key === "generateAudio") return "声音";
    if (key === "prompt") return "提示词";
    return key;
}

function formatParameterValue(key: string, value: unknown): string {
    if (key === "durationSeconds" && typeof value === "number") return `${value} 秒`;
    if (key === "generateAudio" && typeof value === "boolean") return value ? "生成" : "不生成";
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
    return JSON.stringify(value);
}

function formatApprovalExpiry(value: string): string {
    return new Intl.DateTimeFormat("zh-CN", {
        timeZone: "Asia/Shanghai",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    }).format(new Date(value));
}
