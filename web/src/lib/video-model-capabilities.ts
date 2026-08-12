import { isMiniMaxH3VideoConfig, miniMaxH3DurationOptions, miniMaxH3ResolutionOptions, normalizeMiniMaxH3Duration, normalizeMiniMaxH3Resolution } from "@/lib/minimax-h3-video";
import { isKlingVideoConfig, klingDurationOptions, klingRatioOptions, klingResolutionOptions, normalizeKlingDuration, normalizeKlingResolution } from "@/lib/kling-video";
import { isSeedanceVideoConfig, normalizeResolutionToken, normalizeSeedanceRatio } from "@/lib/seedance-video";
import { normalizeVideoDuration, normalizeVideoResolution, VIDEO_DURATION_OPTIONS } from "@/lib/video-generation-options";
import { modelOptionName, resolveModelRequestConfig, type AiConfig, type VideoProviderCapabilities } from "@/stores/use-config-store";
import type { CanvasVideoGenerationMode } from "@/types/canvas";

export type VideoParameterOption = Readonly<{ value: string; label: string }>;

export type VideoModelCapabilities = Readonly<{
    id: "kling" | "minimax-h3" | "seedance" | "standard";
    resolutions: readonly VideoParameterOption[];
    ratios: readonly VideoParameterOption[];
    durations: readonly number[];
    customDurationRange?: Readonly<{ min: number; max: number }>;
    outputCounts: readonly number[];
    supportedGenerationModes: readonly CanvasVideoGenerationMode[];
    supportsGeneratedAudio: boolean;
    supportsSuperResolution: boolean;
    referenceLimits?: Readonly<{ images: number; videos: number; audios: number; totalVideoDurationSeconds: number; totalAudioDurationSeconds: number }>;
    supportedTools: readonly string[];
    requiresAdaptiveFrameRatio?: boolean;
    unsupportedReasons: Readonly<Partial<Record<"generatedAudio" | "superResolution", string>>>;
}>;

const standardResolutionOptions = [
    { value: "480p", label: "480P" },
    { value: "720p", label: "720P" },
    { value: "1080p", label: "1080P" },
] as const;

const standardRatioOptions = [
    { value: "adaptive", label: "Auto" },
    { value: "16:9", label: "16:9" },
    { value: "4:3", label: "4:3" },
    { value: "1:1", label: "1:1" },
    { value: "3:4", label: "3:4" },
    { value: "9:16", label: "9:16" },
    { value: "21:9", label: "21:9" },
] as const;

const miniMaxH3Capabilities: VideoModelCapabilities = {
    id: "minimax-h3",
    resolutions: miniMaxH3ResolutionOptions,
    ratios: standardRatioOptions,
    durations: miniMaxH3DurationOptions,
    customDurationRange: { min: 4, max: 15 },
    outputCounts: [1, 2, 4],
    supportedGenerationModes: ["text", "image", "first_last_frame", "image_reference", "omni_reference"],
    supportsGeneratedAudio: false,
    supportsSuperResolution: false,
    supportedTools: [],
    unsupportedReasons: {
        generatedAudio: "MiniMax H3 接口不提供同步生成音频参数",
        superResolution: "MiniMax H3 使用模型原生 768P / 2K 输出，不支持独立超分任务",
    },
};

const klingCapabilities: VideoModelCapabilities = {
    id: "kling",
    resolutions: klingResolutionOptions,
    ratios: klingRatioOptions,
    durations: klingDurationOptions,
    outputCounts: [1, 2, 4],
    supportedGenerationModes: ["text", "image", "first_last_frame"],
    supportsGeneratedAudio: false,
    supportsSuperResolution: false,
    supportedTools: [],
    unsupportedReasons: {
        generatedAudio: "当前可灵视频接口不支持同步生成音频",
        superResolution: "可灵视频使用模型原生输出，不支持独立超分任务",
    },
};

export function resolveVideoModelCapabilities(config: AiConfig): VideoModelCapabilities {
    if (isKlingVideoConfig(config)) return klingCapabilities;
    if (isMiniMaxH3VideoConfig(config)) return miniMaxH3Capabilities;
    const model = modelOptionName(config.model || config.videoModel);
    if (isSeedanceVideoConfig(config)) {
        const providerCapabilities = resolveSeedanceProviderCapabilities(config, model);
        return {
            id: "seedance",
            resolutions: providerCapabilities.resolutions.map((value) => ({ value, label: value.toUpperCase() })),
            ratios: providerCapabilities.ratios.map((value) => ({ value, label: value === "adaptive" ? "Auto" : value })),
            durations: Array.from({ length: providerCapabilities.durationMax - providerCapabilities.durationMin + 1 }, (_, index) => providerCapabilities.durationMin + index),
            customDurationRange: { min: providerCapabilities.durationMin, max: providerCapabilities.durationMax },
            outputCounts: [1, 2, 4],
            supportedGenerationModes: ["text", "image", "first_last_frame", "image_reference", "omni_reference"],
            supportsGeneratedAudio: providerCapabilities.supportsGeneratedAudio,
            supportsSuperResolution: false,
            referenceLimits: {
                images: providerCapabilities.maxImages,
                videos: providerCapabilities.maxVideos,
                audios: providerCapabilities.maxAudios,
                totalVideoDurationSeconds: providerCapabilities.maxVideoDurationSeconds,
                totalAudioDurationSeconds: providerCapabilities.maxAudioDurationSeconds,
            },
            supportedTools: providerCapabilities.tools,
            requiresAdaptiveFrameRatio: providerCapabilities.requiresAdaptiveFrames,
            unsupportedReasons: { superResolution: "筷子兼容接口不支持独立超分参数" },
        };
    }
    return {
        id: "standard",
        resolutions: standardResolutionOptions,
        ratios: standardRatioOptions,
        durations: VIDEO_DURATION_OPTIONS,
        outputCounts: [1, 2, 4],
        supportedGenerationModes: ["text", "image", "first_last_frame", "image_reference", "omni_reference"],
        supportsGeneratedAudio: true,
        supportsSuperResolution: true,
        supportedTools: [],
        unsupportedReasons: {},
    };
}

export function resolveSeedanceProviderCapabilities(config: AiConfig, model: string): VideoProviderCapabilities {
    const resolved = resolveModelRequestConfig(config, config.model || config.videoModel);
    const channel = config.channels.find((candidate) => candidate.id === resolved.channelId);
    const capabilities = channel?.modelCosts?.find((candidate) => candidate.model === model)?.providerCapabilities;
    if (!capabilities || capabilities.modelKey !== model || capabilities.capability !== "video") {
        throw new Error(`模型 ${model} 缺少后台发布的视频能力契约`);
    }
    return capabilities;
}

export function normalizeVideoConfigForModel(config: AiConfig, generationMode?: CanvasVideoGenerationMode): AiConfig {
    const capabilities = resolveVideoModelCapabilities(config);
    const normalizedResolution =
        capabilities.id === "kling"
            ? normalizeKlingResolution(config.vquality)
            : capabilities.id === "minimax-h3"
              ? normalizeMiniMaxH3Resolution(config.vquality)
              : capabilities.id === "seedance"
                ? normalizeSeedanceCapabilityResolution(config.vquality, capabilities)
                : `${normalizeVideoResolution(config.vquality)}p`;
    const normalizedDuration =
        capabilities.id === "kling"
            ? normalizeKlingDuration(config.videoSeconds)
            : capabilities.id === "minimax-h3"
              ? normalizeMiniMaxH3Duration(config.videoSeconds)
              : capabilities.id === "seedance"
                ? normalizeSeedanceCapabilityDuration(config.videoSeconds, capabilities)
                : Number(normalizeVideoDuration(config.videoSeconds));
    const normalizedRatio = capabilities.id === "seedance" ? normalizeSeedanceCapabilityRatio(config.size, capabilities) : normalizeSeedanceRatio(config.size);
    const ratioOptions = videoRatiosForMode(capabilities, generationMode);
    const supportedRatio = ratioOptions.some((option) => option.value === normalizedRatio) ? normalizedRatio : ratioOptions[0].value;
    const requestedCount = Math.max(1, Math.floor(Math.abs(Number(config.count)) || 1));
    const normalizedCount = capabilities.outputCounts.reduce((nearest, option) => (Math.abs(option - requestedCount) < Math.abs(nearest - requestedCount) ? option : nearest), capabilities.outputCounts[0]);
    return {
        ...config,
        vquality: normalizedResolution,
        videoSeconds: String(normalizedDuration),
        size: supportedRatio,
        count: String(normalizedCount),
        videoGenerateAudio: capabilities.supportsGeneratedAudio ? config.videoGenerateAudio : "false",
        videoSuperResolutionEnabled: capabilities.supportsSuperResolution ? config.videoSuperResolutionEnabled : "false",
        videoSuperResolutionFps: capabilities.supportsSuperResolution ? config.videoSuperResolutionFps : "",
    };
}

function normalizeSeedanceCapabilityResolution(value: string, capabilities: VideoModelCapabilities) {
    const normalized = normalizeResolutionToken(value);
    if (capabilities.resolutions.some((option) => option.value === normalized)) return normalized;
    return capabilities.resolutions.find((option) => option.value === "720p")?.value || capabilities.resolutions[0].value;
}

function normalizeSeedanceCapabilityDuration(value: string, capabilities: VideoModelCapabilities) {
    const requested = Math.round(Number(value));
    const range = capabilities.customDurationRange;
    if (!range) throw new Error("Seedance 模型缺少生成时长能力契约");
    return Math.max(range.min, Math.min(range.max, Number.isFinite(requested) ? requested : range.min));
}

function normalizeSeedanceCapabilityRatio(value: string, capabilities: VideoModelCapabilities) {
    const requested = value === "auto" ? "adaptive" : value;
    if (capabilities.ratios.some((option) => option.value === requested)) return requested;
    const dimensions = requested.match(/^(\d+)x(\d+)$/);
    if (!dimensions) return capabilities.ratios[0].value;
    const requestedRatio = Number(dimensions[1]) / Number(dimensions[2]);
    const numericOptions = capabilities.ratios.flatMap((option) => {
        const parts = option.value.match(/^(\d+):(\d+)$/);
        return parts ? [{ value: option.value, ratio: Number(parts[1]) / Number(parts[2]) }] : [];
    });
    if (!numericOptions.length || !Number.isFinite(requestedRatio)) return capabilities.ratios[0].value;
    return numericOptions.reduce((nearest, option) => (Math.abs(option.ratio - requestedRatio) < Math.abs(nearest.ratio - requestedRatio) ? option : nearest)).value;
}

export function videoRatiosForMode(capabilities: VideoModelCapabilities, mode?: CanvasVideoGenerationMode) {
    if (capabilities.id === "kling" && mode && mode !== "text") return [{ value: "adaptive", label: "Auto" }] as const;
    if (capabilities.requiresAdaptiveFrameRatio && mode === "first_last_frame") return [{ value: "adaptive", label: "Auto" }] as const;
    if (capabilities.id !== "minimax-h3") return capabilities.ratios;
    if (!mode || mode === "text") return capabilities.ratios.filter((option) => option.value !== "adaptive");
    return capabilities.ratios.filter((option) => option.value === "adaptive");
}

export function videoModelMetadataPatch(config: AiConfig, model: string, generationMode?: CanvasVideoGenerationMode) {
    const nextConfig = { ...config, model };
    const capabilities = resolveVideoModelCapabilities(nextConfig);
    const effectiveMode = generationMode && capabilities.supportedGenerationModes.includes(generationMode) ? generationMode : "text";
    const normalized = normalizeVideoConfigForModel(nextConfig, effectiveMode);
    return {
        model,
        videoGenerationMode: effectiveMode,
        size: normalized.size,
        seconds: normalized.videoSeconds,
        vquality: normalized.vquality,
        generateAudio: normalized.videoGenerateAudio,
        superResolutionEnabled: normalized.videoSuperResolutionEnabled,
        superResolutionFps: normalized.videoSuperResolutionFps,
        count: Number(normalized.count),
    };
}
