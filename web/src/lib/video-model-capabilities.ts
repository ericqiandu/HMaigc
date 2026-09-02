import { normalizeResolutionToken } from "@/lib/seedance-video";
import {
    configuredModelMatchesCapability,
    modelOptionName,
    resolveModelRequestConfig,
    type AiConfig,
    type ProviderModelCapabilities,
    type WatermarkCapability,
} from "@/stores/use-config-store";
import type { CanvasVideoGenerationMode } from "@/types/canvas";

export type VideoParameterOption = Readonly<{ value: string; label: string }>;

export type VideoModelCapabilities = Readonly<{
    id: string;
    resolutions: readonly VideoParameterOption[];
    referenceVideoResolutions: readonly string[];
    ratios: readonly VideoParameterOption[];
    durations: readonly number[];
    customDurationRange?: Readonly<{ min: number; max: number }>;
    outputCounts: readonly number[];
    supportedGenerationModes: readonly CanvasVideoGenerationMode[];
    supportsGeneratedAudio: boolean;
    supportsNativeAudio: boolean;
    supportsDialogue: boolean;
    supportsVoiceReference: boolean;
    supportsLipSync: boolean;
    supportsIndependentAudio: boolean;
    supportsGeneratedAudioWithReferenceVideo: boolean;
    generatedAudioResolutions: readonly string[];
    watermarkCapability: WatermarkCapability;
    supportsSuperResolution: boolean;
    referenceLimits?: Readonly<{
        images: number;
        imagesWithVideo: number;
        videos: number;
        audios: number;
        totalVideoDurationSeconds: number;
        totalAudioDurationSeconds: number;
    }>;
    supportedTools: readonly string[];
    adaptiveRatioModes: readonly CanvasVideoGenerationMode[];
    requiredAdaptiveRatioModes: readonly CanvasVideoGenerationMode[];
    inputVariants: readonly ("standard" | "standard_audio" | "reference_video")[];
    unsupportedReasons: Readonly<Partial<Record<"generatedAudio" | "superResolution", string>>>;
}>;

export function hasPublishedVideoModel(config: AiConfig) {
    return configuredModelMatchesCapability(config, config.model || config.videoModel, "video");
}

export function resolveVideoModelCapabilities(config: AiConfig): VideoModelCapabilities {
    const model = modelOptionName(config.model || config.videoModel);
    const providerCapabilities = resolvePublishedVideoProviderCapabilities(config, model);
    if (!providerCapabilities) throw new Error(`模型 ${model} 缺少后台发布的视频能力契约`);
    assertCompletePublishedVideoCapabilities(providerCapabilities, model);
    const generationModes = parseVideoGenerationModes(providerCapabilities.generationModes, model);
    const adaptiveRatioModes = parseVideoGenerationModes(providerCapabilities.adaptiveRatioModes, model);
    const requiredAdaptiveRatioModes = parseVideoGenerationModes(providerCapabilities.requiredAdaptiveRatioModes, model);

    return {
        id: providerCapabilities.providerFamily,
        resolutions: providerCapabilities.resolutions.map(parameterOption),
        referenceVideoResolutions: [...providerCapabilities.referenceVideoResolutions],
        ratios: providerCapabilities.ratios.map(parameterOption),
        durations: [...providerCapabilities.durations],
        customDurationRange: continuousDurationRange(providerCapabilities.durations),
        outputCounts: [...providerCapabilities.outputCounts],
        supportedGenerationModes: generationModes,
        supportsGeneratedAudio: providerCapabilities.supportsGeneratedAudio,
        supportsNativeAudio: providerCapabilities.supportsNativeAudio,
        supportsDialogue: providerCapabilities.supportsDialogue,
        supportsVoiceReference: providerCapabilities.supportsVoiceReference,
        supportsLipSync: providerCapabilities.supportsLipSync,
        supportsIndependentAudio: providerCapabilities.supportsIndependentAudio,
        supportsGeneratedAudioWithReferenceVideo: providerCapabilities.supportsGeneratedAudioWithReferenceVideo,
        generatedAudioResolutions: [...providerCapabilities.generatedAudioResolutions],
        watermarkCapability: providerCapabilities.watermarkCapability,
        supportsSuperResolution: false,
        referenceLimits: {
            images: providerCapabilities.maxImages,
            imagesWithVideo: providerCapabilities.maxImagesWithVideo,
            videos: providerCapabilities.maxVideos,
            audios: providerCapabilities.maxAudios,
            totalVideoDurationSeconds: providerCapabilities.maxVideoDurationSeconds,
            totalAudioDurationSeconds: providerCapabilities.maxAudioDurationSeconds,
        },
        supportedTools: [...providerCapabilities.tools],
        adaptiveRatioModes,
        requiredAdaptiveRatioModes,
        inputVariants: [...providerCapabilities.inputVariants],
        unsupportedReasons: {
            generatedAudio: providerCapabilities.supportsNativeAudio ? undefined : "当前模型的服务端能力契约未开放同步生成音频",
            superResolution: "当前模型的服务端能力契约未开放独立超分",
        },
    };
}

function parameterOption(value: string): VideoParameterOption {
    return { value, label: value === "adaptive" ? "Auto" : value.toUpperCase() };
}

function continuousDurationRange(durations: readonly number[]) {
    if (durations.length === 0) return undefined;
    for (let index = 1; index < durations.length; index += 1) {
        if (durations[index] !== durations[index - 1] + 1) return undefined;
    }
    return { min: durations[0], max: durations[durations.length - 1] };
}

function resolvePublishedVideoProviderCapabilities(config: AiConfig, model: string): ProviderModelCapabilities | undefined {
    const resolved = resolveModelRequestConfig(config, config.model || config.videoModel);
    const channel = config.channels.find((candidate) => candidate.id === resolved.channelId);
    const capabilities = channel?.modelCosts?.find((candidate) => candidate.model === model)?.providerCapabilities;
    if (!capabilities) return undefined;
    if (capabilities.modelKey !== model || capabilities.capability !== "video") {
        throw new Error(`模型 ${model} 缺少后台发布的视频能力契约`);
    }
    return capabilities;
}

export function resolveSeedanceProviderCapabilities(config: AiConfig, model: string): ProviderModelCapabilities {
    const capabilities = resolvePublishedVideoProviderCapabilities(config, model);
    if (!capabilities) throw new Error(`模型 ${model} 缺少后台发布的视频能力契约`);
    return capabilities;
}

function assertCompletePublishedVideoCapabilities(capabilities: ProviderModelCapabilities, model: string) {
    const arrayFieldsAreValid =
        isStringArray(capabilities.resolutions) &&
        isStringArray(capabilities.referenceVideoResolutions) &&
        isStringArray(capabilities.generatedAudioResolutions) &&
        isStringArray(capabilities.ratios) &&
        isStringArray(capabilities.qualities) &&
        isPositiveIntegerArray(capabilities.outputCounts) &&
        isPositiveIntegerArray(capabilities.durations) &&
        isStringArray(capabilities.inputVariants) &&
        isStringArray(capabilities.generationModes) &&
        isStringArray(capabilities.adaptiveRatioModes) &&
        isStringArray(capabilities.requiredAdaptiveRatioModes) &&
        isStringArray(capabilities.tools);
    const booleanFieldsAreValid = [
        capabilities.supportsSmartDuration,
        capabilities.supportsTextToVideo,
        capabilities.supportsImageToVideo,
        capabilities.supportsReferenceVideo,
        capabilities.supportsNativeAudio,
        capabilities.supportsDialogue,
        capabilities.supportsVoiceReference,
        capabilities.supportsLipSync,
        capabilities.supportsIndependentAudio,
        capabilities.supportsGeneratedAudio,
        capabilities.supportsGeneratedAudioWithReferenceVideo,
        capabilities.supportsAudioOnly,
        capabilities.requiresAdaptiveFrames,
        capabilities.supportsTokenUsageBilling,
    ].every((value) => typeof value === "boolean");
    if (!arrayFieldsAreValid || !booleanFieldsAreValid) {
        throw new Error(`模型 ${model} 的后台视频能力契约不完整`);
    }
    const durationsAreOrdered = capabilities.durations.every(
        (duration, index) => Number.isInteger(duration) && duration > 0 && (index === 0 || duration > capabilities.durations[index - 1]),
    );
    const countsAreValid = capabilities.outputCounts.every((count) => Number.isInteger(count) && count > 0);
    const inputVariantsAreValid = capabilities.inputVariants.every(
        (variant) => variant === "standard" || variant === "standard_audio" || variant === "reference_video",
    );
    const limitsAreValid = [
        capabilities.maxImages,
        capabilities.maxImagesWithVideo,
        capabilities.maxVideos,
        capabilities.maxAudios,
        capabilities.maxVideoDurationSeconds,
        capabilities.maxAudioDurationSeconds,
    ].every((value) => Number.isInteger(value) && value >= 0);
    const durationBoundsMatch =
        capabilities.durations.length > 0 &&
        capabilities.durationMin === capabilities.durations[0] &&
        capabilities.durationMax === capabilities.durations[capabilities.durations.length - 1];
    const supportedModes = parseVideoGenerationModes(capabilities.generationModes, model);
    const modesAreUnique = new Set(supportedModes).size === supportedModes.length;
    const modeFlagsAreConsistent =
        capabilities.supportsTextToVideo === supportedModes.includes("text") &&
        capabilities.supportsImageToVideo ===
            supportedModes.some((mode) => mode === "image" || mode === "first_last_frame" || mode === "image_reference") &&
        capabilities.supportsReferenceVideo === supportedModes.includes("omni_reference");
    const adaptiveModesAreValid = capabilities.adaptiveRatioModes.every(
        (mode) => isCanvasVideoGenerationMode(mode) && supportedModes.includes(mode),
    );
    const requiredAdaptiveModesAreValid = capabilities.requiredAdaptiveRatioModes.every(
        (mode) => isCanvasVideoGenerationMode(mode) && capabilities.adaptiveRatioModes.includes(mode),
    );
    const adaptiveContractIsValid =
        adaptiveModesAreValid &&
        requiredAdaptiveModesAreValid &&
        new Set(capabilities.adaptiveRatioModes).size === capabilities.adaptiveRatioModes.length &&
        new Set(capabilities.requiredAdaptiveRatioModes).size === capabilities.requiredAdaptiveRatioModes.length &&
        capabilities.ratios.includes("adaptive") === (capabilities.adaptiveRatioModes.length > 0) &&
        capabilities.requiresAdaptiveFrames === (capabilities.requiredAdaptiveRatioModes.length > 0) &&
        supportedModes.every(
            (mode) =>
                capabilities.requiredAdaptiveRatioModes.includes(mode) ||
                capabilities.adaptiveRatioModes.includes(mode) ||
                capabilities.ratios.some((ratio) => ratio !== "adaptive"),
        );
    const referenceVideoContractIsValid =
        !capabilities.supportsReferenceVideo ||
        (capabilities.maxVideos > 0 &&
            capabilities.referenceVideoResolutions.length > 0 &&
            valuesBelongToCapabilities(capabilities.referenceVideoResolutions, capabilities.resolutions));
    const voiceReferenceContractIsValid = !capabilities.supportsVoiceReference || capabilities.maxAudios > 0;
    const nativeAudioContractIsValid = !capabilities.supportsNativeAudio || capabilities.supportsGeneratedAudio;
    const generatedAudioResolutionsAreValid = valuesBelongToCapabilities(capabilities.generatedAudioResolutions, capabilities.resolutions);
    const referenceVideoAudioContractIsValid =
        !capabilities.supportsGeneratedAudioWithReferenceVideo ||
        (capabilities.supportsReferenceVideo && capabilities.supportsNativeAudio && capabilities.supportsGeneratedAudio);
    if (
        !capabilities.providerFamily ||
        !capabilities.resolutions.length ||
        !capabilities.ratios.length ||
        !capabilities.outputCounts.length ||
        !durationsAreOrdered ||
        !countsAreValid ||
        !inputVariantsAreValid ||
        !limitsAreValid ||
        !durationBoundsMatch ||
        !modesAreUnique ||
        !modeFlagsAreConsistent ||
        !adaptiveContractIsValid ||
        !referenceVideoContractIsValid ||
        !voiceReferenceContractIsValid ||
        !nativeAudioContractIsValid ||
        !generatedAudioResolutionsAreValid ||
        !referenceVideoAudioContractIsValid ||
        (!capabilities.supportsTextToVideo && !capabilities.supportsImageToVideo)
    ) {
        throw new Error(`模型 ${model} 的后台视频能力契约不完整`);
    }
}

function valuesBelongToCapabilities(values: readonly string[], capabilities: readonly string[]) {
    const normalizedCapabilities = new Set(capabilities.map((value) => value.trim().toLowerCase()));
    return values.every((value) => normalizedCapabilities.has(value.trim().toLowerCase()));
}

function isStringArray(value: unknown): value is string[] {
    return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function isPositiveIntegerArray(value: unknown): value is number[] {
    return Array.isArray(value) && value.every((item) => Number.isInteger(item) && item > 0);
}

function isCanvasVideoGenerationMode(value: string): value is CanvasVideoGenerationMode {
    return value === "text" || value === "image" || value === "first_last_frame" || value === "image_reference" || value === "omni_reference";
}

function parseVideoGenerationModes(values: readonly string[], model: string): CanvasVideoGenerationMode[] {
    return values.map((value) => {
        if (!isCanvasVideoGenerationMode(value)) {
            throw new Error(`模型 ${model} 的后台视频能力契约包含未知生成模式 ${value}`);
        }
        return value;
    });
}

export function normalizeVideoConfigForModel(config: AiConfig, generationMode?: CanvasVideoGenerationMode): AiConfig {
    const capabilities = resolveVideoModelCapabilities(config);
    if (generationMode && !capabilities.supportedGenerationModes.includes(generationMode)) {
        throw new Error(`模型 ${modelOptionName(config.model || config.videoModel)} 未开放生成模式 ${generationMode}`);
    }
    const resolutionOptions = videoResolutionsForMode(capabilities, generationMode);
    const normalizedResolution = normalizePublishedCapabilityResolution(config.vquality, resolutionOptions);
    const normalizedDuration = normalizePublishedCapabilityDuration(config.videoSeconds, capabilities);
    const normalizedRatio = normalizePublishedCapabilityRatio(config.size, capabilities);
    const ratioOptions = videoRatiosForMode(capabilities, generationMode);
    const supportedRatio = ratioOptions.some((option) => option.value === normalizedRatio) ? normalizedRatio : ratioOptions[0].value;
    const requestedCount = Math.max(1, Math.floor(Math.abs(Number(config.count)) || 1));
    const normalizedCount = capabilities.outputCounts.reduce((nearest, option) =>
        Math.abs(option - requestedCount) < Math.abs(nearest - requestedCount) ? option : nearest,
    );
    return {
        ...config,
        vquality: normalizedResolution,
        videoSeconds: String(normalizedDuration),
        size: supportedRatio,
        count: String(normalizedCount),
        videoGenerateAudio: videoSupportsGeneratedAudio(capabilities, generationMode, normalizedResolution) ? config.videoGenerateAudio : "false",
        videoSuperResolutionEnabled: capabilities.supportsSuperResolution ? config.videoSuperResolutionEnabled : "false",
        videoSuperResolutionFps: capabilities.supportsSuperResolution ? config.videoSuperResolutionFps : "",
    };
}

function normalizePublishedCapabilityResolution(value: string, options: readonly VideoParameterOption[]) {
    const requested = value.trim().toLowerCase();
    const exact = options.find((option) => option.value.toLowerCase() === requested);
    if (exact) return exact.value;
    const normalized = normalizeResolutionToken(requested);
    const normalizedOption = options.find((option) => option.value.toLowerCase() === normalized);
    if (normalizedOption) return normalizedOption.value;
    return options.find((option) => option.value.toLowerCase() === "720p")?.value ?? options[0].value;
}

function normalizePublishedCapabilityDuration(value: string, capabilities: VideoModelCapabilities) {
    const requested = Math.round(Number(value));
    const candidate = Number.isFinite(requested) ? requested : capabilities.durations[0];
    if (capabilities.customDurationRange) {
        return Math.max(capabilities.customDurationRange.min, Math.min(capabilities.customDurationRange.max, candidate));
    }
    return capabilities.durations.reduce((nearest, option) =>
        Math.abs(option - candidate) < Math.abs(nearest - candidate) ? option : nearest,
    );
}

function normalizePublishedCapabilityRatio(value: string, capabilities: VideoModelCapabilities) {
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
    return numericOptions.reduce((nearest, option) =>
        Math.abs(option.ratio - requestedRatio) < Math.abs(nearest.ratio - requestedRatio) ? option : nearest,
    ).value;
}

export function videoRatiosForMode(capabilities: VideoModelCapabilities, mode?: CanvasVideoGenerationMode) {
    if (mode && capabilities.requiredAdaptiveRatioModes.includes(mode)) {
        return capabilities.ratios.filter((option) => option.value === "adaptive");
    }
    if (mode && !capabilities.adaptiveRatioModes.includes(mode)) {
        return capabilities.ratios.filter((option) => option.value !== "adaptive");
    }
    return capabilities.ratios;
}

export function videoResolutionsForMode(capabilities: VideoModelCapabilities, mode?: CanvasVideoGenerationMode) {
    if (mode !== "omni_reference") return capabilities.resolutions;
    const allowed = new Set(capabilities.referenceVideoResolutions.map((resolution) => resolution.toLowerCase()));
    return capabilities.resolutions.filter((option) => allowed.has(option.value.toLowerCase()));
}

export function videoSupportsGeneratedAudio(capabilities: VideoModelCapabilities, mode?: CanvasVideoGenerationMode, resolution?: string) {
    if (!capabilities.supportsGeneratedAudio || !capabilities.supportsNativeAudio) return false;
    if (mode === "omni_reference" && !capabilities.supportsGeneratedAudioWithReferenceVideo) return false;
    if (!resolution || capabilities.generatedAudioResolutions.length === 0) return true;
    const requested = resolution.trim().toLowerCase();
    return capabilities.generatedAudioResolutions.some((candidate) => candidate.trim().toLowerCase() === requested);
}

export function videoModelMetadataPatch(config: AiConfig, model: string, generationMode?: CanvasVideoGenerationMode) {
    const nextConfig = { ...config, model };
    const capabilities = resolveVideoModelCapabilities(nextConfig);
    const effectiveMode =
        generationMode && capabilities.supportedGenerationModes.includes(generationMode)
            ? generationMode
            : capabilities.supportedGenerationModes[0];
    const normalized = normalizeVideoConfigForModel(nextConfig, effectiveMode);
    const requestConfig = resolveModelRequestConfig(config, model);
    return {
        channelId: requestConfig.channelId,
        model: requestConfig.model,
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
