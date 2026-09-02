import type { ChannelModel } from "@/services/api/wallet";
import type { ChannelInterfaceType } from "@/stores/use-config-store";

export type AgentModelCandidate = ChannelModel & { channelName: string; channelInterfaceType?: ChannelInterfaceType };

export function supportsTokenUsageBilling(model: Pick<ChannelModel, "providerCapabilities">) {
    return model.providerCapabilities?.supportsTokenUsageBilling === true;
}

type TokenPricingFacts = {
    inputPerMillionMicros?: number;
    outputPerMillionMicros?: number;
    cachedPerMillionMicros?: number;
    expectedOutputTokens?: number;
    maxOutputTokens?: number;
};

export function pricingContractForModel(model: Pick<ChannelModel, "billingMode" | "priceStrategy" | "capability" | "providerCapabilities">, pricing?: TokenPricingFacts) {
    const completeTokenPricing =
        (model.capability === "text" || model.capability === "vision") &&
        supportsTokenUsageBilling(model) &&
        (pricing?.inputPerMillionMicros || 0) > 0 &&
        (pricing?.outputPerMillionMicros || 0) > 0 &&
        (pricing?.cachedPerMillionMicros || 0) >= 0 &&
        (pricing?.maxOutputTokens || 0) > 0;
    return completeTokenPricing ? ({ billingMode: "token_usage", priceStrategy: "token" } as const) : { billingMode: model.billingMode, priceStrategy: model.priceStrategy };
}

function supportsAgentInterface(interfaceType?: ChannelInterfaceType) {
    return interfaceType === "chat-completion" || interfaceType === "openai-response";
}

export function agentDefaultModelOptions(models: AgentModelCandidate[]) {
    return models
        .filter((model) => {
            if (model.capability !== "text" || !model.enabled || !model.priceConfigured || model.accessPolicy !== "authenticated" || !supportsAgentInterface(model.channelInterfaceType)) return false;
            if (model.billingMode === "token_usage") return model.priceStrategy === "token" && model.unitPriceMicrocredits === 0;
            return model.billingMode === "fixed_request" && model.priceStrategy === "flat" && model.unitPriceMicrocredits > 0;
        })
        .map((model) => ({ label: `${model.displayName || model.modelKey} · ${model.channelName}`, value: model.id }));
}

export function agentVisionModelOptions(models: AgentModelCandidate[]) {
    return models
        .filter(
            (model) =>
                model.capability === "vision" &&
                model.enabled &&
                model.priceConfigured &&
                model.accessPolicy === "authenticated" &&
                model.billingMode === "token_usage" &&
                model.priceStrategy === "token" &&
                model.unitPriceMicrocredits === 0 &&
                supportsTokenUsageBilling(model) &&
                supportsAgentInterface(model.channelInterfaceType),
        )
        .map((model) => ({ label: `${model.displayName || model.modelKey} · ${model.channelName}`, value: model.id }));
}
