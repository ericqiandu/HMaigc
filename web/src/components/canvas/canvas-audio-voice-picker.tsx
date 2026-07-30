import { useMemo, useState } from "react";
import { AudioLines, Check, Search, X } from "lucide-react";
import { Modal } from "antd";

import { audioVoiceOptions, audioVoiceLabel, normalizeAudioVoiceValue } from "@/lib/audio-generation";
import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";

type CanvasAudioVoicePickerProps = {
    value: string;
    onChange: (voice: string) => void;
};

export function CanvasAudioVoicePicker({ value, onChange }: CanvasAudioVoicePickerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const selectedVoice = normalizeAudioVoiceValue(value);
    const visibleVoices = useMemo(() => {
        const normalizedQuery = query.trim().toLocaleLowerCase();
        if (!normalizedQuery) return audioVoiceOptions;
        return audioVoiceOptions.filter((voice) => voice.label.toLocaleLowerCase().includes(normalizedQuery));
    }, [query]);

    const close = () => {
        setOpen(false);
        setQuery("");
    };

    return (
        <>
            <button
                type="button"
                className="canvas-audio-voice-trigger inline-flex min-w-0 items-center gap-2"
                onClick={() => setOpen(true)}
                aria-label={`选择音色，当前为 ${audioVoiceLabel(selectedVoice)}`}
            >
                <AudioLines className="canvas-audio-voice-trigger-icon size-4 shrink-0" />
                <span className="canvas-audio-voice-trigger-label truncate">{audioVoiceLabel(selectedVoice)}</span>
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
                            const selected = voice.value === selectedVoice;
                            return (
                                <button
                                    key={voice.value}
                                    type="button"
                                    className={`canvas-audio-voice-option ${selected ? "canvas-audio-voice-option--selected" : ""}`}
                                    role="option"
                                    aria-selected={selected}
                                    onClick={() => {
                                        onChange(voice.value);
                                        close();
                                    }}
                                >
                                    <span className="canvas-audio-voice-option-icon">
                                        <AudioLines className="canvas-audio-voice-option-wave size-5" />
                                    </span>
                                    <span className="canvas-audio-voice-option-copy">
                                        <span className="canvas-audio-voice-option-name">{voice.label}</span>
                                        <span className="canvas-audio-voice-option-meta">系统音色</span>
                                    </span>
                                    <span className={`canvas-audio-voice-option-state ${selected ? "canvas-audio-voice-option-state--selected" : ""}`}>
                                        {selected ? <Check className="canvas-audio-voice-option-check size-3.5" /> : "选择"}
                                    </span>
                                </button>
                            );
                        })}
                        {!visibleVoices.length ? (
                            <div className="canvas-audio-voice-empty">没有匹配的系统音色</div>
                        ) : null}
                    </div>
                </section>
            </Modal>
        </>
    );
}
