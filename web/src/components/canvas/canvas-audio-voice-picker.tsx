import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { AudioLines } from "lucide-react";
import { Modal } from "antd";

import { loadCanvasAudioVoiceCatalog } from "@/components/canvas/canvas-audio-voice-catalog-data";
import { CanvasAudioVoiceCatalog } from "@/components/canvas/canvas-audio-voice-catalog";
import { CanvasAudioVoiceCloneDialog } from "@/components/canvas/canvas-audio-voice-clone-dialog";
import { useChannelVoicePreview } from "@/components/canvas/use-channel-voice-preview";
import { normalizeAudioVoiceValue } from "@/lib/audio-generation";
import { canvasThemes } from "@/lib/canvas-theme";
import { refreshSystemChannels } from "@/lib/user-session";
import { listUserChannelVoices, setChannelVoiceFavorite } from "@/services/api/voices";
import { modelOptionName, resolveModelChannel, type AiConfig, type ChannelVoice } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";

import "./canvas-audio-voice-picker.css";

type CanvasAudioVoicePickerProps = {
    config: AiConfig;
    value: string;
    onChange: (voice: string) => void;
    className?: string;
};

export function CanvasAudioVoicePicker({ config, value, onChange, className = "" }: CanvasAudioVoicePickerProps) {
    const themeName = useThemeStore((state) => state.theme);
    const theme = canvasThemes[themeName];
    const [open, setOpen] = useState(false);
    const [cloneOpen, setCloneOpen] = useState(false);
    const [catalogVoices, setCatalogVoices] = useState<ChannelVoice[]>([]);
    const [catalogLoading, setCatalogLoading] = useState(false);
    const [favoritingVoiceId, setFavoritingVoiceId] = useState("");
    const [operationError, setOperationError] = useState("");
    const selectedVoice = normalizeAudioVoiceValue(value);
    const selectedModel = modelOptionName(config.model);
    const channel = resolveModelChannel(config, config.model);
    const configuredVoices = useMemo(
        () => (channel.voices || []).filter((voice) => voice.enabled && (voice.providerStatus === "active" || voice.providerStatus === "pending_activation") && (voice.compatibleModels.length === 0 || voice.compatibleModels.includes(selectedModel))),
        [channel.voices, selectedModel],
    );
    const preview = useChannelVoicePreview(channel.id, selectedModel);
    const selectedVoiceLabel = catalogVoices.find((voice) => voice.voiceKey === selectedVoice)?.displayName || configuredVoices.find((voice) => voice.voiceKey === selectedVoice)?.displayName || "选择音色";
    const cloneAvailable = channel.interfaceType === "minimax-speech" && Boolean(channel.id);

    useEffect(() => {
        if (!open) return;

        const controller = new AbortController();
        setCatalogLoading(true);
        setCatalogVoices([]);
        setOperationError("");

        void loadCanvasAudioVoiceCatalog(channel.id, selectedModel, listUserChannelVoices, controller.signal)
            .then((voices) => {
                if (!controller.signal.aborted) setCatalogVoices(voices);
            })
            .catch((loadError: unknown) => {
                if (controller.signal.aborted) return;
                setOperationError(loadError instanceof Error ? `读取音色目录失败：${loadError.message}` : "读取音色目录失败");
            })
            .finally(() => {
                if (!controller.signal.aborted) setCatalogLoading(false);
            });

        return () => controller.abort();
    }, [channel.id, open, selectedModel]);

    const dialogStyle = {
        "--audio-voice-surface": theme.spatial.elevated,
        "--audio-voice-fill": theme.node.fill,
        "--audio-voice-text": theme.node.text,
        "--audio-voice-muted": theme.node.muted,
        "--audio-voice-stroke": theme.node.stroke,
        "--audio-voice-accent": themeName === "dark" ? "#2997ff" : "#0071e3",
    } as CSSProperties;

    function close() {
        preview.stop();
        setOpen(false);
        setOperationError("");
        preview.setError("");
    }

    function selectVoice(voice: ChannelVoice) {
        onChange(voice.voiceKey);
        close();
    }

    async function toggleFavorite(voice: ChannelVoice) {
        setFavoritingVoiceId(voice.id);
        setOperationError("");
        try {
            const result = await setChannelVoiceFavorite(channel.id, voice.id, !voice.favorited);
            setCatalogVoices((current) => current.map((item) => (item.id === voice.id ? result.voice : item)));
            try {
                await refreshSystemChannels();
            } catch (refreshError) {
                setOperationError(refreshError instanceof Error ? `收藏已保存，但刷新音色目录失败：${refreshError.message}` : "收藏已保存，但刷新音色目录失败");
            }
        } catch (favoriteError) {
            setOperationError(favoriteError instanceof Error ? favoriteError.message : "更新音色收藏失败");
        } finally {
            setFavoritingVoiceId("");
        }
    }

    async function handleVoiceCreated(voice: ChannelVoice) {
        setCatalogVoices((current) => [voice, ...current.filter((item) => item.id !== voice.id)]);
        setOperationError("");
        try {
            await refreshSystemChannels();
        } catch (refreshError) {
            setOperationError(refreshError instanceof Error ? `音色已创建，但刷新系统目录失败：${refreshError.message}` : "音色已创建，但刷新系统目录失败");
        }
    }

    return (
        <>
            <button type="button" className={`canvas-audio-voice-trigger inline-flex min-w-0 items-center gap-2 ${className}`.trim()} onClick={() => setOpen(true)} aria-label={`选择音色，当前为 ${selectedVoiceLabel}`}>
                <AudioLines className="canvas-audio-voice-trigger-icon size-4 shrink-0" />
                <span className="canvas-audio-voice-trigger-label truncate">{selectedVoiceLabel}</span>
            </button>
            <Modal
                className="canvas-audio-voice-modal"
                open={open}
                title={null}
                footer={null}
                centered
                width={800}
                destroyOnHidden
                closeIcon={null}
                onCancel={close}
                styles={{
                    container: {
                        padding: 0,
                        overflow: "hidden",
                        borderRadius: 12,
                        background: theme.spatial.elevated,
                        color: theme.node.text,
                    },
                    body: { padding: 0 },
                    mask: { backdropFilter: "blur(6px)" },
                }}
            >
                <div className="canvas-audio-voice-dialog-theme" style={dialogStyle}>
                    <CanvasAudioVoiceCatalog
                        voices={catalogVoices}
                        loading={catalogLoading}
                        selectedVoice={selectedVoice}
                        loadingVoiceId={preview.loadingVoiceId}
                        playingVoiceId={preview.playingVoiceId}
                        favoritingVoiceId={favoritingVoiceId}
                        error={operationError || preview.error}
                        cloneAvailable={cloneAvailable}
                        onClose={close}
                        onClone={() => setCloneOpen(true)}
                        onSelect={selectVoice}
                        onPreview={(voice) => void preview.toggle(voice)}
                        onFavorite={(voice) => void toggleFavorite(voice)}
                    />
                </div>
            </Modal>
            <CanvasAudioVoiceCloneDialog open={cloneOpen} channelId={channel.id} onCancel={() => setCloneOpen(false)} onCreated={handleVoiceCreated} />
        </>
    );
}
