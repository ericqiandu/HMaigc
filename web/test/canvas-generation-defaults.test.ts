import { describe, expect, test } from "bun:test";

import { createCanvasNode } from "../src/lib/canvas/canvas-project-domain";
import { buildGenerationConfig } from "../src/lib/canvas/canvas-project-generation";
import { defaultConfig, type AiConfig } from "../src/stores/use-config-store";
import { CanvasNodeType } from "../src/types/canvas";

function videoConfig(overrides: Partial<AiConfig> = {}): AiConfig {
    const model = "doubao-seedance-2-0-260128";
    const selectedModel = `seedance::${model}`;
    return {
        ...defaultConfig,
        model: selectedModel,
        videoModel: selectedModel,
        models: [selectedModel],
        channels: [
            {
                id: "seedance",
                name: "Seedance",
                baseUrl: "https://aiopenapi.kuaizi.cn",
                apiKey: "configured",
                apiFormat: "openai",
                interfaceType: "ai-open-platform-video-volcengine",
                models: [model],
                scope: "system",
                enabled: true,
                modelCosts: [
                    {
                        model,
                        marketingCopy: "",
                        promotionBadge: "",
                        estimatedDurationSeconds: 0,
                        brandKey: "seedance",
                        accessPolicy: "authenticated",
                        accessible: true,
                        capability: "video",
                        watermarkCapability: "controlled",
                        billingMode: "per_second",
                        priceStrategy: "video_resolution",
                        unitPriceMicrocredits: 1,
                        priceTiers: [
                            { resolution: "720p", unitPriceMicrocredits: 1 },
                            { resolution: "1080p", unitPriceMicrocredits: 1 },
                        ],
                        providerCapabilities: {
                            providerFamily: "seedance",
                            modelKey: model,
                            displayName: "Seedance 2.0 Pro",
                            upstreamMode: model,
                            capability: "video",
                            resolutions: ["480p", "720p", "1080p", "4k"],
                            resolutionPixels: {},
                            inputVariants: ["standard", "reference_video"],
                            referenceVideoResolutions: ["480p", "720p", "1080p", "4k"],
                            ratios: ["adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"],
                            qualities: [],
                            outputCounts: [1, 2, 4],
                            durationMin: 4,
                            durationMax: 15,
                            supportsSmartDuration: true,
                            supportsGeneratedAudio: true,
                            watermarkCapability: "controlled",
                            supportsAudioOnly: false,
                            requiresAdaptiveFrames: false,
                            maxImages: 9,
                            maxImagesWithVideo: 9,
                            maxVideos: 3,
                            maxAudios: 3,
                            maxVideoDurationSeconds: 15,
                            maxAudioDurationSeconds: 15,
                            tools: ["web_search"],
                        },
                    },
                ],
            },
        ],
        ...overrides,
    };
}

describe("画布媒体节点默认参数", () => {
    test("新图片节点默认使用 16:9", () => {
        const node = createCanvasNode(CanvasNodeType.Image, { x: 0, y: 0 });
        expect(node.metadata?.size).toBe("16:9");
    });

    test("新视频节点默认使用 16:9 与 720P", () => {
        const node = createCanvasNode(CanvasNodeType.Video, { x: 0, y: 0 });
        expect(node.metadata).toMatchObject({ size: "16:9", vquality: "720p" });
    });

    test("缺少显式参数的旧图片节点不再继承持久化的 1:1", () => {
        const node = createCanvasNode(CanvasNodeType.Image, { x: 0, y: 0 }, { size: undefined });
        const config = buildGenerationConfig({ ...defaultConfig, size: "1:1" }, node, "image");
        expect(config.size).toBe("16:9");
    });

    test("明确保存为 1:1 的图片节点保持用户选择", () => {
        const node = createCanvasNode(CanvasNodeType.Image, { x: 0, y: 0 }, { size: "1:1" });
        const config = buildGenerationConfig({ ...defaultConfig, size: "16:9" }, node, "image");
        expect(config.size).toBe("1:1");
    });

    test("缺少显式参数的旧视频节点使用 16:9 与 720P", () => {
        const node = createCanvasNode(CanvasNodeType.Video, { x: 0, y: 0 }, { size: undefined, vquality: undefined });
        const config = buildGenerationConfig(videoConfig({ size: "1:1", vquality: "1080p" }), node, "video");
        expect(config).toMatchObject({ size: "16:9", vquality: "720p" });
    });

    test("明确保存的视频参数保持用户选择", () => {
        const node = createCanvasNode(CanvasNodeType.Video, { x: 0, y: 0 }, { size: "9:16", vquality: "1080p" });
        const config = buildGenerationConfig(videoConfig({ size: "16:9", vquality: "720p" }), node, "video");
        expect(config).toMatchObject({ size: "9:16", vquality: "1080p" });
    });
});
