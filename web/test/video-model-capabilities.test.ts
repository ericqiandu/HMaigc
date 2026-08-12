import { describe, expect, test } from "bun:test";

import { normalizeVideoConfigForModel, resolveVideoModelCapabilities, videoRatiosForMode } from "../src/lib/video-model-capabilities";
import { validateVideoDuration, videoSecondsLabel } from "../src/components/video-settings-panel";
import { defaultConfig, type AiConfig } from "../src/stores/use-config-store";

function miniMaxConfig(overrides: Partial<AiConfig> = {}): AiConfig {
    const model = "MiniMax-H3";
    return {
        ...defaultConfig,
        model,
        videoModel: model,
        channels: [{ id: "minimax", name: "MiniMax", baseUrl: "https://api.minimaxi.com", apiKey: "configured", apiFormat: "openai", interfaceType: "minimax-video", models: [model], scope: "system", enabled: true }],
        ...overrides,
    };
}

function seedanceConfig(model: string, overrides: Partial<AiConfig> = {}): AiConfig {
    return {
        ...defaultConfig,
        model,
        videoModel: model,
        channels: [{ id: "seedance", name: "Seedance", baseUrl: "https://aiopenapi.kuaizi.cn", apiKey: "configured", apiFormat: "openai", interfaceType: "ai-open-platform-video-volcengine", models: [model], scope: "system", enabled: true }],
        ...overrides,
    };
}

describe("MiniMax H3 视频能力", () => {
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

describe("Seedance 2.0 分辨率能力", () => {
    test("Fast、Pro 与 Mini 都支持 1、2、4 条并行任务", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-fast-260128")).outputCounts).toEqual([1, 2, 4]);
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128")).outputCounts).toEqual([1, 2, 4]);
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-mini-260615")).outputCounts).toEqual([1, 2, 4]);
    });

    test("Fast 开放 480P、720P 与 1080P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-fast-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p"]);
    });

    test("Mini 开放 480P、720P 与 1080P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-mini-260615")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p"]);
    });

    test("Pro 开放 480P、720P、1080P 与 4K", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p", "4k"]);
    });

    test("旧节点切换到 Fast 或 Mini 后只移除不支持的 4K", () => {
        expect(normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-fast-260128", { vquality: "4k" }), "text").vquality).toBe("720p");
        expect(normalizeVideoConfigForModel(seedanceConfig("doubao-seedance-2-0-mini-260615", { vquality: "1080p" }), "text").vquality).toBe("1080p");
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
});
