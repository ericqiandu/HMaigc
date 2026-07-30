import { useEffect, useRef, useState } from "react";

import { previewChannelVoice } from "@/services/api/voices";
import type { ChannelVoice } from "@/stores/use-config-store";

export function useChannelVoicePreview(channelId: string, model: string) {
    const [loadingVoiceId, setLoadingVoiceId] = useState("");
    const [playingVoiceId, setPlayingVoiceId] = useState("");
    const [error, setError] = useState("");
    const audioRef = useRef<HTMLAudioElement | null>(null);
    const requestRef = useRef<AbortController | null>(null);
    const cacheRef = useRef(new Map<string, string>());

    function stop() {
        requestRef.current?.abort();
        requestRef.current = null;
        if (audioRef.current) {
            audioRef.current.pause();
            audioRef.current.currentTime = 0;
            audioRef.current = null;
        }
        setLoadingVoiceId("");
        setPlayingVoiceId("");
    }

    useEffect(
        () => () => {
            requestRef.current?.abort();
            audioRef.current?.pause();
            audioRef.current = null;
        },
        [],
    );

    async function toggle(voice: ChannelVoice) {
        if (playingVoiceId === voice.id) {
            stop();
            return;
        }
        stop();
        setError("");
        setLoadingVoiceId(voice.id);
        const controller = new AbortController();
        requestRef.current = controller;
        try {
            const cacheKey = `${channelId}\u0000${model}\u0000${voice.id}`;
            let audioDataUrl = cacheRef.current.get(cacheKey);
            if (!audioDataUrl) {
                const result = await previewChannelVoice(channelId, voice.id, model, controller.signal);
                audioDataUrl = result.preview.audioDataUrl;
                cacheRef.current.set(cacheKey, audioDataUrl);
            }
            if (controller.signal.aborted) return;
            const audio = new Audio(audioDataUrl);
            audioRef.current = audio;
            audio.onended = () => {
                audioRef.current = null;
                setPlayingVoiceId("");
            };
            audio.onerror = () => {
                audioRef.current = null;
                setPlayingVoiceId("");
                setError(`${voice.displayName} 的试听音频无法播放`);
            };
            await audio.play();
            setPlayingVoiceId(voice.id);
        } catch (previewError) {
            if (!controller.signal.aborted) setError(previewError instanceof Error ? previewError.message : "音色试听失败");
        } finally {
            if (requestRef.current === controller) requestRef.current = null;
            setLoadingVoiceId((current) => (current === voice.id ? "" : current));
        }
    }

    return {
        loadingVoiceId,
        playingVoiceId,
        error,
        setError,
        stop,
        toggle,
    };
}
