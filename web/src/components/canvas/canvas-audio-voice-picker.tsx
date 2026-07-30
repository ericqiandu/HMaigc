import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { AudioLines, Check, Diamond, LoaderCircle, Pause, Play, Search, X } from "lucide-react";
import { Modal } from "antd";

import { audioLanguageLabel, normalizeAudioVoiceValue } from "@/lib/audio-generation";
import { canvasThemes } from "@/lib/canvas-theme";
import { previewChannelVoice } from "@/services/api/voices";
import { modelOptionName, resolveModelChannel, type AiConfig, type ChannelVoice } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";

import "./canvas-audio-voice-picker.css";

type CanvasAudioVoicePickerProps = {
    config: AiConfig;
    value: string;
    onChange: (voice: string) => void;
    className?: string;
};

type LanguageOption = {
    value: string;
    label: string;
    count: number;
};

const allLanguages = "__all__";
const unknownLanguage = "__unknown__";
const voiceRowHeight = 64;

export function CanvasAudioVoicePicker({ config, value, onChange, className = "" }: CanvasAudioVoicePickerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [open, setOpen] = useState(false);
    const [query, setQuery] = useState("");
    const [language, setLanguage] = useState(allLanguages);
    const [loadingVoiceId, setLoadingVoiceId] = useState("");
    const [playingVoiceId, setPlayingVoiceId] = useState("");
    const [previewError, setPreviewError] = useState("");
    const [voiceListElement, setVoiceListElement] = useState<HTMLDivElement | null>(null);
    const audioRef = useRef<HTMLAudioElement | null>(null);
    const previewAbortRef = useRef<AbortController | null>(null);
    const previewCacheRef = useRef(new Map<string, string>());
    const selectedVoice = normalizeAudioVoiceValue(value);
    const selectedModel = modelOptionName(config.model);
    const channel = resolveModelChannel(config, config.model);
    const voices = useMemo(
        () => (channel.voices || []).filter((voice) => voice.enabled && (voice.providerStatus === "active" || voice.providerStatus === "pending_activation") && (voice.compatibleModels.length === 0 || voice.compatibleModels.includes(selectedModel))),
        [channel.voices, selectedModel],
    );
    const selectedVoiceLabel = voices.find((voice) => voice.voiceKey === selectedVoice)?.displayName || "选择音色";
    const languageOptions = useMemo<LanguageOption[]>(() => {
        const counts = new Map<string, number>();
        voices.forEach((voice) => {
            const key = voice.language.trim() || unknownLanguage;
            counts.set(key, (counts.get(key) || 0) + 1);
        });
        return [
            { value: allLanguages, label: "全部音色", count: voices.length },
            ...[...counts.entries()]
                .filter(([value]) => value !== unknownLanguage)
                .sort(([left], [right]) => audioLanguageLabel(left).localeCompare(audioLanguageLabel(right), "zh-CN"))
                .map(([value, count]) => ({ value, label: audioLanguageLabel(value), count })),
            ...(counts.has(unknownLanguage) ? [{ value: unknownLanguage, label: "未标注", count: counts.get(unknownLanguage) || 0 }] : []),
        ];
    }, [voices]);
    const visibleVoices = useMemo(() => {
        const normalizedQuery = query.trim().toLocaleLowerCase();
        return voices.filter((voice) => {
            const matchesLanguage = language === allLanguages || (language === unknownLanguage ? !voice.language.trim() : voice.language === language);
            if (!matchesLanguage) return false;
            if (!normalizedQuery) return true;
            return `${voice.displayName} ${voice.description} ${audioLanguageLabel(voice.language)}`.toLocaleLowerCase().includes(normalizedQuery);
        });
    }, [language, query, voices]);
    const voiceVirtualizer = useVirtualizer({
        count: visibleVoices.length,
        getScrollElement: () => voiceListElement,
        estimateSize: () => voiceRowHeight,
        overscan: 8,
    });

    useEffect(
        () => () => {
            previewAbortRef.current?.abort();
            audioRef.current?.pause();
            audioRef.current = null;
        },
        [],
    );

    useEffect(() => {
        if (voiceListElement) voiceListElement.scrollTop = 0;
    }, [language, query, voiceListElement]);

    const dialogStyle = {
        "--audio-voice-surface": theme.spatial.elevated,
        "--audio-voice-fill": theme.node.fill,
        "--audio-voice-text": theme.node.text,
        "--audio-voice-muted": theme.node.muted,
        "--audio-voice-stroke": theme.node.stroke,
        "--audio-voice-accent": theme.accent.primary,
    } as CSSProperties;

    function stopPreview() {
        previewAbortRef.current?.abort();
        previewAbortRef.current = null;
        if (audioRef.current) {
            audioRef.current.pause();
            audioRef.current.currentTime = 0;
            audioRef.current = null;
        }
        setLoadingVoiceId("");
        setPlayingVoiceId("");
    }

    function close() {
        stopPreview();
        setOpen(false);
        setQuery("");
        setLanguage(allLanguages);
        setPreviewError("");
    }

    async function togglePreview(voice: ChannelVoice) {
        if (playingVoiceId === voice.id) {
            stopPreview();
            return;
        }
        stopPreview();
        setPreviewError("");
        setLoadingVoiceId(voice.id);
        const controller = new AbortController();
        previewAbortRef.current = controller;
        try {
            const cacheKey = `${channel.id}\u0000${selectedModel}\u0000${voice.id}`;
            let audioDataUrl = previewCacheRef.current.get(cacheKey);
            if (!audioDataUrl) {
                const result = await previewChannelVoice(channel.id, voice.id, selectedModel, controller.signal);
                audioDataUrl = result.preview.audioDataUrl;
                previewCacheRef.current.set(cacheKey, audioDataUrl);
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
                setPreviewError(`${voice.displayName} 的试听音频无法播放`);
            };
            await audio.play();
            setPlayingVoiceId(voice.id);
        } catch (error) {
            if (!controller.signal.aborted) setPreviewError(error instanceof Error ? error.message : "音色试听失败");
        } finally {
            if (previewAbortRef.current === controller) previewAbortRef.current = null;
            setLoadingVoiceId((current) => (current === voice.id ? "" : current));
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
                <section className="canvas-audio-voice-dialog" style={dialogStyle} aria-labelledby="canvas-audio-voice-title">
                    <header className="canvas-audio-voice-dialog-header">
                        <div className="canvas-audio-voice-dialog-heading">
                            <h2 id="canvas-audio-voice-title" className="canvas-audio-voice-dialog-title">
                                选择音色
                            </h2>
                            <p className="canvas-audio-voice-dialog-description">试听并选择当前音频节点的声音</p>
                        </div>
                        <button type="button" className="canvas-audio-voice-dialog-close" onClick={close} aria-label="关闭音色选择">
                            <X className="canvas-audio-voice-dialog-close-icon size-4" />
                        </button>
                    </header>

                    <div className="canvas-audio-voice-dialog-body">
                        <aside className="canvas-audio-voice-sidebar" aria-label="音色语言分类">
                            <span className="canvas-audio-voice-sidebar-title">语言</span>
                            <nav className="canvas-audio-voice-language-list thin-scrollbar" aria-label="按语言筛选音色">
                                {languageOptions.map((item) => (
                                    <button
                                        key={item.value}
                                        type="button"
                                        className={`canvas-audio-voice-language ${language === item.value ? "canvas-audio-voice-language--active" : ""}`}
                                        aria-pressed={language === item.value}
                                        onClick={() => setLanguage(item.value)}
                                    >
                                        <span className="canvas-audio-voice-language-label">{item.label}</span>
                                        <span className="canvas-audio-voice-language-count">{item.count}</span>
                                    </button>
                                ))}
                            </nav>
                        </aside>

                        <div className="canvas-audio-voice-catalog">
                            <div className="canvas-audio-voice-dialog-toolbar">
                                <label className="canvas-audio-voice-search">
                                    <Search className="canvas-audio-voice-search-icon size-4 shrink-0" />
                                    <input className="canvas-audio-voice-search-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索音色名称或描述" aria-label="搜索系统音色" />
                                </label>
                                <span className="canvas-audio-voice-count">共 {visibleVoices.length} 个</span>
                            </div>

                            {previewError ? (
                                <div className="canvas-audio-voice-preview-error" role="alert">
                                    {previewError}
                                </div>
                            ) : null}

                            <div ref={setVoiceListElement} className="canvas-audio-voice-list thin-scrollbar" role="list" aria-label="系统音色">
                                {visibleVoices.length ? (
                                    <div className="canvas-audio-voice-virtual-space" style={{ height: voiceVirtualizer.getTotalSize() }}>
                                        {voiceVirtualizer.getVirtualItems().map((virtualRow) => {
                                            const voice = visibleVoices[virtualRow.index];
                                            if (!voice) return null;
                                            const selected = voice.voiceKey === selectedVoice;
                                            const loading = loadingVoiceId === voice.id;
                                            const playing = playingVoiceId === voice.id;
                                            const languageLabel = voice.language.trim() ? audioLanguageLabel(voice.language) : "未标注";
                                            return (
                                                <div
                                                    key={voice.id}
                                                    className={`canvas-audio-voice-option ${selected ? "canvas-audio-voice-option--selected" : ""}`}
                                                    role="listitem"
                                                    style={{ height: voiceRowHeight, transform: `translateY(${virtualRow.start}px)` }}
                                                >
                                                    <button
                                                        type="button"
                                                        className="canvas-audio-voice-preview-button"
                                                        disabled={!voice.accessible || loading}
                                                        onClick={() => void togglePreview(voice)}
                                                        aria-label={`${playing ? "停止" : "试听"}${voice.displayName}`}
                                                        title={`${playing ? "停止" : "试听"} ${voice.displayName}`}
                                                    >
                                                        {loading ? (
                                                            <LoaderCircle className="canvas-audio-voice-preview-loading size-4 animate-spin" />
                                                        ) : playing ? (
                                                            <Pause className="canvas-audio-voice-preview-pause size-4 fill-current" />
                                                        ) : (
                                                            <Play className="canvas-audio-voice-preview-play size-4 fill-current" />
                                                        )}
                                                    </button>
                                                    <button
                                                        type="button"
                                                        className="canvas-audio-voice-select-button"
                                                        disabled={!voice.accessible}
                                                        onClick={() => {
                                                            onChange(voice.voiceKey);
                                                            close();
                                                        }}
                                                        aria-label={`选择音色 ${voice.displayName}`}
                                                    >
                                                        <span className="canvas-audio-voice-option-copy">
                                                            <span className="canvas-audio-voice-option-title-row">
                                                                <span className="canvas-audio-voice-option-name">{voice.displayName}</span>
                                                                <span className="canvas-audio-voice-option-language">{languageLabel}</span>
                                                                {voice.accessPolicy === "member" ? <Diamond className="canvas-audio-voice-member-icon size-3.5 text-amber-400" aria-label="会员专属" /> : null}
                                                            </span>
                                                            <span className="canvas-audio-voice-option-meta" title={voice.description || "未配置音色说明"}>
                                                                {voice.description || "未配置音色说明"}
                                                            </span>
                                                        </span>
                                                        <span className={`canvas-audio-voice-option-state ${selected ? "canvas-audio-voice-option-state--selected" : ""}`}>
                                                            {selected ? <Check className="canvas-audio-voice-option-check size-3.5" /> : voice.accessible ? null : "会员"}
                                                        </span>
                                                    </button>
                                                </div>
                                            );
                                        })}
                                    </div>
                                ) : (
                                    <div className="canvas-audio-voice-empty">{voices.length === 0 ? "当前音频模型尚未配置可用音色，请联系管理员" : "当前条件下没有匹配的音色"}</div>
                                )}
                            </div>
                        </div>
                    </div>
                </section>
            </Modal>
        </>
    );
}
