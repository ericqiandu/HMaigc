import { useMemo, useState } from "react";
import { AudioLines, Check, Diamond, Search, X } from "lucide-react";
import { Modal } from "antd";

import { normalizeAudioVoiceValue } from "@/lib/audio-generation";
import { canvasThemes } from "@/lib/canvas-theme";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";

type CanvasAudioVoicePickerProps = {
    config: AiConfig;
    value: string;
    onChange: (voice: string) => void;
    className?: string;
};

export function CanvasAudioVoicePicker({ config, value, onChange, className = "" }: CanvasAudioVoicePickerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const selectedVoice = normalizeAudioVoiceValue(value);
    const selectedModel = modelOptionName(config.model);
    const channel = resolveModelChannel(config, config.model);
    const voices = useMemo(
        () => (channel.voices || []).filter((voice) =>
            voice.enabled
            && (voice.providerStatus === "active" || voice.providerStatus === "pending_activation")
            && (voice.compatibleModels.length === 0 || voice.compatibleModels.includes(selectedModel)),
        ),
        [channel.voices, selectedModel],
    );
    const selectedVoiceLabel = voices.find((voice) => voice.voiceKey === selectedVoice)?.displayName || "选择音色";
    const visibleVoices = useMemo(() => {
        const normalizedQuery = query.trim().toLocaleLowerCase();
        if (!normalizedQuery) return voices;
        return voices.filter((voice) =>
            `${voice.displayName} ${voice.description} ${voice.language}`.toLocaleLowerCase().includes(normalizedQuery),
        );
    }, [query, voices]);

    const close = () => {
        setOpen(false);
        setQuery("");
    };

    return (
        <>
            <button
                type="button"
                className={`canvas-audio-voice-trigger inline-flex min-w-0 items-center gap-2 ${className}`.trim()}
                onClick={() => setOpen(true)}
                aria-label={`选择音色，当前为 ${selectedVoiceLabel}`}
            >
                <AudioLines className="canvas-audio-voice-trigger-icon size-4 shrink-0" />
                <span className="canvas-audio-voice-trigger-label truncate">{selectedVoiceLabel}</span>
            </button>
            <Modal
                className="canvas-audio-voice-modal"
                open={open}
                title={null}
                footer={null}
                centered
                width={720}
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
                    mask: { backdropFilter: "blur(5px)" },
                }}
            >
                <section className="canvas-audio-voice-dialog" aria-labelledby="canvas-audio-voice-title">
                    <header className="canvas-audio-voice-dialog-header">
                        <div className="canvas-audio-voice-dialog-heading">
                            <h2 id="canvas-audio-voice-title" className="canvas-audio-voice-dialog-title">音色选择</h2>
                            <p className="canvas-audio-voice-dialog-description">选择当前音频节点使用的系统音色</p>
                        </div>
                        <button type="button" className="canvas-audio-voice-dialog-close" onClick={close} aria-label="关闭音色选择">
                            <X className="canvas-audio-voice-dialog-close-icon size-4" />
                        </button>
                    </header>
                    <div className="canvas-audio-voice-dialog-toolbar">
                        <label className="canvas-audio-voice-search">
                            <Search className="canvas-audio-voice-search-icon size-4 shrink-0" />
                            <input
                                className="canvas-audio-voice-search-input"
                                value={query}
                                onChange={(event) => setQuery(event.target.value)}
                                placeholder="搜索系统音色"
                                aria-label="搜索系统音色"
                            />
                        </label>
                        <span className="canvas-audio-voice-count">{visibleVoices.length} 个音色</span>
                    </div>
                    <div className="canvas-audio-voice-list thin-scrollbar" role="listbox" aria-label="系统音色">
                        {visibleVoices.map((voice) => {
                            const selected = voice.voiceKey === selectedVoice;
                            return (
                                <button
                                    key={voice.id}
                                    type="button"
                                    className={`canvas-audio-voice-option ${selected ? "canvas-audio-voice-option--selected" : ""}`}
                                    role="option"
                                    aria-selected={selected}
                                    aria-disabled={!voice.accessible}
                                    disabled={!voice.accessible}
                                    onClick={() => {
                                        onChange(voice.voiceKey);
                                        close();
                                    }}
                                >
                                    <span className="canvas-audio-voice-option-icon">
                                        <AudioLines className="canvas-audio-voice-option-wave size-5" />
                                    </span>
                                    <span className="canvas-audio-voice-option-copy">
                                        <span className="canvas-audio-voice-option-name">
                                            {voice.displayName}
                                            {voice.accessPolicy === "member" ? <Diamond className="ml-1 inline size-3.5 text-amber-400" aria-label="会员专属" /> : null}
                                        </span>
                                        <span className="canvas-audio-voice-option-meta">{voice.description || voice.language || voice.kind}</span>
                                    </span>
                                    <span className={`canvas-audio-voice-option-state ${selected ? "canvas-audio-voice-option-state--selected" : ""}`}>
                                        {selected ? <Check className="canvas-audio-voice-option-check size-3.5" /> : voice.accessible ? "选择" : "会员"}
                                    </span>
                                </button>
                            );
                        })}
                        {!visibleVoices.length ? (
                            <div className="canvas-audio-voice-empty">
                                {voices.length === 0 ? "当前音频模型尚未配置可用音色，请联系管理员" : "没有匹配的系统音色"}
                            </div>
                        ) : null}
                    </div>
                </section>
            </Modal>
        </>
    );
}
