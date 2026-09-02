import { beforeAll, describe, expect, test } from "bun:test";

import { agentDefaultModelOptions, agentVisionModelOptions, pricingContractForModel, supportsTokenUsageBilling, type AgentModelCandidate } from "../src/pages/admin/model-pricing/agent-model-options";
import { parseAgentVisionModelSetting } from "../src/services/api/auth";

let configStore: typeof import("../src/stores/use-config-store");

beforeAll(async () => {
    configStore = await import("../src/stores/use-config-store");
});

function candidate(overrides: Partial<AgentModelCandidate> = {}): AgentModelCandidate {
    return {
        id: "agent-text",
        channelId: "agent-channel",
        providerCredentialId: "credential",
        modelKey: "gpt-5.5",
        displayName: "GPT 5.5",
        marketingCopy: "",
        promotionBadge: "",
        estimatedDurationSeconds: 0,
        brandKey: "openai",
        accessPolicy: "authenticated",
        capability: "text",
        billingMode: "fixed_request",
        priceStrategy: "flat",
        unitPriceMicrocredits: 100,
        priceConfigured: true,
        enabled: true,
        priceVersion: 1,
        priceTiers: [],
        capabilities: undefined,
        channelName: "筷子科技",
        channelInterfaceType: "chat-completion",
        ...overrides,
    };
}

describe("Agent default model setting", () => {
    test("admin candidates include only enabled priced authenticated flat text models", () => {
        const valid = candidate();
        const options = agentDefaultModelOptions([
            valid,
            candidate({ id: "image", capability: "image" }),
            candidate({ id: "disabled", enabled: false }),
            candidate({ id: "unpriced", priceConfigured: false }),
            candidate({ id: "member", accessPolicy: "member" }),
            candidate({ id: "zero", unitPriceMicrocredits: 0 }),
        ]);
        expect(options).toEqual([{ label: "GPT 5.5 · 筷子科技", value: valid.id }]);
    });

    test("admin candidates include a complete token-usage text model", () => {
        const tokenModel = candidate({
            id: "deepseek-token",
            modelKey: "deepseek-v4-pro",
            displayName: "DeepSeek V4 Pro",
            billingMode: "token_usage",
            priceStrategy: "token",
            unitPriceMicrocredits: 0,
        });
        expect(agentDefaultModelOptions([tokenModel])).toEqual([{ label: "DeepSeek V4 Pro · 筷子科技", value: tokenModel.id }]);
    });

    test("vision candidates require an enabled authenticated token-priced vision model on a supported interface", () => {
        const valid = candidate({
            id: "deepseek-vision",
            modelKey: "deepseek-v4-flash-vision-exp",
            displayName: "DeepSeek Vision",
            capability: "vision",
            billingMode: "token_usage",
            priceStrategy: "token",
            unitPriceMicrocredits: 0,
            providerCapabilities: { resolutions: [], inputVariants: [], supportsTokenUsageBilling: true },
        });
        expect(
            agentVisionModelOptions([
                valid,
                candidate({ ...valid, id: "text", capability: "text" }),
                candidate({ ...valid, id: "disabled", enabled: false }),
                candidate({ ...valid, id: "unpriced", priceConfigured: false }),
                candidate({ ...valid, id: "member", accessPolicy: "member" }),
                candidate({ ...valid, id: "fixed", billingMode: "fixed_request", priceStrategy: "flat", unitPriceMicrocredits: 100 }),
                candidate({ ...valid, id: "unsupported-billing", providerCapabilities: undefined }),
                candidate({ ...valid, id: "unsupported-interface", channelInterfaceType: "openai-image" }),
            ]),
        ).toEqual([{ label: "DeepSeek Vision · 筷子科技", value: valid.id }]);
    });

    test("complete token supplier pricing hard-cuts a text model to token billing", () => {
        expect(
            pricingContractForModel(candidate({ modelKey: "deepseek-v4-pro", providerCapabilities: { resolutions: [], inputVariants: [], supportsTokenUsageBilling: true } }), {
                inputPerMillionMicros: 3_000_000,
                outputPerMillionMicros: 6_000_000,
                cachedPerMillionMicros: 25_000,
                expectedOutputTokens: 8192,
                maxOutputTokens: 16_384,
            }),
        ).toEqual({ billingMode: "token_usage", priceStrategy: "token" });
    });

    test("complete token supplier pricing hard-cuts a vision model to token billing", () => {
        expect(
            pricingContractForModel(candidate({ capability: "vision", providerCapabilities: { resolutions: [], inputVariants: [], supportsTokenUsageBilling: true } }), {
                inputPerMillionMicros: 3_000_000,
                outputPerMillionMicros: 6_000_000,
                cachedPerMillionMicros: 25_000,
                maxOutputTokens: 16_384,
            }),
        ).toEqual({ billingMode: "token_usage", priceStrategy: "token" });
    });

    test("token supplier pricing without an explicit output ceiling stays unpublished", () => {
        expect(
            pricingContractForModel(candidate({ modelKey: "deepseek-v4-pro", providerCapabilities: { resolutions: [], inputVariants: [], supportsTokenUsageBilling: true } }), {
                inputPerMillionMicros: 3_000_000,
                outputPerMillionMicros: 6_000_000,
                cachedPerMillionMicros: 25_000,
                expectedOutputTokens: 8192,
            }),
        ).toEqual({ billingMode: "fixed_request", priceStrategy: "flat" });
    });

    test("an unmanaged text channel keeps its published billing contract", () => {
        expect(
            pricingContractForModel(candidate({ providerCredentialId: undefined, modelKey: "deepseek-v4-pro" }), {
                inputPerMillionMicros: 3_000_000,
                outputPerMillionMicros: 6_000_000,
                expectedOutputTokens: 8192,
            }),
        ).toEqual({ billingMode: "fixed_request", priceStrategy: "flat" });
    });

    test("uses the backend capability fact instead of inferring token billing from model text", () => {
        expect(supportsTokenUsageBilling(candidate({ providerCapabilities: { resolutions: [], inputVariants: [], supportsTokenUsageBilling: true } }))).toBe(true);
        expect(supportsTokenUsageBilling(candidate({ modelKey: "deepseek-v4-pro", providerCapabilities: undefined }))).toBe(false);
    });

    test("strictly decodes the admin vision setting instead of synthesizing an empty value", () => {
        expect(
            parseAgentVisionModelSetting({
                configured: true,
                channelModelId: "deepseek-vision-record",
                channelId: "deepseek-channel",
                modelKey: "deepseek-v4-flash-vision-exp",
                displayName: "DeepSeek Vision",
            }),
        ).toEqual({
            configured: true,
            channelModelId: "deepseek-vision-record",
            channelId: "deepseek-channel",
            modelKey: "deepseek-v4-flash-vision-exp",
            displayName: "DeepSeek Vision",
        });
        expect(() => parseAgentVisionModelSetting({ configured: true, channelModelId: "", channelId: "deepseek-channel", modelKey: "deepseek-v4-flash-vision-exp", displayName: "DeepSeek Vision" })).toThrow("缺少已配置模型身份");
        expect(() => parseAgentVisionModelSetting({ configured: false })).toThrow("返回格式错误");
    });

    test("session merge accepts only an exact model reference present in the server catalog", () => {
        const channel: configStore.ModelChannel = {
            id: "agent-channel",
            name: "筷子科技",
            baseUrl: "/api/ai/system/agent-channel",
            apiKey: "system",
            apiFormat: "openai",
            interfaceType: "chat-completion",
            models: ["gpt-5.5"],
            scope: "system",
            enabled: true,
            modelCosts: [
                {
                    model: "gpt-5.5",
                    displayName: "GPT 5.5",
                    marketingCopy: "",
                    promotionBadge: "",
                    estimatedDurationSeconds: 0,
                    brandKey: "openai",
                    accessPolicy: "authenticated",
                    accessible: true,
                    capability: "text",
                    billingMode: "fixed_request",
                    priceStrategy: "flat",
                    unitPriceMicrocredits: 100,
                    priceTiers: [],
                },
            ],
        };
        configStore.useConfigStore.setState({ config: configStore.defaultConfig, agentDefaultModel: "" });
        configStore.useConfigStore.getState().mergeSystemChannels([channel], { channelId: channel.id, modelKey: "gpt-5.5" });
        expect(configStore.useConfigStore.getState().agentDefaultModel).toBe(configStore.encodeChannelModel(channel.id, "gpt-5.5"));

        configStore.useConfigStore.getState().mergeSystemChannels([channel], { channelId: channel.id, modelKey: "missing" });
        expect(configStore.useConfigStore.getState().agentDefaultModel).toBe("");
    });

    test("session merge keeps a configured token-usage Agent model in the catalog", () => {
        const channel: configStore.ModelChannel = {
            id: "kuaizi-deepseek",
            name: "筷子科技",
            baseUrl: "/api/ai/system/kuaizi-deepseek",
            apiKey: "system",
            apiFormat: "openai",
            interfaceType: "chat-completion",
            models: ["deepseek-v4-pro"],
            scope: "system",
            enabled: true,
            modelCosts: [
                {
                    model: "deepseek-v4-pro",
                    displayName: "DeepSeek V4 Pro",
                    marketingCopy: "",
                    promotionBadge: "",
                    estimatedDurationSeconds: 0,
                    brandKey: "deepseek",
                    accessPolicy: "authenticated",
                    accessible: true,
                    capability: "text",
                    billingMode: "token_usage",
                    priceStrategy: "token",
                    unitPriceMicrocredits: 0,
                    priceTiers: [],
                },
            ],
        };

        configStore.useConfigStore.setState({ config: configStore.defaultConfig, agentDefaultModel: "" });
        configStore.useConfigStore.getState().mergeSystemChannels([channel], { channelId: channel.id, modelKey: "deepseek-v4-pro" });

        expect(configStore.useConfigStore.getState().agentDefaultModel).toBe(configStore.encodeChannelModel(channel.id, "deepseek-v4-pro"));
    });
});
