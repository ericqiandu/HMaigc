import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { CanvasImageGenerationSettings, buildImageDimensions } from "../src/components/canvas/canvas-image-generation-settings";
import { canvasThemes } from "../src/lib/canvas-theme";
import { imageCanvasResolutionLabel, imageModelMetadataPatch, normalizeImageConfigForModel, resolveImageModelCapabilities } from "../src/lib/image-model-capabilities";
import { defaultConfig, type AiConfig, type ProviderModelCapabilities } from "../src/stores/use-config-store";

const ratios = ["1:1", "1:2", "2:1", "9:16", "16:9", "3:4", "4:3", "3:2", "2:3", "5:4", "4:5", "21:9", "9:21"] as const;

function imageConfig(capabilities: ProviderModelCapabilities, overrides: Partial<AiConfig> = {}): AiConfig {
    const model = capabilities.modelKey;
    return {
        ...defaultConfig,
        model: `images::${model}`,
        imageModel: `images::${model}`,
        channels: [
            {
                id: "images",
                name: "图片渠道",
                baseUrl: "/api/ai/system/images",
                apiKey: "system",
                apiFormat: "openai",
                models: [model],
                scope: "system",
                enabled: true,
                modelCosts: [
                    {
                        model,
                        marketingCopy: "",
                        promotionBadge: "",
                        estimatedDurationSeconds: 0,
                        brandKey: "openai",
                        accessPolicy: "authenticated",
                        accessible: true,
                        capability: "image",
                        watermarkCapability: capabilities.watermarkCapability,
                        billingMode: "fixed_request",
                        priceStrategy: "image_resolution",
                        unitPriceMicrocredits: 1,
                        priceTiers: [],
                        providerCapabilities: capabilities,
                    },
                ],
            },
        ],
        ...overrides,
    };
}

const gptImage2Capabilities: ProviderModelCapabilities = {
    providerFamily: "gpt-image2",
    modelKey: "kz_gpt_image2",
    displayName: "GPT Image 2",
    upstreamMode: "kz_gpt_image2",
    capability: "image",
    resolutions: ["1K", "2K", "4K"],
    resolutionPixels: {},
    qualities: ["low", "medium", "high"],
    outputCounts: [1],
    inputVariants: [],
    referenceVideoResolutions: [],
    ratios: [...ratios],
    durationMin: 0,
    durationMax: 0,
    supportsSmartDuration: false,
    supportsGeneratedAudio: false,
    watermarkCapability: "unsupported" as const,
    supportsAudioOnly: false,
    requiresAdaptiveFrames: false,
    maxImages: 0,
    maxImagesWithVideo: 0,
    maxVideos: 0,
    maxAudios: 0,
    maxVideoDurationSeconds: 0,
    maxAudioDurationSeconds: 0,
    tools: [],
};

describe("图片模型能力驱动参数", () => {
    test("新图片节点默认使用 16:9 横屏比例", () => {
        expect(defaultConfig.size).toBe("16:9");
    });

    test("Seedream 按后台发布的像素预算派生 2K 与 3K 尺寸", () => {
        const resolutionPixels = { "2K": 4_194_304, "3K": 9_437_184 };
        expect(buildImageDimensions("1:1", "2K", resolutionPixels)).toBe("2048x2048");
        expect(buildImageDimensions("16:9", "3K", resolutionPixels)).toBe("4096x2304");
    });

    test("没有像素预算时保持 GPT Image 2 的长边尺寸契约", () => {
        expect(buildImageDimensions("16:9", "2K", {})).toBe("2048x1152");
    });

    test("全部声明比例和清晰度都满足上游像素边界", () => {
        for (const ratio of ratios) {
            for (const resolution of ["1K", "2K", "4K"] as const) {
                const [width, height] = buildImageDimensions(ratio, resolution).split("x").map(Number);
                expect(width % 16).toBe(0);
                expect(height % 16).toBe(0);
                expect(Math.max(width, height)).toBeLessThanOrEqual(3840);
                expect(width * height).toBeGreaterThanOrEqual(655_360);
                expect(width * height).toBeLessThanOrEqual(8_294_400);
            }
        }
    });

    test("4K 正方形受像素上限缩放后仍保持 4K 参数身份", () => {
        const size = buildImageDimensions("1:1", "4K");
        expect(size).toBe("2880x2880");
        expect(imageCanvasResolutionLabel(size, ["1K", "2K", "4K"])).toBe("4K");
    });

    test("解析当前模型后台发布的参数，不按模型名称猜测", () => {
        const capabilities = resolveImageModelCapabilities(imageConfig(gptImage2Capabilities));
        expect(capabilities).toMatchObject({ resolutions: ["1K", "2K", "4K"], qualities: ["low", "medium", "high"], outputCounts: [1] });
    });

    test("参数框只渲染当前模型声明的参数分组", () => {
        const limited = imageConfig({ ...gptImage2Capabilities, resolutions: [], qualities: [], outputCounts: [1], ratios: ["1:1", "16:9"] });
        const markup = renderToStaticMarkup(
            createElement(CanvasImageGenerationSettings, {
                config: limited,
                theme: canvasThemes.dark,
                showCount: true,
                onConfigChange: () => undefined,
            }),
        );
        expect(markup).toContain("比例");
        expect(markup).not.toContain("画质");
        expect(markup).not.toContain("清晰度");
        expect(markup).not.toContain("生成数量");
    });

    test("参数选项复用画布选中态与中性表面，不使用独立灰白配色", () => {
        const markup = renderToStaticMarkup(
            createElement(CanvasImageGenerationSettings, {
                config: imageConfig(gptImage2Capabilities, { quality: "medium", size: "1024x1024" }),
                theme: canvasThemes.dark,
                showCount: true,
                onConfigChange: () => undefined,
            }),
        );
        expect(markup).toContain(`background:${canvasThemes.dark.accent.primarySoft}`);
        expect(markup).toContain(`background:${canvasThemes.dark.toolbar.itemHover}`);
        expect(markup).toContain('aria-pressed="true"');
        expect(markup).not.toContain("#525252");
    });

    test("切换模型后清理不受支持的参数并选择能力内首项", () => {
        const next = imageConfig(
            { ...gptImage2Capabilities, modelKey: "ratio-only", resolutions: [], qualities: [], outputCounts: [1], ratios: ["1:1", "16:9"] },
            { model: "images::kz_gpt_image2", size: "2048x1024", quality: "high", count: "4", transparentBackground: "true" },
        );
        const patch = imageModelMetadataPatch(next, "images::ratio-only");
        expect(patch).toEqual({ model: "images::ratio-only", size: "1:1", quality: "", count: 1, transparentBackground: "false" });
    });

    test("生成前只保留模型声明的参数", () => {
        const normalized = normalizeImageConfigForModel(
            imageConfig({ ...gptImage2Capabilities, resolutions: [], qualities: [], outputCounts: [1], ratios: ["1:1", "16:9"] }, { size: "2048x1024", quality: "high", count: "4", transparentBackground: "true" }),
        );
        expect(normalized).toMatchObject({ size: "16:9", quality: "", count: "1", transparentBackground: "false" });
    });

    test("APIMart 分辨率模型只冻结真实尺寸，不携带画质代理参数", () => {
        const normalized = normalizeImageConfigForModel(
            imageConfig(
                { ...gptImage2Capabilities, qualities: [] },
                { size: buildImageDimensions("16:9", "4K"), quality: "low" },
            ),
        );
        expect(normalized).toMatchObject({ size: "3840x2160", quality: "" });
    });

    test("后台未发布图片能力时显式失败，不回退到硬编码参数", () => {
        expect(() => resolveImageModelCapabilities({ ...defaultConfig, model: "images::unknown", channels: [] })).toThrow("缺少后台发布的图片能力契约");
    });

    test("不支持水印控制的图片模型只展示供应商决定说明", () => {
        const config = imageConfig({ ...gptImage2Capabilities, watermarkCapability: "unsupported" });
        const markup = renderToStaticMarkup(
            createElement(CanvasImageGenerationSettings, {
                config,
                theme: canvasThemes.dark,
                showCount: true,
                onConfigChange: () => undefined,
            }),
        );

        expect(markup).toContain("该模型不支持水印控制，结果由模型服务商决定");
        expect(markup).not.toContain('role="switch"');
    });
});
