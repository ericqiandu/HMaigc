import { type CSSProperties, type ReactNode } from "react";

import { ImageSettingsTheme } from "@/components/image-settings-panel";
import {
    audioFormatOptionsForInterface,
    audioSpeedLabel,
    audioSpeedRangeForInterface,
    miniMaxBitrateOptions,
    miniMaxChannelOptions,
    miniMaxEmotionOptionsForModel,
    miniMaxLanguageOptions,
    miniMaxSampleRateOptions,
    normalizeAudioFormatValue,
    normalizeAudioSpeedValue,
    normalizeMiniMaxPitchValue,
    normalizeMiniMaxVolumeValue,
    type AudioSettingKey,
} from "@/lib/audio-generation";
import { type CanvasTheme } from "@/lib/canvas-theme";
import { modelOptionName, resolveModelChannel, type AiConfig } from "@/stores/use-config-store";

const speedOptions = ["0.75", "1", "1.25", "1.5"];

type AudioSettingsPanelProps = {
    config: AiConfig;
    onConfigChange: (key: AudioSettingKey, value: string) => void;
    theme: CanvasTheme;
    showTitle?: boolean;
    className?: string;
};

export function AudioSettingsPanel({ config, onConfigChange, theme, showTitle = true, className = "w-[360px] space-y-5 px-1 py-0.5" }: AudioSettingsPanelProps) {
    const channel = resolveModelChannel(config, config.model);
    const interfaceType = channel.interfaceType;
    const model = modelOptionName(config.model);
    const formatOptions = audioFormatOptionsForInterface(interfaceType);
    const speedRange = audioSpeedRangeForInterface(interfaceType);
    const isMiniMaxSpeech = interfaceType === "minimax-speech";
    const format = normalizeAudioFormatValue(config.audioFormat);
    const speed = normalizeAudioSpeedValue(config.audioSpeed, interfaceType);
    const selectStyle = { background: theme.node.fill, color: theme.node.text, borderColor: theme.node.stroke };

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={className} style={{ color: theme.node.text }} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? <div className="audio-settings-title text-lg font-semibold">音频设置</div> : null}
                {isMiniMaxSpeech ? (
                    <SettingGroup title="语气与语言" color={theme.node.muted}>
                        <div className="audio-settings-field-grid grid grid-cols-2 gap-2">
                            <LabeledSelect label="情绪" value={config.audioEmotion} options={miniMaxEmotionOptionsForModel(model)} style={selectStyle} onChange={(value) => onConfigChange("audioEmotion", value)} />
                            <LabeledSelect label="语言增强" value={config.audioLanguageBoost} options={miniMaxLanguageOptions} style={selectStyle} onChange={(value) => onConfigChange("audioLanguageBoost", value)} />
                        </div>
                    </SettingGroup>
                ) : null}
                <SettingGroup title="声音" color={theme.node.muted}>
                    <div className="audio-settings-speed-grid grid grid-cols-4 gap-2">
                        {speedOptions.map((value) => (
                            <OptionPill key={value} selected={speed === value} theme={theme} onClick={() => onConfigChange("audioSpeed", value)}>
                                {audioSpeedLabel(value)}
                            </OptionPill>
                        ))}
                    </div>
                    <div className={`audio-settings-field-grid grid gap-2 ${isMiniMaxSpeech ? "grid-cols-3" : "grid-cols-1"}`}>
                        <LabeledNumber
                            label="语速"
                            value={config.audioSpeed || "1"}
                            min={speedRange.min}
                            max={speedRange.max}
                            step={0.05}
                            style={selectStyle}
                            onChange={(value) => onConfigChange("audioSpeed", value)}
                            onBlur={(value) => onConfigChange("audioSpeed", normalizeAudioSpeedValue(value, interfaceType))}
                        />
                        {isMiniMaxSpeech ? (
                            <>
                                <LabeledNumber
                                    label="音量"
                                    value={config.audioVolume || "1"}
                                    min={0.1}
                                    max={10}
                                    step={0.1}
                                    style={selectStyle}
                                    onChange={(value) => onConfigChange("audioVolume", value)}
                                    onBlur={(value) => onConfigChange("audioVolume", normalizeMiniMaxVolumeValue(value))}
                                />
                                <LabeledNumber
                                    label="音调"
                                    value={config.audioPitch || "0"}
                                    min={-12}
                                    max={12}
                                    step={1}
                                    style={selectStyle}
                                    onChange={(value) => onConfigChange("audioPitch", value)}
                                    onBlur={(value) => onConfigChange("audioPitch", normalizeMiniMaxPitchValue(value))}
                                />
                            </>
                        ) : null}
                    </div>
                </SettingGroup>
                <SettingGroup title="格式" color={theme.node.muted}>
                    <div className="audio-settings-format-grid grid grid-cols-3 gap-2.5">
                        {formatOptions.map((item) => (
                            <OptionPill key={item.value} selected={format === item.value} theme={theme} onClick={() => onConfigChange("audioFormat", item.value)}>
                                {item.label}
                            </OptionPill>
                        ))}
                    </div>
                    {isMiniMaxSpeech ? (
                        <div className="audio-settings-field-grid grid grid-cols-3 gap-2">
                            <LabeledSelect label="采样率" value={config.audioSampleRate} options={miniMaxSampleRateOptions} style={selectStyle} onChange={(value) => onConfigChange("audioSampleRate", value)} />
                            <LabeledSelect label="码率" value={config.audioBitrate} options={miniMaxBitrateOptions} style={selectStyle} disabled={format !== "mp3"} onChange={(value) => onConfigChange("audioBitrate", value)} />
                            <LabeledSelect label="声道" value={config.audioChannel} options={miniMaxChannelOptions} style={selectStyle} onChange={(value) => onConfigChange("audioChannel", value)} />
                        </div>
                    ) : null}
                </SettingGroup>
                {!isMiniMaxSpeech ? (
                    <SettingGroup title="声音指令" color={theme.node.muted}>
                        <textarea
                            value={config.audioInstructions || ""}
                            placeholder="例如：自然、温暖、适合旁白。"
                            className="audio-settings-instructions thin-scrollbar h-20 w-full resize-none rounded-xl border bg-transparent px-3 py-2 text-sm leading-5 outline-none"
                            style={{ borderColor: theme.node.stroke, color: theme.node.text }}
                            onChange={(event) => onConfigChange("audioInstructions", event.target.value)}
                            onMouseDown={(event) => event.stopPropagation()}
                        />
                    </SettingGroup>
                ) : (
                    <p className="audio-settings-protocol-note text-xs leading-5" style={{ color: theme.node.muted }}>
                        停顿和语气词请从输入框上方插入，节点会按 MiniMax 官方文本标记提交。
                    </p>
                )}
            </div>
        </ImageSettingsTheme>
    );
}

function OptionPill({ selected, theme, onClick, children }: { selected: boolean; theme: CanvasTheme; onClick: () => void; children: ReactNode }) {
    return (
        <button
            type="button"
            className="audio-settings-option h-9 cursor-pointer rounded-md border px-2 text-sm transition hover:opacity-80"
            style={{ background: selected ? theme.node.fill : "transparent", borderColor: selected ? theme.node.text : theme.node.stroke, color: theme.node.text }}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onClick}
        >
            {children}
        </button>
    );
}

type SelectOption = { value: string; label: string };

function LabeledSelect({ label, value, options, style, disabled = false, onChange }: { label: string; value: string; options: readonly SelectOption[]; style: CSSProperties; disabled?: boolean; onChange: (value: string) => void }) {
    return (
        <label className="audio-settings-field min-w-0 space-y-1">
            <span className="audio-settings-field-label block text-[11px] opacity-65">{label}</span>
            <select
                className="audio-settings-select h-9 w-full min-w-0 rounded-md border px-2 text-xs outline-none disabled:cursor-not-allowed disabled:opacity-45"
                style={style}
                value={value}
                disabled={disabled}
                onChange={(event) => onChange(event.target.value)}
                onMouseDown={(event) => event.stopPropagation()}
            >
                {options.map((option) => (
                    <option key={option.value || "auto"} className="audio-settings-select-option" value={option.value}>
                        {option.label}
                    </option>
                ))}
            </select>
        </label>
    );
}

function LabeledNumber({ label, value, min, max, step, style, onChange, onBlur }: { label: string; value: string; min: number; max: number; step: number; style: CSSProperties; onChange: (value: string) => void; onBlur: (value: string) => void }) {
    return (
        <label className="audio-settings-field min-w-0 space-y-1">
            <span className="audio-settings-field-label block text-[11px] opacity-65">{label}</span>
            <input
                type="number"
                min={min}
                max={max}
                step={step}
                className="audio-settings-number h-9 w-full rounded-md border px-2 text-center text-xs outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none"
                style={{ ...style, WebkitTextFillColor: style.color }}
                value={value}
                onChange={(event) => onChange(event.target.value)}
                onBlur={(event) => onBlur(event.target.value)}
                onMouseDown={(event) => event.stopPropagation()}
            />
        </label>
    );
}

function SettingGroup({ title, color, children }: { title: string; color: string; children: ReactNode }) {
    return (
        <div className="audio-settings-group space-y-2.5">
            <div className="audio-settings-group-title text-xs font-medium" style={{ color }}>
                {title}
            </div>
            {children}
        </div>
    );
}
