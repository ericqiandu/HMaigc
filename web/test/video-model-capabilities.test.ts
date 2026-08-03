import { describe, expect, test } from "bun:test";

import { normalizeVideoConfigForModel, resolveVideoModelCapabilities } from "../src/lib/video-model-capabilities";
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
        channels: [{ id: "seedance", name: "Seedance", baseUrl: "https://aiopenapi.kuaizi.cn", apiKey: "configured", apiFormat: "openai", interfaceType: "ai-open-platform-video", models: [model], scope: "system", enabled: true }],
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

    test("Fast 仅开放 480P 与 720P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-fast-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p"]);
    });

    test("Mini 仅开放 480P 与 720P", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-mini-260615")).resolutions.map((option) => option.value)).toEqual(["480p", "720p"]);
    });

    test("Pro 开放 480P、720P、1080P 与 4K", () => {
        expect(resolveVideoModelCapabilities(seedanceConfig("doubao-seedance-2-0-260128")).resolutions.map((option) => option.value)).toEqual(["480p", "720p", "1080p", "4k"]);
    });

    test("旧节点切换到 Fast 或 Mini 后不会保留不支持的高分辨率", () => {
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
});
