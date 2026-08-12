import type { ChannelModel } from "@/services/api/wallet";

export type AgentModelCandidate = ChannelModel & { channelName: string };

export function agentDefaultModelOptions(models: AgentModelCandidate[]) {
    return models
        .filter((model) => model.capability === "text" && model.enabled && model.priceConfigured && model.accessPolicy === "authenticated" && model.billingMode === "fixed_request" && model.priceStrategy === "flat" && model.unitPriceMicrocredits > 0)
        .map((model) => ({ label: `${model.displayName || model.modelKey} · ${model.channelName}`, value: model.id }));
}
