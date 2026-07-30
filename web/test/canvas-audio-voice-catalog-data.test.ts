import { describe, expect, test } from "bun:test";

import { loadCanvasAudioVoiceCatalog } from "@/components/canvas/canvas-audio-voice-catalog-data";
import type { ChannelVoice } from "@/stores/use-config-store";

function voice(overrides: Partial<ChannelVoice> = {}): ChannelVoice {
    return {
        id: "voice-1",
        voiceKey: "voice-key",
        displayName: "测试音色",
        description: "",
        language: "zh-CN",
        kind: "system",
        accessPolicy: "authenticated",
        accessible: true,
        compatibleModels: [],
        providerStatus: "active",
        enabled: true,
        ownedByCurrentUser: false,
        favorited: false,
        ...overrides,
    };
}

describe("loadCanvasAudioVoiceCatalog", () => {
    test("loads the authoritative channel catalog and keeps voices available to the selected model", async () => {
        const calls: string[] = [];
        const result = await loadCanvasAudioVoiceCatalog("channel-1", "speech-2.8-turbo", async (channelId) => {
            calls.push(channelId);
            return {
                voices: [
                    voice(),
                    voice({ id: "voice-2", voiceKey: "disabled", enabled: false }),
                    voice({ id: "voice-3", voiceKey: "wrong-model", compatibleModels: ["speech-2.8-hd"] }),
                    voice({ id: "voice-4", voiceKey: "failed", providerStatus: "failed" }),
                    voice({ id: "voice-5", voiceKey: "pending", providerStatus: "pending_activation" }),
                ],
            };
        });

        expect(calls).toEqual(["channel-1"]);
        expect(result.map((item) => item.voiceKey)).toEqual(["voice-key", "pending"]);
    });

    test("rejects a model without a bound system channel instead of showing an empty catalog", async () => {
        await expect(loadCanvasAudioVoiceCatalog("", "speech-2.8-turbo", async () => ({ voices: [] }))).rejects.toThrow("当前音频模型未绑定系统渠道");
    });
});
