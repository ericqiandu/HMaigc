import { describe, expect, test } from "bun:test";

import { buildCanvasTaskBillingQuoteRequest } from "../src/hooks/use-canvas-task-billing-quote";
import { defaultConfig, type AiConfig } from "../src/stores/use-config-store";

function imageQuoteConfig(): AiConfig {
    const selectedModel = "image-channel::image-model";
    return {
        ...defaultConfig,
        model: selectedModel,
        imageModel: selectedModel,
        models: [selectedModel],
        imageModels: [selectedModel],
        channels: [
            {
                id: "image-channel",
                name: "Image",
                baseUrl: "/api/ai/system/image-channel",
                apiKey: "system",
                apiFormat: "openai",
                interfaceType: "openai-image",
                models: ["image-model"],
                scope: "system",
                enabled: true,
                modelCosts: [
                    {
                        model: "image-model",
                        marketingCopy: "",
                        promotionBadge: "",
                        estimatedDurationSeconds: 0,
                        brandKey: "openai",
                        accessPolicy: "authenticated",
                        accessible: true,
                        capability: "image",
                        watermarkCapability: "unsupported",
                        billingMode: "fixed_request",
                        priceStrategy: "flat",
                        unitPriceMicrocredits: 100,
                        priceTiers: [],
                    },
                ],
            },
        ],
    };
}

describe("buildCanvasTaskBillingQuoteRequest", () => {
    test("有效系统模型生成原始渠道与模型报价请求", () => {
        const request = buildCanvasTaskBillingQuoteRequest("project-1", imageQuoteConfig(), "image", "image", 1, { referenceImageCount: 0, referenceVideoCount: 0 });

        expect(request?.input.config).toMatchObject({ channelId: "image-channel", model: "image-model" });
    });

    test("新增节点尚无有效模型时不在渲染期抛错", () => {
        const config = { ...defaultConfig, model: "unbound-image-model", imageModel: "unbound-image-model", models: [], imageModels: [], channels: [] };

        expect(() => buildCanvasTaskBillingQuoteRequest("project-1", config, "image", "image", 1, { referenceImageCount: 0, referenceVideoCount: 0 })).not.toThrow();
        expect(buildCanvasTaskBillingQuoteRequest("project-1", config, "image", "image", 1, { referenceImageCount: 0, referenceVideoCount: 0 })).toBeNull();
    });
});
