import { beforeAll, describe, expect, test } from "bun:test";

import { agentDefaultModelOptions, type AgentModelCandidate } from "../src/pages/admin/model-pricing/agent-model-options";

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
});
