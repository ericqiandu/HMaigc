import type { resolveModelRequestConfig } from "@/stores/use-config-store";

type ResolvedModelRequestConfig = ReturnType<typeof resolveModelRequestConfig>;

export type SystemProviderTaskConfig = Pick<
    ResolvedModelRequestConfig,
    | "channelId"
    | "model"
    | "size"
    | "quality"
    | "transparentBackground"
    | "count"
    | "videoSeconds"
    | "vquality"
    | "videoGenerateAudio"
    | "videoWatermark"
    | "videoSuperResolutionEnabled"
    | "videoSuperResolutionResolution"
    | "videoSuperResolutionScene"
    | "videoSuperResolutionVersion"
    | "videoSuperResolutionFps"
    | "audioVoice"
    | "audioFormat"
    | "audioSpeed"
    | "audioVolume"
    | "audioPitch"
    | "audioEmotion"
    | "audioLanguageBoost"
    | "audioSampleRate"
    | "audioBitrate"
    | "audioChannel"
    | "audioInstructions"
>;

/**
 * User-facing generation requests may select an enabled system channel/model,
 * but provider credentials and upstream endpoints are resolved by the backend.
 */
export function systemProviderTaskConfig(config: ResolvedModelRequestConfig): SystemProviderTaskConfig {
    const channelId = config.channelId.trim();
    const model = config.model.trim();
    const channel = config.channels.find((item) => item.id === channelId);
    const isAuthorizedSystemModel = channel?.scope === "system" && channel.enabled !== false && channel.models.some((candidate) => candidate.trim() === model);
    if (!channelId || !model || !isAuthorizedSystemModel) {
        throw new Error("当前模型未绑定后台系统渠道，请联系管理员检查模型配置");
    }
    return {
        channelId,
        model,
        size: config.size,
        quality: config.quality,
        transparentBackground: config.transparentBackground,
        count: config.count,
        videoSeconds: config.videoSeconds,
        vquality: config.vquality,
        videoGenerateAudio: config.videoGenerateAudio,
        videoWatermark: config.videoWatermark,
        videoSuperResolutionEnabled: config.videoSuperResolutionEnabled,
        videoSuperResolutionResolution: config.videoSuperResolutionResolution,
        videoSuperResolutionScene: config.videoSuperResolutionScene,
        videoSuperResolutionVersion: config.videoSuperResolutionVersion,
        videoSuperResolutionFps: config.videoSuperResolutionFps,
        audioVoice: config.audioVoice,
        audioFormat: config.audioFormat,
        audioSpeed: config.audioSpeed,
        audioVolume: config.audioVolume,
        audioPitch: config.audioPitch,
        audioEmotion: config.audioEmotion,
        audioLanguageBoost: config.audioLanguageBoost,
        audioSampleRate: config.audioSampleRate,
        audioBitrate: config.audioBitrate,
        audioChannel: config.audioChannel,
        audioInstructions: config.audioInstructions,
    };
}
