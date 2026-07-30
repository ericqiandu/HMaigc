import { useEffect, useMemo, useState } from "react";
import { Check, ChevronLeft, ChevronRight, Diamond, Filter, LoaderCircle, Mic, Pause, Play, Search, Star, X } from "lucide-react";
import { Popover } from "antd";

import { audioLanguageLabel } from "@/lib/audio-generation";
import type { ChannelVoice } from "@/stores/use-config-store";

type VoiceCatalogTab = "library" | "mine" | "favorites";

type CanvasAudioVoiceCatalogProps = {
    voices: ChannelVoice[];
    selectedVoice: string;
    loadingVoiceId: string;
    playingVoiceId: string;
    favoritingVoiceId: string;
    error: string;
    cloneAvailable: boolean;
    onClose: () => void;
    onClone: () => void;
    onSelect: (voice: ChannelVoice) => void;
    onPreview: (voice: ChannelVoice) => void;
    onFavorite: (voice: ChannelVoice) => void;
};

const allLanguages = "__all__";
const unknownLanguage = "__unknown__";
const pageSize = 20;

function paginationItems(current: number, pageCount: number): Array<number | "ellipsis"> {
    if (pageCount <= 7) return Array.from({ length: pageCount }, (_, index) => index + 1);
    const pages = new Set([1, pageCount, current - 1, current, current + 1].filter((page) => page >= 1 && page <= pageCount));
    const sorted = [...pages].sort((left, right) => left - right);
    const result: Array<number | "ellipsis"> = [];
    sorted.forEach((page, index) => {
        if (index > 0 && page - sorted[index - 1] > 1) result.push("ellipsis");
        result.push(page);
    });
    return result;
}

export function CanvasAudioVoiceCatalog(props: CanvasAudioVoiceCatalogProps) {
    const [tab, setTab] = useState<VoiceCatalogTab>("library");
    const [query, setQuery] = useState("");
    const [language, setLanguage] = useState(allLanguages);
    const [page, setPage] = useState(1);

    const languageOptions = useMemo(() => {
        const counts = new Map<string, number>();
        props.voices.forEach((voice) => {
            const key = voice.language.trim() || unknownLanguage;
            counts.set(key, (counts.get(key) || 0) + 1);
        });
        return [
            { value: allLanguages, label: "全部语言", count: props.voices.length },
            ...[...counts.entries()]
                .filter(([value]) => value !== unknownLanguage)
                .sort(([left], [right]) => audioLanguageLabel(left).localeCompare(audioLanguageLabel(right), "zh-CN"))
                .map(([value, count]) => ({ value, label: audioLanguageLabel(value), count })),
            ...(counts.has(unknownLanguage) ? [{ value: unknownLanguage, label: "未标注", count: counts.get(unknownLanguage) || 0 }] : []),
        ];
    }, [props.voices]);

    const filteredVoices = useMemo(() => {
        const normalizedQuery = query.trim().toLocaleLowerCase();
        return props.voices.filter((voice) => {
            const matchesTab = tab === "library" || (tab === "mine" ? voice.ownedByCurrentUser : voice.favorited);
            const matchesLanguage = language === allLanguages || (language === unknownLanguage ? !voice.language.trim() : voice.language === language);
            const matchesQuery = !normalizedQuery || `${voice.displayName} ${voice.description} ${audioLanguageLabel(voice.language)}`.toLocaleLowerCase().includes(normalizedQuery);
            return matchesTab && matchesLanguage && matchesQuery;
        });
    }, [language, props.voices, query, tab]);
    const pageCount = Math.max(1, Math.ceil(filteredVoices.length / pageSize));
    const visibleVoices = filteredVoices.slice((page - 1) * pageSize, page * pageSize);

    useEffect(() => {
        setPage(1);
    }, [language, query, tab]);

    useEffect(() => {
        setPage((current) => Math.min(current, pageCount));
    }, [pageCount]);

    const filterContent = (
        <div className="canvas-audio-voice-filter-menu" aria-label="按语言筛选音色">
            <span className="canvas-audio-voice-filter-title">语言</span>
            <div className="canvas-audio-voice-filter-options">
                {languageOptions.map((option) => (
                    <button
                        key={option.value}
                        type="button"
                        className={`canvas-audio-voice-filter-option ${language === option.value ? "canvas-audio-voice-filter-option--active" : ""}`}
                        aria-pressed={language === option.value}
                        onClick={() => setLanguage(option.value)}
                    >
                        <span className="canvas-audio-voice-filter-option-label">{option.label}</span>
                        <span className="canvas-audio-voice-filter-option-count">{option.count}</span>
                    </button>
                ))}
            </div>
        </div>
    );

    return (
        <section className="canvas-audio-voice-dialog" aria-labelledby="canvas-audio-voice-title">
            <header className="canvas-audio-voice-dialog-header">
                <h2 id="canvas-audio-voice-title" className="canvas-audio-voice-dialog-title">
                    音色选择
                </h2>
                <button type="button" className="canvas-audio-voice-dialog-close" onClick={props.onClose} aria-label="关闭音色选择">
                    <X className="canvas-audio-voice-dialog-close-icon size-5" />
                </button>
            </header>

            <div className="canvas-audio-voice-toolbar">
                <div className="canvas-audio-voice-tabs" role="tablist" aria-label="音色分类">
                    {(
                        [
                            ["library", "音色库"],
                            ["mine", "我的音色"],
                            ["favorites", "收藏音色"],
                        ] as const
                    ).map(([value, label]) => (
                        <button key={value} type="button" role="tab" aria-selected={tab === value} className={`canvas-audio-voice-tab ${tab === value ? "canvas-audio-voice-tab--active" : ""}`} onClick={() => setTab(value)}>
                            {label}
                        </button>
                    ))}
                </div>
                <div className="canvas-audio-voice-toolbar-actions">
                    <button type="button" className="canvas-audio-voice-clone-entry" disabled={!props.cloneAvailable} onClick={props.onClone} title={props.cloneAvailable ? "克隆新音色" : "当前音频渠道不支持音色克隆"}>
                        <Mic className="canvas-audio-voice-clone-entry-icon size-4" />
                        克隆新音色
                    </button>
                    <label className="canvas-audio-voice-search">
                        <Search className="canvas-audio-voice-search-icon size-4" />
                        <input className="canvas-audio-voice-search-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索音色库" aria-label="搜索音色库" />
                    </label>
                    <Popover placement="bottomRight" trigger="click" content={filterContent} overlayClassName="canvas-audio-voice-filter-popover">
                        <button type="button" className={`canvas-audio-voice-filter-trigger ${language !== allLanguages ? "canvas-audio-voice-filter-trigger--active" : ""}`} aria-label="筛选音色">
                            <Filter className="canvas-audio-voice-filter-trigger-icon size-4" />
                            筛选
                        </button>
                    </Popover>
                </div>
            </div>

            {props.error ? (
                <div className="canvas-audio-voice-error" role="alert">
                    {props.error}
                </div>
            ) : null}

            <div className="canvas-audio-voice-list thin-scrollbar" role="list" aria-label="音色列表">
                {visibleVoices.length ? (
                    visibleVoices.map((voice) => {
                        const selected = voice.voiceKey === props.selectedVoice;
                        const loading = props.loadingVoiceId === voice.id;
                        const playing = props.playingVoiceId === voice.id;
                        const favoriting = props.favoritingVoiceId === voice.id;
                        const languageLabel = voice.language.trim() ? audioLanguageLabel(voice.language) : "未标注";
                        return (
                            <div key={voice.id} className={`canvas-audio-voice-row ${selected ? "canvas-audio-voice-row--selected" : ""}`} role="listitem">
                                <button type="button" className="canvas-audio-voice-preview-button" disabled={!voice.accessible || loading} onClick={() => props.onPreview(voice)} aria-label={`${playing ? "停止" : "试听"}${voice.displayName}`}>
                                    {loading ? (
                                        <LoaderCircle className="canvas-audio-voice-preview-icon size-4 animate-spin" />
                                    ) : playing ? (
                                        <Pause className="canvas-audio-voice-preview-icon size-4 fill-current" />
                                    ) : (
                                        <Play className="canvas-audio-voice-preview-icon size-4 fill-current" />
                                    )}
                                </button>
                                <button type="button" className="canvas-audio-voice-row-select" disabled={!voice.accessible} onClick={() => props.onSelect(voice)}>
                                    <span className="canvas-audio-voice-row-copy">
                                        <span className="canvas-audio-voice-row-name">{voice.displayName}</span>
                                        <span className="canvas-audio-voice-row-tags">
                                            <span className="canvas-audio-voice-row-tag">{languageLabel}</span>
                                            {voice.ownedByCurrentUser ? <span className="canvas-audio-voice-row-tag">克隆</span> : null}
                                            {voice.accessPolicy === "member" ? <Diamond className="canvas-audio-voice-member-icon size-3.5 text-amber-400" aria-label="会员专属" /> : null}
                                        </span>
                                    </span>
                                    <span className={`canvas-audio-voice-select-state ${selected ? "canvas-audio-voice-select-state--selected" : ""}`}>
                                        {selected ? (
                                            <>
                                                <Check className="canvas-audio-voice-select-check size-3.5" /> 已选
                                            </>
                                        ) : voice.accessible ? (
                                            "选择"
                                        ) : (
                                            "会员"
                                        )}
                                    </span>
                                </button>
                                <button
                                    type="button"
                                    className={`canvas-audio-voice-favorite ${voice.favorited ? "canvas-audio-voice-favorite--active" : ""}`}
                                    disabled={favoriting}
                                    onClick={() => props.onFavorite(voice)}
                                    aria-label={`${voice.favorited ? "取消收藏" : "收藏"}${voice.displayName}`}
                                >
                                    {favoriting ? <LoaderCircle className="canvas-audio-voice-favorite-icon size-4 animate-spin" /> : <Star className={`canvas-audio-voice-favorite-icon size-4 ${voice.favorited ? "fill-current" : ""}`} />}
                                </button>
                            </div>
                        );
                    })
                ) : (
                    <div className="canvas-audio-voice-empty">{props.voices.length === 0 ? "当前音频模型尚未配置可用音色，请联系管理员" : tab === "mine" ? "还没有克隆音色" : tab === "favorites" ? "还没有收藏音色" : "当前条件下没有匹配的音色"}</div>
                )}
            </div>

            <footer className="canvas-audio-voice-footer">
                <span className="canvas-audio-voice-total">共 {filteredVoices.length} 条</span>
                <nav className="canvas-audio-voice-pagination" aria-label="音色分页">
                    <button type="button" className="canvas-audio-voice-page-button" disabled={page <= 1} onClick={() => setPage((current) => Math.max(1, current - 1))} aria-label="上一页">
                        <ChevronLeft className="canvas-audio-voice-page-icon size-4" />
                    </button>
                    {paginationItems(page, pageCount).map((item, index) =>
                        item === "ellipsis" ? (
                            <span key={`ellipsis-${index}`} className="canvas-audio-voice-page-ellipsis">
                                …
                            </span>
                        ) : (
                            <button key={item} type="button" className={`canvas-audio-voice-page-button ${page === item ? "canvas-audio-voice-page-button--active" : ""}`} aria-current={page === item ? "page" : undefined} onClick={() => setPage(item)}>
                                {item}
                            </button>
                        ),
                    )}
                    <button type="button" className="canvas-audio-voice-page-button" disabled={page >= pageCount} onClick={() => setPage((current) => Math.min(pageCount, current + 1))} aria-label="下一页">
                        <ChevronRight className="canvas-audio-voice-page-icon size-4" />
                    </button>
                    <span className="canvas-audio-voice-page-size">20条/页</span>
                </nav>
            </footer>
        </section>
    );
}
