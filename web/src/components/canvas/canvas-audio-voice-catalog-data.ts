import type { ChannelVoice } from "@/stores/use-config-store";

export type CanvasAudioVoiceCatalogLoader = (
    channelId: string,
    signal?: AbortSignal,
) => Promise<{ voices: ChannelVoice[] }>;

export function filterCanvasAudioVoiceCatalog(voices: ChannelVoice[], selectedModel: string): ChannelVoice[] {
    return voices.filter(
        (voice) =>
            voice.enabled &&
            (voice.providerStatus === "active" || voice.providerStatus === "pending_activation") &&
            (voice.compatibleModels.length === 0 || voice.compatibleModels.includes(selectedModel)),
    );
}

export async function loadCanvasAudioVoiceCatalog(
    channelId: string,
    selectedModel: string,
    loader: CanvasAudioVoiceCatalogLoader,
    signal?: AbortSignal,
): Promise<ChannelVoice[]> {
    if (!channelId.trim()) {
        throw new Error("当前音频模型未绑定系统渠道");
    }

    const response = await loader(channelId, signal);
    return filterCanvasAudioVoiceCatalog(response.voices, selectedModel);
}
