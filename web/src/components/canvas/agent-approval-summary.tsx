import { formatCredits } from "@/constant/credits";
import type { AgentToolCall } from "@/services/api/agent-runtime";
import { encodeChannelModel, modelDisplayName, type AiConfig } from "@/stores/use-config-store";

type RenderApprovalFacts = {
    amountMicrocredits?: number;
    model?: { channelId: string; model: string };
    videoDetails?: string;
};

export function AgentApprovalSummary({ call, config }: { call: AgentToolCall; config: AiConfig }) {
    if (call.toolName !== "media.generate") return null;
    const facts = renderApprovalFacts(call.arguments);
    if (facts.amountMicrocredits === undefined && !facts.model && !facts.videoDetails) return null;
    const modelName = facts.model ? modelDisplayName(config, encodeChannelModel(facts.model.channelId, facts.model.model)) : "";
    return (
        <section className="canvas-agent-runtime-approval-summary" aria-label="冻结生成费用">
            {facts.amountMicrocredits !== undefined ? (
                <div className="canvas-agent-runtime-approval-cost">
                    <span className="canvas-agent-runtime-approval-cost-label">预计扣除</span>
                    <strong className="canvas-agent-runtime-approval-cost-value">{formatCredits(facts.amountMicrocredits)} 积分</strong>
                </div>
            ) : null}
            {modelName || facts.videoDetails ? (
                <dl className="canvas-agent-runtime-approval-facts">
                    {modelName ? (
                        <div className="canvas-agent-runtime-approval-fact">
                            <dt className="canvas-agent-runtime-approval-fact-label">模型</dt>
                            <dd className="canvas-agent-runtime-approval-fact-value">{modelName}</dd>
                        </div>
                    ) : null}
                    {facts.videoDetails ? (
                        <div className="canvas-agent-runtime-approval-fact">
                            <dt className="canvas-agent-runtime-approval-fact-label">参数</dt>
                            <dd className="canvas-agent-runtime-approval-fact-value">{facts.videoDetails}</dd>
                        </div>
                    ) : null}
                </dl>
            ) : null}
        </section>
    );
}

function renderApprovalFacts(argumentsValue: Record<string, unknown>): RenderApprovalFacts {
    const generationModel = recordValue(argumentsValue.generationModel);
    const channelId = stringValue(generationModel?.channelId);
    const model = stringValue(generationModel?.model);
    const amountMicrocredits = nonNegativeInteger(argumentsValue.amountMicrocredits);
    const videoConfig = recordValue(argumentsValue.videoConfig);
    const details = [
        videoInputModeLabel(stringValue(argumentsValue.videoInputMode)),
        stringValue(videoConfig?.quality)?.toLocaleUpperCase(),
        stringValue(videoConfig?.aspectRatio),
        durationLabel(positiveInteger(videoConfig?.durationSeconds)),
        quantityLabel(positiveInteger(argumentsValue.quantity)),
        audioLabel(videoConfig?.generateAudio),
    ].filter((value): value is string => Boolean(value));
    return {
        amountMicrocredits,
        model: channelId && model ? { channelId, model } : undefined,
        videoDetails: details.length ? details.join(" · ") : undefined,
    };
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
    return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function stringValue(value: unknown): string | undefined {
    return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function nonNegativeInteger(value: unknown): number | undefined {
    return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

function positiveInteger(value: unknown): number | undefined {
    return typeof value === "number" && Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

function videoInputModeLabel(value?: string): string | undefined {
    if (value === "text_to_video") return "文生视频";
    if (value === "storyboard") return "分镜图生视频";
    return value;
}

function durationLabel(value?: number): string | undefined {
    return value ? `${value}s` : undefined;
}

function quantityLabel(value?: number): string | undefined {
    return value ? `${value} 个` : undefined;
}

function audioLabel(value: unknown): string | undefined {
    if (value === true) return "有声";
    if (value === false) return "无声";
    return undefined;
}
