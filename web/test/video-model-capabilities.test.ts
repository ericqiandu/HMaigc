import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { hasPublishedVideoModel, normalizeVideoConfigForModel, resolveVideoModelCapabilities, videoModelMetadataPatch, videoRatiosForMode, videoResolutionsForMode } from "../src/lib/video-model-capabilities";
import { VideoSettingsPanel, validateVideoDuration, videoSecondsLabel } from "../src/components/video-settings-panel";
import { canvasThemes } from "../src/lib/canvas-theme";
import { defaultConfig, type AiConfig } from "../src/stores/use-config-store";
import { seedanceReferenceError } from "../src/lib/seedance-video";
import { normalizedPricingTierKey, specificationsForModel } from "../src/pages/admin/model-pricing/pricing-specifications";

function miniMaxConfig(overrides: Partial<AiConfig> = {}): AiConfig {
    const model = "MiniMax-H3";
    return {
        ...defaultConfig,
        model,
        videoModel: model,
        channels: [
            {
                id: "minimax",
                name: "MiniMax",
                baseUrl: "https://api.minimaxi.com",
                apiKey: "configured",
                apiFormat: "openai",
                interfaceType: "minimax-video",
                models: [model],
                scope: "system",
                enabled: true,
                modelCosts: [
                    {
                        model,
                        marketingCopy: "",
                        promotionBadge: "",
                        estimatedDurationSeconds: 0,
                        brandKey: "minimax",
                        accessPolicy: "authenticated",
                        accessible: true,
                        capability: "video",
                        watermarkCapability: "controlled",
                        billingMode: "per_second",
                        priceStrategy: "video_resolution",
                        unitPriceMicrocredits: 1,
                        priceTiers: [],
                    },
                ],
            },
        ],
        ...overrides,
    };
}

function seedanceConfig(model: string, overrides: Partial<AiConfig> = {}): AiConfig {
    const is25 = model === "doubao-seedance-2-5-260628";
    const isPro = model === "doubao-seedance-2-0-260128";
    const isMini = model === "doubao-seedance-2-0-mini-260615";
    const providerCapabilities = {
        providerFamily: "seedance",
        modelKey: model,
        displayName: is25 ? "Seedance 2.5" : isPro ? "Seedance 2.0 Pro" : model.includes("fast") ? "Seedance 2.0 Fast" : "Seedance 2.0 Mini",
        upstreamMode: model,
        capability: "video",
        resolutions: is25 || isMini ? ["480p", "720p"] : isPro ? ["480p", "720p", "1080p", "4k"] : ["480p", "720p", "1080p"],
        resolutionPixels: {},
        inputVariants: ["standard", "reference_video"] as Array<"standard" | "reference_video">,
        referenceVideoResolutions: is25 || isMini ? ["480p", "720p"] : isPro ? ["480p", "720p", "1080p", "4k"] : ["480p", "720p", "1080p"],
        generatedAudioResolutions: [],
        ratios: ["adaptive", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"],
        qualities: [],
        outputCounts: [1, 2, 4],
        durationMin: 4,
        durationMax: is25 ? 30 : 15,
        supportsSmartDuration: true,
        supportsGeneratedAudio: true,
        watermarkCapability: "controlled" as const,
        supportsAudioOnly: is25,
        requiresAdaptiveFrames: is25,
        maxImages: is25 ? 30 : 9,
        maxImagesWithVideo: is25 ? 30 : 9,
        maxVideos: is25 ? 10 : 3,
        maxAudios: is25 ? 10 : 3,
        maxVideoDurationSeconds: is25 ? 30 : 15,
        maxAudioDurationSeconds: is25 ? 30 : 15,
        tools: is25 ? [] : ["web_search"],
    };
    return {
        ...defaultConfig,
        model,
        videoModel: model,
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
                        watermarkCapability: providerCapabilities.watermarkCapability,
                        billingMode: "per_second",
                        priceStrategy: "video_resolution",
                        unitPriceMicrocredits: 1,
                        priceTiers: [],
                        providerCapabilities,
                    },
                ],
            },
        ],
        ...overrides,
    };
}

function kuaiziKlingConfig(overrides: Partial<AiConfig> = {}): AiConfig {
    const model = "kling-v3-omni";
    const providerCapabilities = {
        providerFamily: "kling",
        modelKey: model,
        displayName: "Kling 3 Omni",
        upstreamMode: model,
        capability: "video",
        resolutions: ["std", "pro", "4k"],
        resolutionPixels: {},
        inputVariants: ["standard", "standard_audio", "reference_video"] as Array<"standard" | "standard_audio" | "reference_video">,
        referenceVideoResolutions: ["std", "pro"],
        generatedAudioResolutions: ["std", "pro"],
        ratios: ["16:9", "9:16", "1:1"],
        qualities: ["std", "pro", "4k"],
        outputCounts: [1],
        durationMin: 3,
        durationMax: 15,
        supportsSmartDuration: false,
        supportsGeneratedAudio: true,
        watermarkCapability: "unsupported" as const,
        supportsAudioOnly: false,
        requiresAdaptiveFrames: false,
        maxImages: 7,
        maxImagesWithVideo: 4,
        maxVideos: 1,
        maxAudios: 0,
        maxVideoDurationSeconds: 10,
        maxAudioDurationSeconds: 0,
        tools: [],
    };
    return {
        ...defaultConfig,
        model,
        videoModel: model,
        channels: [
            {
                id: "kuaizi-video",
                name: "筷子科技",
                baseUrl: "https://aiopenapi.kuaizi.cn",
                apiKey: "system",
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
                        brandKey: "kling",
                        accessPolicy: "authenticated",
                        accessible: true,
                        capability: "video",
                        watermarkCapability: "unsupported",
                        billingMode: "per_second",
                        priceStrategy: "video_resolution",
                        unitPriceMicrocredits: 1,
                        priceTiers: [],
                        providerCapabilities,
                    },
                ],
            },
        ],
        ...overrides,
    };
}

describe("筷子 Kling 3 Omni 视频能力", () => {
    test("商业定价键忽略分辨率和输入模式的大小写差异", () => {
        expect(normalizedPricingTierKey("STD", "STANDARD_AUDIO")).toBe("std::standard_audio");
        expect(normalizedPricingTierKey("4K", "standard")).toBe("4k::standard");
    });

    test("从后台发布契约读取模式、时长、音频与单任务限制", () => {
        const capabilities = resolveVideoModelCapabilities(kuaiziKlingConfig());
        expect(capabilities.id).toBe("kuaizi-kling");
        expect(capabilities.resolutions.map((option) => option.value)).toEqual(["std", "pro", "4k"]);
        expect(capabilities.customDurationRange).toEqual({ min: 3, max: 15 });
        expect(capabilities.outputCounts).toEqual([1]);
        expect(capabilities.supportsGeneratedAudio).toBe(true);
        expect(normalizeVideoConfigForModel(kuaiziKlingConfig({ vquality: "pro" }), "text").vquality).toBe("pro");
    });

    test("4K 不在后台有声分辨率目录时强制关闭同步音频", () => {
        expect(normalizeVideoConfigForModel(kuaiziKlingConfig({ vquality: "4k", videoGenerateAudio: "true" }), "text").videoGenerateAudio).toBe("false");
        expect(normalizeVideoConfigForModel(kuaiziKlingConfig({ vquality: "pro", videoGenerateAudio: "true" }), "text").videoGenerateAudio).toBe("true");
    });

    test("参考视频模式只开放上游允许的 std 与 pro", () => {
        const capabilities = resolveVideoModelCapabilities(kuaiziKlingConfig());
        expect(videoResolutionsForMode(capabilities, "omni_reference").map((option) => option.value)).toEqual(["std", "pro"]);
        expect(normalizeVideoConfigForModel(kuaiziKlingConfig({ vquality: "4k", videoSeconds: "12" }), "omni_reference")).toMatchObject({ vquality: "std", videoSeconds: "10", videoGenerateAudio: "false" });
    });

    test("定价规格不生成上游禁止的 4k 参考视频组合", () => {
        const providerCapabilities = kuaiziKlingConfig().channels[0].modelCosts?.[0].providerCapabilities;
        if (!providerCapabilities) throw new Error("测试模型缺少供应商能力");
        const specifications = specificationsForModel({ modelKey: "kling-v3-omni", priceStrategy: "video_resolution", providerCapabilities });
        expect(specifications.map((item) => item.key)).toEqual(["std::standard", "std::standard_audio", "std::reference_video", "pro::standard", "pro::standard_audio", "pro::reference_video", "4k::standard"]);
        expect(specifications.find((item) => item.key === "std::standard_audio")?.label).toBe("std · 普通生成（有声）");
    });
});

describe("MiniMax H3 视频能力", () => {
    test("新视频节点默认使用 16:9 与 720P", () => {
        const normalized = normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-fast-260128"), "text");
        expect(normalized).toMatchObject({ size: "16:9", vquality: "720p" });
    });

    test("后台未发布视频模型时不进入视频能力解析", () => {
        expect(hasPublishedVideoModel(defaultConfig)).toBe(false);
    });

    test("视频节点开放一次创建 1、2 或 4 条独立生成任务", () => {
        expect(resolveVideoModelCapabilities(miniMaxConfig()).outputCounts).toEqual([1, 2, 4]);
    });

    test("多输出设置在执行前保持受支持的任务数量", () => {
        expect(normalizeVideoConfigForModel(miniMaxConfig({ count: "4" }), "text").count).toBe("4");
    });

    test("保留 H3 原生 2K 与时长边界", () => {
        const normalized = normalizeVideoConfigForModel(miniMaxConfig({ vquality: "2k", videoSeconds: "15" }), "text");
        expect(normalized.vquality).toBe("2k");
        expect(normalized.videoSeconds).toBe("15");
    });

    test("MiniMax H3 接受 4 至 15 秒内的自定义整数", () => {
        const normalized = normalizeVideoConfigForModel(miniMaxConfig({ videoSeconds: "7" }), "text");
        expect(normalized.videoSeconds).toBe("7");
    });
});

describe("视频时长输入", () => {
    test("节点摘要展示真实秒数而不是旧固定档位的近似值", () => {
        expect(videoSecondsLabel("5")).toBe("5s");
        expect(videoSecondsLabel("7")).toBe("7s");
    });

    test("连续范围模型接受范围内的自定义整数", () => {
        expect(validateVideoDuration("7", [4, 5, 6], { min: 4, max: 15 })).toEqual({ valid: true, value: 7 });
    });

    test("连续范围模型明确拒绝越界和小数", () => {
        expect(validateVideoDuration("16", [4, 5, 6], { min: 4, max: 15 })).toEqual({ valid: false, message: "当前模型仅支持 4–15 秒" });
        expect(validateVideoDuration("7.5", [4, 5, 6], { min: 4, max: 15 })).toEqual({ valid: false, message: "请输入整数秒数" });
    });

    test("离散时长模型只接受渠道支持的秒数", () => {
        expect(validateVideoDuration("5", [5, 10])).toEqual({ valid: true, value: 5 });
        expect(validateVideoDuration("7", [5, 10])).toEqual({ valid: false, message: "当前模型仅支持 5 / 10 秒" });
    });
});

describe("画布视频设置", () => {
    test("不再展示独立视频超分功能", () => {
        const markup = renderToStaticMarkup(
            createElement(VideoSettingsPanel, {
                config: seedanceConfig("doubao-seedance-2-5-260628"),
                onConfigChange: () => undefined,
                theme: canvasThemes.dark,
                showTitle: false,
            }),
        );

        expect(markup).not.toContain("视频超分");
        expect(markup).not.toContain("独立超分");
    });

    test("视频参数与图片参数复用同一分组、选项和选中态", () => {
        const markup = renderToStaticMarkup(
            createElement(VideoSettingsPanel, {
                config: seedanceConfig("doubao-seedance-2-5-260628"),
                onConfigChange: () => undefined,
                theme: canvasThemes.dark,
                showTitle: false,
            }),
        );
        const globalsCSS = readFileSync(new URL("../src/styles/globals.css", import.meta.url), "utf8");

        expect(markup).toContain("canvas-generation-settings-section");
        expect(markup).toContain("canvas-generation-settings-option");
        expect(markup).toContain("canvas-generation-settings-ratio-option");
        expect(markup).toContain(`background:${canvasThemes.dark.accent.primarySoft}`);
        expect(markup).toContain(`background:${canvasThemes.dark.toolbar.itemHover}`);
        expect(markup).toContain('aria-pressed="true"');
        expect(markup).not.toContain("canvas-video-option-button");
        expect(globalsCSS).not.toContain(".canvas-video-generation-settings button[data-selected");
        expect(globalsCSS).not.toContain("\n    .canvas-video-settings-popover {\n");
    });

    test("不支持水印控制的模型只展示供应商决定说明", () => {
        const config = seedanceConfig("doubao-seedance-2-5-260628");
        const providerCapabilities = config.channels[0].modelCosts?.[0].providerCapabilities;
        if (!providerCapabilities) throw new Error("测试模型缺少供应商能力");
        providerCapabilities.watermarkCapability = "unsupported";

        const markup = renderToStaticMarkup(
            createElement(VideoSettingsPanel, {
                config,
                onConfigChange: () => undefined,
                theme: canvasThemes.dark,
                showTitle: false,
            }),
        );

        expect(markup).toContain("该模型不支持水印控制，结果由模型服务商决定");
        expect(markup).not.toContain("videoWatermark");
        expect(markup).not.toContain('role="switch"');
    });
});

describe("Seedance 2.0 分辨率能力", () => {
    test("Agent 视频节点切换模型时原子替换渠道与原始模型名", () => {
        const model = "doubao-seedance-2-0-mini-260615";
        const patch = videoModelMetadataPatch(seedanceConfig(model), `seedance::${model}`, "text");

        expect(patch).toMatchObject({ channelId: "seedance", model });
    });

    test("Fast、Pro 与 Mini 都支持 1、2、4 条并行任务", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-fast-260128")).outputCounts).toEqual([1, 2, 4]);
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128")).outputCounts).toEqual([1, 2, 4]);
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-mini-260615")).outputCounts).toEqual([1, 2, 4]);
    });

    test("Fast 开放 480P、720P 与 1080P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-fast-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p"]);
    });

    test("Mini 只开放供应商支持的 480P 与 720P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-mini-260615")).resolutions.map((option) => option.value)).toEqual(["480p", "720p"]);
    });

    test("Mini 未单列有声分辨率时为全部已发布分辨率生成有声定价规格", () => {
        const config = seedanceConfig("doubao-seedance-2-0-mini-260615");
        const providerCapabilities = config.channels[0].modelCosts?.[0].providerCapabilities;
        if (!providerCapabilities) throw new Error("测试模型缺少供应商能力");
        providerCapabilities.inputVariants = ["standard", "standard_audio", "reference_video"];

        const specifications = specificationsForModel({ modelKey: "doubao-seedance-2-0-mini-260615", priceStrategy: "video_resolution", providerCapabilities });

        expect(specifications.map((item) => item.key)).toEqual([
            "480p::standard",
            "480p::standard_audio",
            "480p::reference_video",
            "720p::standard",
            "720p::standard_audio",
            "720p::reference_video",
        ]);
    });

    test("Pro 开放 480P、720P、1080P 与 4K", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p", "4k"]);
    });

    test("旧节点切换到 Fast 或 Mini 后移除目标模型不支持的分辨率", () => {
        expect(normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-fast-260128", { vquality: "4k" }), "text").vquality).toBe("720p");
        expect(normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-mini-260615", { vquality: "1080p" }), "text").vquality).toBe("720p");
    });

    test("Pro 的 4K 参数在执行前保持不变", () => {
        expect(normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-260128", { vquality: "4K" }), "text").vquality).toBe("4k");
    });

    test("Seedance 接受 4 至 15 秒内的自定义整数", () => {
        const normalized = normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-260128", { videoSeconds: "7" }), "text");
        expect(normalized.videoSeconds).toBe("7");
    });

    test("Seedance 2.5 只开放 480P/720P、最长 30 秒且关闭兼容接口不支持的超分", () => {
        const config = seedanceConfig("doubao-seedance-2-5-260628", { videoSeconds: "30", vquality: "1080p", videoSuperResolutionEnabled: "true" });
        const capabilities = resolveVideoModelCapabilities(config);
        expect(capabilities.resolutions.map((option) => option.value)).toEqual(["480p", "720p"]);
        expect(capabilities.customDurationRange).toEqual({ min: 4, max: 30 });
        expect(capabilities.supportsSuperResolution).toBe(false);
        const normalized = normalizeVideoConfigForModel(config, "first_last_frame");
        expect(normalized).toMatchObject({ videoSeconds: "30", vquality: "720p", size: "adaptive", videoSuperResolutionEnabled: "false" });
    });

    test("只有 2.5 首尾帧强制自适应比例，2.0 保留兼容接口支持的画幅", () => {
        const seedance20 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128"));
        const seedance25 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-5-260628"));
        expect(videoRatiosForMode(seedance20, "first_last_frame").map((option) => option.value)).toContain("16:9");
        expect(videoRatiosForMode(seedance25, "first_last_frame").map((option) => option.value)).toEqual(["adaptive"]);
    });

    test("动态能力公开各型号素材配额和联网搜索支持", () => {
        const seedance20 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128"));
        const seedance25 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-5-260628"));
        expect(seedance20.referenceLimits).toMatchObject({ images: 9, videos: 3, audios: 3, totalVideoDurationSeconds: 15, totalAudioDurationSeconds: 15 });
        expect(seedance20.supportedTools).toEqual(["web_search"]);
        expect(seedance25.referenceLimits).toMatchObject({ images: 30, videos: 10, audios: 10, totalVideoDurationSeconds: 30, totalAudioDurationSeconds: 30 });
        expect(seedance25.supportedTools).toEqual([]);
    });

    test("2.5 接受 30 秒参考素材而 2.0 明确拒绝，不静默截断", () => {
        const videos = [
            { id: "v1", name: "v1.mp4", type: "video/mp4", url: "https://cdn.example.com/v1.mp4", durationMs: 15_000 },
            { id: "v2", name: "v2.mp4", type: "video/mp4", url: "https://cdn.example.com/v2.mp4", durationMs: 15_000 },
        ];
        const seedance25 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-5-260628"));
        const seedance20 = resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128"));
        expect(seedanceReferenceError("Seedance 2.5", seedance25.referenceLimits!, [], videos, [])).toBe("");
        expect(seedanceReferenceError("Seedance 2.0 Pro", seedance20.referenceLimits!, [], videos, [])).toBe("Seedance 2.0 Pro 参考视频总时长不能超过 15 秒");
    });

    test("提交校验消费后台动态配额并保留原数组", () => {
        const images = Array.from({ length: 10 }, (_, index) => ({ id: `i${index}`, name: `i${index}.png`, type: "image/png", dataUrl: "data:image/png;base64,AA==" }));
        const limits = { images: 1, videos: 2, audios: 3, totalVideoDurationSeconds: 12, totalAudioDurationSeconds: 13 };
        expect(seedanceReferenceError("后台动态模型", limits, images, [], [])).toBe("后台动态模型 最多支持 1 张参考图片，当前 10 张");
        expect(images).toHaveLength(10);
    });
});
