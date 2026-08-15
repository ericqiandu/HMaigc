import type { ChannelModel } from "@/services/api/wallet";

export type AgentModelCandidate = ChannelModel & { channelName: string };

type TokenPricingFacts = {
    inputPerMillionMicros?: number;
    outputPerMillionMicros?: number;
    cachedPerMillionMicros?: number;
    expectedOutputTokens?: number;
};

export function pricingContractForModel(model: Pick<ChannelModel, "billingMode" | "priceStrategy" | "capability" | "modelKey" | "providerCredentialId">, pricing?: TokenPricingFacts) {
    const completeTokenPricing =
        model.capability === "text" &&
        Boolean(model.providerCredentialId) &&
        model.modelKey.startsWith("deepseek-") &&
        (pricing?.inputPerMillionMicros || 0) > 0 &&
        (pricing?.outputPerMillionMicros || 0) > 0 &&
        (pricing?.cachedPerMillionMicros || 0) >= 0 &&
        (pricing?.expectedOutputTokens || 0) > 0;
    return completeTokenPricing ? ({ billingMode: "token_usage", priceStrategy: "token" } as const) : { billingMode: model.billingMode, priceStrategy: model.priceStrategy };
}

export function agentDefaultModelOptions(models: AgentModelCandidate[]) {
    return models
        .filter((model) => {
            if (model.capability !== "text" || !model.enabled || !model.priceConfigured || model.accessPolicy !== "authenticated") return false;
            if (model.billingMode === "token_usage") return model.priceStrategy === "token" && model.unitPriceMicrocredits === 0;
            return model.billingMode === "fixed_request" && model.priceStrategy === "flat" && model.unitPriceMicrocredits > 0;
        })
        .map((model) => ({ label: `${model.displayName || model.modelKey} · ${model.channelName}`, value: model.id }));
}
