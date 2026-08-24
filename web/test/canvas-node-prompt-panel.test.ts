import { describe, expect, test } from "bun:test";

import { buildNodeConfig } from "@/components/canvas/canvas-node-prompt-panel";
import { defaultConfig, encodeChannelModel, type AiConfig } from "@/stores/use-config-store";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

describe("buildNodeConfig", () => {
    test("uses the Agent-frozen channel, model and video parameters instead of global defaults", () => {
        const frozenModel = "doubao-seedance-2-0-mini-260615";
        const config: AiConfig = {
            ...defaultConfig,
            model: "default-channel::doubao-seedance-2-0-260128",
            videoModel: "default-channel::doubao-seedance-2-0-260128",
            models: ["default-channel::doubao-seedance-2-0-260128", `seedance-channel::${frozenModel}`],
            videoModels: ["default-channel::doubao-seedance-2-0-260128", `seedance-channel::${frozenModel}`],
            videoSeconds: "6",
            vquality: "1080p",
            size: "16:9",
            videoGenerateAudio: "true",
            channels: [
                {
                    id: "default-channel",
                    name: "Default",
                    baseUrl: "/api/system/channels/default-channel",
                    apiKey: "",
                    apiFormat: "openai",
                    interfaceType: "ai-open-platform-video-volcengine",
                    models: ["doubao-seedance-2-0-260128"],
                    scope: "system",
                    enabled: true,
                    modelCosts: [],
                },
                {
                    id: "seedance-channel",
                    name: "Seedance",
                    baseUrl: "/api/system/channels/seedance-channel",
                    apiKey: "",
                    apiFormat: "openai",
                    interfaceType: "ai-open-platform-video-volcengine",
                    models: [frozenModel],
                    scope: "system",
                    enabled: true,
                    modelCosts: [
                        {
                            model: frozenModel,
                            displayName: "Seedance 2.0 Mini",
                            marketingCopy: "",
                            promotionBadge: "",
                            estimatedDurationSeconds: 30,
                            brandKey: "seedance",
                            accessPolicy: "authenticated",
                            accessible: true,
                            capability: "video",
                            watermarkCapability: "controlled",
                            billingMode: "per_second",
                            priceStrategy: "video_resolution",
                            unitPriceMicrocredits: 0,
                            priceTiers: [{ resolution: "720p", inputVariant: "standard", unitPriceMicrocredits: 200_000 }],
                            providerCapabilities: {
                                providerFamily: "seedance",
                                modelKey: frozenModel,
                                displayName: "Seedance 2.0 Mini",
                                upstreamMode: "video",
                                capability: "video",
                                resolutions: ["480p", "720p"],
                                resolutionPixels: {},
                                inputVariants: ["standard"],
                                referenceVideoResolutions: [],
                                generatedAudioResolutions: [],
                                ratios: ["adaptive", "16:9", "9:16"],
                                qualities: [],
                                outputCounts: [1],
                                durationMin: 4,
                                durationMax: 15,
                                supportsSmartDuration: false,
                                supportsGeneratedAudio: false,
                                watermarkCapability: "controlled",
                                supportsAudioOnly: false,
                                requiresAdaptiveFrames: false,
                                maxImages: 1,
                                maxImagesWithVideo: 1,
                                maxVideos: 0,
                                maxAudios: 0,
                                maxVideoDurationSeconds: 0,
                                maxAudioDurationSeconds: 0,
                                tools: [],
                            },
                        },
                    ],
                },
            ],
        };
        const node: CanvasNodeData = {
            id: "agent-video",
            type: CanvasNodeType.Video,
            title: "Agent video",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: {
                channelId: "seedance-channel",
                model: frozenModel,
                size: "adaptive",
                seconds: "5",
                vquality: "720p",
                generateAudio: "false",
                videoEditOperation: "text_to_video",
            },
        };

        const result = buildNodeConfig(config, node, "video");

        expect(result.model).toBe(encodeChannelModel("seedance-channel", frozenModel));
        expect(result.size).toBe("adaptive");
        expect(result.videoSeconds).toBe("5");
        expect(result.vquality).toBe("720p");
        expect(result.videoGenerateAudio).toBe("false");
    });

    test("does not replace a frozen model with the global default after catalog removal", () => {
        const node: CanvasNodeData = {
            id: "historical-agent-video",
            type: CanvasNodeType.Video,
            title: "Historical Agent video",
            position: { x: 0, y: 0 },
            width: 420,
            height: 236,
            metadata: { channelId: "retired-channel", model: "retired-video-model", seconds: "5", size: "9:16", vquality: "720p", generateAudio: "false" },
        };

        const result = buildNodeConfig({ ...defaultConfig, videoModel: "current-channel::current-video-model" }, node, "video");

        expect(result.model).toBe("retired-channel::retired-video-model");
    });
});
