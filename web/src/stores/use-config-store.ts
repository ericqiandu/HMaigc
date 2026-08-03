import { useMemo } from "react";
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { nanoid } from "nanoid";

import { scopedLocalStorage } from "@/lib/user-scope";
import { normalizeVideoDuration, normalizeVideoResolution } from "@/lib/video-generation-options";
import type { ModelBrandKey } from "@/lib/model-brands";

export type ApiCallFormat = "openai" | "gemini";
export type ChannelInterfaceType = "chat-completion" | "openai-response" | "openai-image" | "apimart-image" | "newapi" | "xai-video" | "ai-open-platform-video" | "ai-open-platform-video-volcengine" | "minimax-speech" | "minimax-video" | "kling-video";

export type ChannelVoice = {
    id: string;
    voiceKey: string;
    displayName: string;
    description: string;
    language: string;
    kind: "system" | "voice_cloning" | "voice_generation";
    accessPolicy: "authenticated" | "member";
    accessible: boolean;
    compatibleModels: string[];
    providerStatus: "active" | "pending_activation" | "creating" | "uncertain" | "failed" | "missing" | "deleted";
    enabled: boolean;
    ownedByCurrentUser: boolean;
    favorited: boolean;
    ownerUserId?: string;
    lastError?: string;
};

export type ModelChannel = {
    id: string;
    name: string;
    baseUrl: string;
    apiKey: string;
    apiFormat: ApiCallFormat;
    interfaceType?: ChannelInterfaceType;
    models: string[];
    scope?: "system" | "user";
    enabled?: boolean;
    hasApiKey?: boolean;
    concurrencyLimit?: number;
    modelCosts?: Array<{
        model: string;
        displayName?: string;
        marketingCopy: string;
        promotionBadge: string;
        brandKey: ModelBrandKey;
        accessPolicy: "authenticated" | "member";
        accessible: boolean;
        capability: ModelCapability;
        billingMode: "fixed_request" | "per_second";
        priceStrategy: "flat" | "image_resolution" | "video_resolution";
        unitPriceMicrocredits: number;
        priceTiers: Array<{
            resolution: string;
            unitPriceMicrocredits: number;
        }>;
    }>;
    voices?: ChannelVoice[];
};

export type AiConfig = {
    channelMode: "remote" | "local";
    baseUrl: string;
    apiKey: string;
    apiFormat: ApiCallFormat;
    channels: ModelChannel[];
    model: string;
    imageModel: string;
    videoModel: string;
    textModel: string;
    audioModel: string;
    audioVoice: string;
    audioFormat: string;
    audioSpeed: string;
    audioVolume: string;
    audioPitch: string;
    audioEmotion: string;
    audioLanguageBoost: string;
    audioSampleRate: string;
    audioBitrate: string;
    audioChannel: string;
    audioInstructions: string;
    videoSeconds: string;
    vquality: string;
    videoGenerateAudio: string;
    videoWatermark: string;
    videoSuperResolutionEnabled: string;
    videoSuperResolutionResolution: string;
    videoSuperResolutionScene: string;
    videoSuperResolutionVersion: string;
    videoSuperResolutionFps: string;
    systemPrompt: string;
    models: string[];
    imageModels: string[];
    videoModels: string[];
    textModels: string[];
    audioModels: string[];
    quality: string;
    size: string;
    transparentBackground: string;
    count: string;
    canvasImageCount: string;
};

export const CONFIG_STORE_KEY = "open_ai_canvas:ai_config_store";
export type ModelCapability = "image" | "video" | "text" | "audio";
const CHANNEL_MODEL_SEPARATOR = "::";
const OPENAI_BASE_URL = "https://api.openai.com";
const GEMINI_BASE_URL = "https://generativelanguage.googleapis.com";

export const defaultConfig: AiConfig = {
    channelMode: "local",
    baseUrl: "",
    apiKey: "",
    apiFormat: "openai",
    channels: [],
    model: "",
    imageModel: "",
    videoModel: "",
    textModel: "",
    audioModel: "",
    audioVoice: "",
    audioFormat: "mp3",
    audioSpeed: "1",
    audioVolume: "1",
    audioPitch: "0",
    audioEmotion: "",
    audioLanguageBoost: "auto",
    audioSampleRate: "32000",
    audioBitrate: "128000",
    audioChannel: "1",
    audioInstructions: "",
    videoSeconds: "6",
    vquality: "720",
    videoGenerateAudio: "true",
    videoWatermark: "false",
    videoSuperResolutionEnabled: "false",
    videoSuperResolutionResolution: "1080p",
    videoSuperResolutionScene: "short_series",
    videoSuperResolutionVersion: "standard",
    videoSuperResolutionFps: "",
    systemPrompt: "",
    models: [],
    imageModels: [],
    videoModels: [],
    textModels: [],
    audioModels: [],
    quality: "auto",
    size: "1:1",
    transparentBackground: "false",
    count: "1",
    canvasImageCount: "1",
};

type ConfigStore = {
    config: AiConfig;
    updateConfig: <K extends keyof AiConfig>(key: K, value: AiConfig[K]) => void;
    replaceConfig: (config: AiConfig) => void;
    mergeSystemChannels: (channels: ModelChannel[]) => void;
    isAiConfigReady: (config: AiConfig, model: string) => boolean;
};

export type ConfigStoreSnapshot = {
    config?: Partial<AiConfig>;
};

function isVideoModelName(model: string) {
    const value = modelOptionName(model).toLowerCase();
    return value.includes("seedance") || value.includes("video") || value.includes("sora") || value.includes("veo") || value.includes("kling") || value.includes("wan") || value.includes("hailuo");
}

function isImageModelName(model: string) {
    const value = modelOptionName(model).toLowerCase();
    return (
        !isVideoModelName(model) &&
        !isAudioModelName(model) &&
        (value.includes("seedream") ||
            value.includes("gpt-image") ||
            value.includes("image") ||
            value.includes("dall-e") ||
            value.includes("dalle") ||
            value.includes("imagen") ||
            value.includes("flux") ||
            value.includes("sdxl") ||
            value.includes("stable-diffusion") ||
            value.includes("midjourney"))
    );
}

function isAudioModelName(model: string) {
    const value = modelOptionName(model).toLowerCase();
    return value.includes("audio") || value.includes("tts") || value.includes("speech") || value.includes("voice") || value.includes("music") || value.includes("sound");
}

function isTextModelName(model: string) {
    return !isImageModelName(model) && !isVideoModelName(model) && !isAudioModelName(model);
}

export function modelMatchesCapability(model: string, capability?: ModelCapability) {
    if (!capability) return true;
    if (capability === "image") return isImageModelName(model);
    if (capability === "video") return isVideoModelName(model);
    if (capability === "audio") return isAudioModelName(model);
    return isTextModelName(model);
}

export function filterModelsByCapability(models: string[], capability?: ModelCapability, channels?: ModelChannel[]) {
    if (!capability) return models;
    return models.filter((model) => {
        const decoded = decodeChannelModel(model);
        const channel = decoded ? channels?.find((item) => item.id === decoded.channelId) : undefined;
        const configuredCapability = channel?.modelCosts?.find((item) => item.model === decoded?.model)?.capability;
        if (configuredCapability) return configuredCapability === capability;
        const channelCapability = capabilityForChannelInterface(channel?.interfaceType);
        return channelCapability ? channelCapability === capability : modelMatchesCapability(model, capability);
    });
}

export function selectableModelsByCapability(config: AiConfig, capability?: ModelCapability) {
    const accessibleModels = config.models.filter((model) => isModelAccessible(config, model));
    if (!capability) return accessibleModels;
    return filterModelsByCapability(accessibleModels, capability, config.channels);
}

export function catalogModelsByCapability(config: AiConfig, capability?: ModelCapability) {
    const models = modelOptionsFromChannels(config.channels);
    return capability ? filterModelsByCapability(models, capability, config.channels) : models;
}

export function isModelAccessible(config: AiConfig, value: string) {
    const channel = resolveModelChannel(config, value);
    if (channel.scope !== "system") return true;
    const entry = channel.modelCosts?.find((item) => item.model === modelOptionName(value));
    return entry?.accessible === true;
}

export function configuredModelMatchesCapability(config: AiConfig, model: string, capability?: ModelCapability) {
    const normalized = normalizeModelOptionValue(model, config.channels);
    if (!normalized || !config.models.includes(normalized)) return false;
    return capability ? selectableModelsByCapability(config, capability).includes(normalized) : true;
}

function isAiConfigReady(config: AiConfig, model: string) {
    const channel = resolveModelChannel(config, model);
    return Boolean(model.trim() && channel.baseUrl.trim() && channel.apiKey.trim());
}

export const useConfigStore = create<ConfigStore>()(
    persist(
        (set) => ({
            config: defaultConfig,
            updateConfig: (key, value) =>
                set((state) => ({
                    config: {
                        ...state.config,
                        [key]: value,
                    },
                })),
            replaceConfig: (config) => set({ config }),
            mergeSystemChannels: (channels) =>
                set((state) => {
                    const systemChannels = channels.map((channel, index) =>
                        createModelChannel({
                            ...channel,
                            id: channel.id || `system-${index + 1}`,
                            name: channel.name || `系统渠道 ${index + 1}`,
                            scope: "system",
                            apiKey: channel.apiKey || "system",
                        }),
                    );
                    return normalizeConfigSnapshot({ config: { ...state.config, channels: systemChannels } });
                }),
            isAiConfigReady: (config, model) => isAiConfigReady(config, model),
        }),
        {
            name: CONFIG_STORE_KEY,
            storage: createJSONStorage(() => scopedLocalStorage),
            partialize: (state) => ({ config: state.config }),
            merge: (persisted, current) => {
                const persistedState = (persisted || {}) as Partial<ConfigStore>;
                return {
                    ...current,
                    ...normalizeConfigSnapshot({ config: persistedState.config }),
                };
            },
        },
    ),
);

export function normalizeConfigSnapshot(snapshot: ConfigStoreSnapshot) {
    const persistedConfig = (snapshot.config || {}) as Partial<AiConfig>;
    const config = { ...defaultConfig, ...persistedConfig };
    const channels = normalizeChannels(config);
    const catalogModels = modelOptionsFromChannels(channels);
    const modelConfig = { ...config, channels, models: catalogModels };
    const models = catalogModels.filter((model) => isModelAccessible(modelConfig, model));
    const imageModels = filterModelsByCapability(models, "image", channels);
    const videoModels = filterModelsByCapability(models, "video", channels);
    const textModels = filterModelsByCapability(models, "text", channels);
    const audioModels = filterModelsByCapability(models, "audio", channels);
    const model = normalizeSelectedModel(config.model || config.imageModel || config.textModel, channels, models);
    return {
        config: {
            ...config,
            channelMode: "local" as const,
            apiFormat: normalizeApiFormat(config.apiFormat),
            channels,
            models,
            model,
            imageModel: normalizeSelectedModel(config.imageModel || model, channels, imageModels),
            videoModel: normalizeSelectedModel(config.videoModel, channels, videoModels),
            textModel: normalizeSelectedModel(config.textModel || model, channels, textModels),
            audioModel: normalizeSelectedModel(config.audioModel || defaultConfig.audioModel, channels, audioModels),
            audioVoice: config.audioVoice || defaultConfig.audioVoice,
            audioFormat: config.audioFormat || defaultConfig.audioFormat,
            audioSpeed: config.audioSpeed || defaultConfig.audioSpeed,
            audioVolume: config.audioVolume || defaultConfig.audioVolume,
            audioPitch: config.audioPitch || defaultConfig.audioPitch,
            audioEmotion: config.audioEmotion || defaultConfig.audioEmotion,
            audioLanguageBoost: config.audioLanguageBoost || defaultConfig.audioLanguageBoost,
            audioSampleRate: config.audioSampleRate || defaultConfig.audioSampleRate,
            audioBitrate: config.audioBitrate || defaultConfig.audioBitrate,
            audioChannel: config.audioChannel || defaultConfig.audioChannel,
            audioInstructions: config.audioInstructions || "",
            videoSeconds: normalizeVideoDuration(config.videoSeconds),
            vquality: normalizeVideoResolution(config.vquality),
            videoGenerateAudio: config.videoGenerateAudio || "true",
            videoWatermark: config.videoWatermark || "false",
            videoSuperResolutionEnabled: config.videoSuperResolutionEnabled === "true" ? "true" : "false",
            videoSuperResolutionResolution: config.videoSuperResolutionResolution || defaultConfig.videoSuperResolutionResolution,
            videoSuperResolutionScene: config.videoSuperResolutionScene || defaultConfig.videoSuperResolutionScene,
            videoSuperResolutionVersion: config.videoSuperResolutionVersion || defaultConfig.videoSuperResolutionVersion,
            videoSuperResolutionFps: config.videoSuperResolutionFps || "",
            transparentBackground: config.transparentBackground === "true" ? "true" : "false",
            canvasImageCount: config.canvasImageCount || defaultConfig.canvasImageCount,
            imageModels,
            videoModels,
            textModels,
            audioModels,
        },
    };
}

function normalizeSelectedModel(value: string, channels: ModelChannel[], options: string[]) {
    const model = normalizeModelOptionValue(value, channels);
    return model && options.includes(model) ? model : options[0] || "";
}

export function useEffectiveConfig() {
    const config = useConfigStore((state) => state.config);
    return useMemo(() => ({ ...config, channelMode: "local" as const }), [config]);
}

export function createModelChannel(channel?: Partial<ModelChannel>): ModelChannel {
    const apiFormat = normalizeApiFormat(channel?.apiFormat);
    const interfaceType = normalizeChannelInterfaceType(channel?.interfaceType);
    const providedBaseUrl = channel?.baseUrl?.trim();
    return {
        id: channel?.id?.trim() || nanoid(),
        name: channel?.name?.trim() || "新渠道",
        baseUrl: providedBaseUrl || (interfaceType ? defaultBaseUrlForChannelInterface(interfaceType) : defaultBaseUrlForApiFormat(apiFormat)),
        apiKey: channel?.apiKey || "",
        apiFormat,
        interfaceType,
        models: uniqueRawModels(channel?.models || []),
        scope: channel?.scope === "system" ? "system" : "user",
        enabled: channel?.enabled !== false,
        hasApiKey: channel?.hasApiKey,
        modelCosts: channel?.modelCosts,
        voices: channel?.voices,
    };
}

export function encodeChannelModel(channelId: string, model: string) {
    return `${channelId}${CHANNEL_MODEL_SEPARATOR}${model.trim()}`;
}

export function isChannelModelValue(value: string) {
    return value.includes(CHANNEL_MODEL_SEPARATOR);
}

export function decodeChannelModel(value: string) {
    const index = value.indexOf(CHANNEL_MODEL_SEPARATOR);
    if (index < 0) return null;
    return { channelId: value.slice(0, index), model: value.slice(index + CHANNEL_MODEL_SEPARATOR.length) };
}

export function modelOptionName(value: string) {
    return decodeChannelModel(value)?.model || value;
}

export function modelDisplayName(config: AiConfig, value: string) {
    const model = modelOptionName(value);
    const channel = resolveModelChannel(config, value);
    return channel.modelCosts?.find((item) => item.model === model)?.displayName?.trim() || model;
}

export function modelOptionsFromChannels(channels: ModelChannel[]) {
    return uniqueModelOptions(
        channels.flatMap((channel) =>
            channel.models
                .map(normalizeRawModelName)
                .filter(Boolean)
                .filter((model) => channel.scope !== "system" || hasSystemModelPrice(channel, model))
                .map((model) => encodeChannelModel(channel.id, model)),
        ),
    );
}

export function hasSystemModelPrice(channel: ModelChannel, model: string) {
    if (channel.scope !== "system") return true;
    return (
        channel.modelCosts?.some((item) => {
            if (item.model !== model) return false;
            if (item.priceStrategy === "flat") {
                return Number.isFinite(item.unitPriceMicrocredits) && item.unitPriceMicrocredits > 0;
            }
            if (!Array.isArray(item.priceTiers)) return false;
            if (item.priceStrategy === "video_resolution") {
                return item.priceTiers.some((tier) => Number.isFinite(tier.unitPriceMicrocredits) && tier.unitPriceMicrocredits > 0);
            }
            const requiredResolutions = ["1K", "2K", "4K"] as const;
            return requiredResolutions.every((resolution) => {
                const tier = item.priceTiers.find((candidate) => candidate.resolution === resolution);
                return tier !== undefined && Number.isFinite(tier.unitPriceMicrocredits) && tier.unitPriceMicrocredits > 0;
            });
        }) === true
    );
}

export function normalizeModelOptionValue(value: unknown, channels: ModelChannel[]) {
    const model = typeof value === "string" ? value.trim() : "";
    if (!normalizeRawModelName(model)) return "";
    const decoded = decodeChannelModel(model);
    if (decoded) {
        const channel = channels.find((item) => item.id === decoded.channelId);
        return channel && channel.models.includes(decoded.model) ? model : "";
    }
    const channel = channels.find((item) => item.models.includes(model)) || channels[0];
    return channel && channel.models.includes(model) ? encodeChannelModel(channel.id, model) : "";
}

export function resolveModelChannel(config: AiConfig, value: string) {
    const decoded = decodeChannelModel(value);
    const model = decoded?.model || value;
    const matched = decoded ? config.channels.find((channel) => channel.id === decoded.channelId) : config.channels.find((channel) => channel.models.includes(model));
    return matched || config.channels[0] || createModelChannel({ id: "system-unavailable", name: "系统渠道未配置", baseUrl: "", apiKey: "", models: [], scope: "system", enabled: false });
}

export function resolveModelRequestConfig(config: AiConfig, value: string) {
    const channel = resolveModelChannel(config, value);
    return {
        ...config,
        model: modelOptionName(value || config.model),
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
        interfaceType: channel.interfaceType,
        channelId: channel.scope === "system" ? channel.id : "",
    };
}

function normalizeChannels(config: AiConfig) {
    const persistedChannels = Array.isArray(config.channels) ? config.channels : [];
    return persistedChannels
        .filter((channel) => channel.scope === "system")
        .map((channel, index) =>
            createModelChannel({
                ...channel,
                id: channel.id || (index === 0 ? "default" : `channel-${index + 1}`),
                name: channel.name || (index === 0 ? "默认渠道" : `渠道 ${index + 1}`),
                models: uniqueRawModels(channel.models || []),
            }),
        )
        .map((channel) => ({ ...channel, models: uniqueRawModels(channel.models) }));
}

export function defaultBaseUrlForApiFormat(apiFormat: ApiCallFormat) {
    return apiFormat === "gemini" ? GEMINI_BASE_URL : OPENAI_BASE_URL;
}

export function defaultBaseUrlForChannelInterface(interfaceType?: ChannelInterfaceType) {
    if (interfaceType === "apimart-image") return "https://api.apimart.ai/v1";
    if (interfaceType === "minimax-speech") return "https://api.minimaxi.com/v1";
    if (interfaceType === "minimax-video") return "https://api.minimaxi.com";
    if (interfaceType === "kling-video") return "https://api.klingai.com";
    if (interfaceType === "newapi" || interfaceType === "xai-video" || interfaceType === "ai-open-platform-video" || interfaceType === "ai-open-platform-video-volcengine") return "";
    return OPENAI_BASE_URL;
}

function capabilityForChannelInterface(interfaceType?: ChannelInterfaceType): ModelCapability | undefined {
    if (interfaceType === "chat-completion" || interfaceType === "openai-response") return "text";
    if (interfaceType === "openai-image" || interfaceType === "apimart-image") return "image";
    if (interfaceType === "newapi" || interfaceType === "xai-video" || interfaceType === "ai-open-platform-video" || interfaceType === "ai-open-platform-video-volcengine" || interfaceType === "minimax-video" || interfaceType === "kling-video")
        return "video";
    if (interfaceType === "minimax-speech") return "audio";
    return undefined;
}

function normalizeApiFormat(apiFormat: unknown): ApiCallFormat {
    return apiFormat === "gemini" ? "gemini" : "openai";
}

function normalizeChannelInterfaceType(value: unknown): ChannelInterfaceType | undefined {
    return value === "chat-completion" ||
        value === "openai-response" ||
        value === "openai-image" ||
        value === "apimart-image" ||
        value === "newapi" ||
        value === "xai-video" ||
        value === "ai-open-platform-video" ||
        value === "ai-open-platform-video-volcengine" ||
        value === "minimax-speech" ||
        value === "minimax-video" ||
        value === "kling-video"
        ? value
        : undefined;
}

function uniqueRawModels(models: string[]) {
    return Array.from(new Set((models || []).map(normalizeRawModelName).filter(Boolean)));
}

function uniqueModelOptions(models: string[]) {
    return Array.from(
        new Set(
            (models || [])
                .filter((model): model is string => typeof model === "string")
                .map((model) => model.trim())
                .filter(Boolean),
        ),
    );
}

function normalizeRawModelName(value: unknown) {
    if (typeof value !== "string") return "";
    const model = modelOptionName(value).trim();
    return model && model !== "undefined" && model !== "null" ? model : "";
}

export function buildApiUrl(baseUrl: string, path: string) {
    let normalizedBaseUrl = resolveBackendApiUrl(baseUrl).replace(/\/+$/, "");
    normalizedBaseUrl = normalizeArkPlanBaseUrl(normalizedBaseUrl);
    const lowerBaseUrl = normalizedBaseUrl.toLowerCase();
    const apiBaseUrl = isSystemProxyBaseUrl(normalizedBaseUrl) || lowerBaseUrl.endsWith("/v1") || lowerBaseUrl.endsWith("/api/v3") || lowerBaseUrl.endsWith("/api/plan/v3") ? normalizedBaseUrl : `${normalizedBaseUrl}/v1`;
    return `${apiBaseUrl}${path}`;
}

export function resolveBackendApiUrl(value: string) {
    const url = value.trim();
    if (!url.startsWith("/api/")) return url;
    const backendBaseUrl = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api")
        .trim()
        .replace(/\/+$/, "");
    return backendBaseUrl === "/api" ? url : `${backendBaseUrl}${url.slice("/api".length)}`;
}

export function isSystemProxyBaseUrl(baseUrl: string) {
    const marker = "/api/ai/system/";
    const index = baseUrl.toLowerCase().indexOf(marker);
    if (index < 0) return false;
    const channelId = baseUrl.slice(index + marker.length);
    return Boolean(channelId && !channelId.includes("/") && !channelId.includes("?") && !channelId.includes("#"));
}

function normalizeArkPlanBaseUrl(baseUrl: string) {
    try {
        const url = new URL(baseUrl);
        const path = url.pathname.replace(/\/+$/, "");
        const lowerPath = path.toLowerCase();
        const arkPlanIndex = lowerPath.indexOf("/api/plan/v3");
        if (arkPlanIndex < 0) return baseUrl;
        const end = arkPlanIndex + "/api/plan/v3".length;
        if (lowerPath.length !== end && lowerPath[end] !== "/") return baseUrl;
        url.pathname = path.slice(0, end);
        url.search = "";
        url.hash = "";
        return url.toString().replace(/\/+$/, "");
    } catch {
        return baseUrl;
    }
}
