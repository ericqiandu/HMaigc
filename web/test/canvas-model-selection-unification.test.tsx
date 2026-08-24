import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { CanvasAgentModelMenu } from "../src/components/canvas/canvas-agent-model-menu";
import { CanvasModelSelectionMenu } from "../src/components/canvas/canvas-model-selection-menu";
import { ModelPicker } from "../src/components/model-picker";
import { defaultConfig, encodeChannelModel, type AiConfig, type ModelChannel } from "../src/stores/use-config-store";

const mediaChannel: ModelChannel = {
    id: "system-media",
    name: "系统媒体模型",
    baseUrl: "/api/ai/system/system-media",
    apiKey: "system",
    apiFormat: "openai",
    interfaceType: "image-generation",
    models: ["gpt-image-2", "member-image", "seedance-2.0", "elevenlabs-voice"],
    scope: "system",
    enabled: true,
    modelCosts: [
        {
            model: "gpt-image-2",
            displayName: "GPT Image 2",
            marketingCopy: "商业级图片生成",
            promotionBadge: "推荐",
            estimatedDurationSeconds: 20,
            brandKey: "openai",
            accessPolicy: "authenticated",
            accessible: true,
            capability: "image",
            billingMode: "fixed_request",
            priceStrategy: "flat",
            unitPriceMicrocredits: 100,
            priceTiers: [],
        },
        {
            model: "member-image",
            displayName: "Member Image",
            marketingCopy: "会员图片模型",
            promotionBadge: "会员",
            estimatedDurationSeconds: 20,
            brandKey: "openai",
            accessPolicy: "member",
            accessible: false,
            capability: "image",
            billingMode: "fixed_request",
            priceStrategy: "flat",
            unitPriceMicrocredits: 100,
            priceTiers: [],
        },
        {
            model: "seedance-2.0",
            displayName: "Seedance 2.0",
            marketingCopy: "高质量视频生成",
            promotionBadge: "",
            estimatedDurationSeconds: 90,
            brandKey: "openai",
            accessPolicy: "authenticated",
            accessible: true,
            capability: "video",
            billingMode: "fixed_request",
            priceStrategy: "flat",
            unitPriceMicrocredits: 100,
            priceTiers: [],
        },
        {
            model: "elevenlabs-voice",
            displayName: "ElevenLabs Voice",
            marketingCopy: "自然语音生成",
            promotionBadge: "",
            estimatedDurationSeconds: 15,
            brandKey: "openai",
            accessPolicy: "authenticated",
            accessible: true,
            capability: "audio",
            billingMode: "fixed_request",
            priceStrategy: "flat",
            unitPriceMicrocredits: 100,
            priceTiers: [],
        },
    ],
};

const imageModel = encodeChannelModel(mediaChannel.id, "gpt-image-2");
const memberImageModel = encodeChannelModel(mediaChannel.id, "member-image");
const videoModel = encodeChannelModel(mediaChannel.id, "seedance-2.0");
const audioModel = encodeChannelModel(mediaChannel.id, "elevenlabs-voice");
const config: AiConfig = {
    ...defaultConfig,
    channels: [mediaChannel],
    models: [imageModel, videoModel, audioModel],
    imageModels: [imageModel, memberImageModel],
    videoModels: [videoModel],
    audioModels: [audioModel],
};

describe("canvas model selection unification", () => {
    test("single-capability node menu keeps the homepage visual contract without exposing the other media type", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasModelSelectionMenu, {
                config,
                value: { image: imageModel, video: videoModel },
                capabilities: ["image"],
                onChange: () => undefined,
            }),
        );

        expect(markup).toContain("canvas-model-selection-menu");
        expect(markup).toContain("GPT Image 2");
        expect(markup).not.toContain("Seedance 2.0");
        expect(markup).not.toContain('role="radiogroup"');
    });

    test("homepage and Agent menu retain the shared two-capability selector", () => {
        const markup = renderToStaticMarkup(createElement(CanvasAgentModelMenu, { config, value: { image: imageModel, video: videoModel }, onChange: () => undefined }));

        expect(markup).toContain("canvas-model-selection-menu");
        expect(markup).toContain('role="radiogroup"');
        expect(markup).toContain("GPT Image 2");
    });

    test("node menu keeps inaccessible catalog models visible but disabled", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasModelSelectionMenu, {
                config,
                value: { image: imageModel, video: videoModel },
                capabilities: ["image"],
                modelSource: "catalog",
                onChange: () => undefined,
            }),
        );

        expect(markup).toContain("Member Image");
        expect(markup).toContain("canvas-model-selection-row--disabled");
        expect(markup).toContain('disabled=""');
    });

    test("shared menu renders configured marketing, membership, and duration facts", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasModelSelectionMenu, {
                config,
                value: { image: imageModel, video: videoModel },
                capabilities: ["image"],
                modelSource: "catalog",
                onChange: () => undefined,
            }),
        );

        expect(markup).toContain("商业级图片生成");
        expect(markup).toContain("推荐");
        expect(markup).toContain('aria-label="会员专属模型"');
        expect(markup).toContain("1min");
    });

    test.each([
        ["image", imageModel, "GPT Image 2"],
        ["video", videoModel, "Seedance 2.0"],
        ["audio", audioModel, "ElevenLabs Voice"],
    ] as const)("%s node picker uses the shared content-width trigger", (capability, value, displayName) => {
        const markup = renderToStaticMarkup(createElement(ModelPicker, { config, value, capability, presentation: "canvasMedia", fullWidth: true, onChange: () => undefined }));

        expect(markup).toContain("canvas-model-selection-trigger");
        expect(markup).toContain("model-picker-root--content");
        expect(markup).not.toContain("ant-select");
        const rootClassName = markup.match(/^<div class="([^"]+)"/)?.[1] ?? "";
        expect(rootClassName.split(/\s+/)).not.toContain("w-full");
        expect(markup).toContain(displayName);
    });
});
