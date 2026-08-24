import { useState } from "react";
import { Check, ChevronDown, CircleAlert, Sparkles } from "lucide-react";

import type { AgentRuntimeEvent, AgentRuntimeState } from "@/services/api/agent-runtime";

type AgentThinkingTraceProps = {
    status: AgentRuntimeState["status"];
    stepNumber: number;
    maxSteps: number;
    hasVisibleReply: boolean;
    deliveryVerified: boolean;
    reconnecting: boolean;
    events: AgentRuntimeEvent[];
};

type ThinkingTraceRow = {
    id: string;
    label: string;
    secondary?: string;
    state: "active" | "completed" | "failed";
};

export function AgentThinkingTrace({ status, stepNumber, maxSteps, hasVisibleReply, deliveryVerified, reconnecting, events }: AgentThinkingTraceProps) {
    const activity = thinkingActivity(status, hasVisibleReply, reconnecting);
    const [manualExpanded, setManualExpanded] = useState<boolean | null>(null);
    const expanded = manualExpanded ?? activity.active;
    const rows = thinkingTraceRows(events, status, stepNumber, maxSteps, hasVisibleReply, deliveryVerified, reconnecting);

    return (
        <section className="canvas-agent-thinking" aria-label="Agent 思考过程">
            <button type="button" className="canvas-agent-thinking-toggle" aria-expanded={expanded} onClick={() => setManualExpanded((current) => !(current ?? activity.active))}>
                <Sparkles className="canvas-agent-thinking-icon" fill="currentColor" aria-hidden="true" />
                <span className="canvas-agent-thinking-label" data-active={activity.active} role="status" aria-live="polite">
                    {activity.label}
                </span>
                <ChevronDown className="canvas-agent-thinking-chevron" data-expanded={expanded} aria-hidden="true" />
            </button>
            <div className="canvas-agent-thinking-disclosure" data-expanded={expanded} aria-hidden={!expanded}>
                <div className="canvas-agent-thinking-disclosure-inner">
                    <div className="canvas-agent-thinking-trace">
                        <span className="canvas-agent-thinking-line" aria-hidden="true" />
                        <div className="canvas-agent-thinking-rows" role="list" aria-label="Agent 执行记录">
                            {rows.map((row) => (
                                <div key={row.id} className="canvas-agent-thinking-row" role="listitem" data-state={row.state}>
                                    {row.state === "active" ? <span className="canvas-agent-thinking-spinner" aria-hidden="true" /> : null}
                                    {row.state === "completed" ? <Check className="canvas-agent-thinking-check" aria-hidden="true" /> : null}
                                    {row.state === "failed" ? <CircleAlert className="canvas-agent-thinking-error" aria-hidden="true" /> : null}
                                    <span className="canvas-agent-thinking-row-label">{row.label}</span>
                                    {row.secondary ? <span className="canvas-agent-thinking-row-secondary">{row.secondary}</span> : null}
                                </div>
                            ))}
                        </div>
                    </div>
                </div>
            </div>
        </section>
    );
}

function thinkingActivity(status: AgentRuntimeState["status"], hasVisibleReply: boolean, reconnecting: boolean) {
    if (reconnecting) return { active: true, label: "正在恢复连接" } as const;
    if (status === "queued") return { active: true, label: "思考中" } as const;
    if (status === "running" && !hasVisibleReply) return { active: true, label: "思考中" } as const;
    if (status === "waiting_tool") return { active: true, label: "执行工具" } as const;
    if (status === "waiting_input") return { active: false, label: "等待回答" } as const;
    if (status === "waiting_approval") return { active: false, label: "等待确认" } as const;
    if (status === "failed") return { active: false, label: "思考失败" } as const;
    if (status === "cancelled") return { active: false, label: "已停止" } as const;
    return { active: false, label: "已思考" } as const;
}

function thinkingTraceRows(events: AgentRuntimeEvent[], status: AgentRuntimeState["status"], stepNumber: number, maxSteps: number, hasVisibleReply: boolean, deliveryVerified: boolean, reconnecting: boolean): ThinkingTraceRow[] {
    const rows: ThinkingTraceRow[] = events.map((event) => ({
        id: String(event.sequence),
        label: traceEventLabel(event),
        state: event.kind === "run.failed" || event.kind === "run.interrupted" || event.kind === "item.failed" ? ("failed" as const) : ("completed" as const),
    }));
    const step = stepNumber > 0 ? `已执行 ${stepNumber} 步 · 上限 ${maxSteps}` : `上限 ${maxSteps} 步`;
    if (reconnecting) rows.push({ id: "active-reconnecting", label: "恢复实时连接", secondary: step, state: "active" });
    else if (status === "queued") rows.push({ id: "active-queued", label: "等待模型任务", secondary: step, state: "active" });
    else if (status === "running" && !hasVisibleReply) rows.push({ id: "active-thinking", label: "生成回复", secondary: step, state: "active" });
    else if (status === "waiting_tool") rows.push({ id: "active-tool", label: "等待工具结果", secondary: step, state: "active" });
    else if (hasVisibleReply) rows.push({ id: "reply-started", label: "开始生成回复", secondary: step, state: "completed" });
    if (deliveryVerified) rows.push({ id: "delivery-verified", label: "交付已验收", state: "completed" });
    return rows.slice(-4);
}

function traceEventLabel(event: AgentRuntimeEvent) {
    switch (event.kind) {
        case "run.started":
            return "建立运行上下文";
        case "run.completed":
            return "完成交付验收";
        case "run.failed":
            return "运行失败";
        case "run.interrupted":
            return "停止运行";
        case "approval.requested":
            return "请求执行确认";
        case "approval.resolved":
            return "记录执行确认";
        case "item.failed":
            return "执行项失败";
        case "item.completed":
            return event.itemKind === "artifact" ? "保存交付资产" : event.itemKind === "tool_call" ? "完成工具调用" : "完成执行项";
        default:
            return "更新运行状态";
    }
}
