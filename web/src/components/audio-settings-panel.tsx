import { type CSSProperties, type ReactNode } from "react";
import { RotateCcw } from "lucide-react";

import { ImageSettingsTheme } from "@/components/image-settings-panel";
import {
    audioFormatOptionsForInterface,
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

type AudioSettingsPanelProps = {
    config: AiConfig;
    onConfigChange: (key: AudioSettingKey, value: string) => void;
    theme: CanvasTheme;
    showTitle?: boolean;
    className?: string;
};

type SelectOption = {
    value: string;
    label: string;
};

const miniMaxResetValues: ReadonlyArray<readonly [AudioSettingKey, string]> = [
    ["audioEmotion", ""],
    ["audioLanguageBoost", "auto"],
    ["audioSpeed", "1"],
    ["audioVolume", "1"],
    ["audioPitch", "0"],
    ["audioFormat", "mp3"],
    ["audioSampleRate", "32000"],
    ["audioBitrate", "128000"],
    ["audioChannel", "1"],
];

const commonResetValues: ReadonlyArray<readonly [AudioSettingKey, string]> = [
    ["audioSpeed", "1"],
    ["audioFormat", "mp3"],
    ["audioInstructions", ""],
];

export function AudioSettingsPanel({ config, onConfigChange, theme, showTitle = true, className = "" }: AudioSettingsPanelProps) {
    const channel = resolveModelChannel(config, config.model);
    const interfaceType = channel.interfaceType;
    const model = modelOptionName(config.model);
    const isMiniMaxSpeech = interfaceType === "minimax-speech";
    const formatOptions = audioFormatOptionsForInterface(interfaceType);
    const format = normalizeAudioFormatValue(config.audioFormat);
    const speedRange = audioSpeedRangeForInterface(interfaceType);
    const panelStyle = {
        "--audio-settings-text": theme.node.text,
        "--audio-settings-muted": theme.node.muted,
        "--audio-settings-fill": theme.node.fill,
        "--audio-settings-stroke": theme.node.stroke,
        "--audio-settings-accent": theme.accent.primary,
    } as CSSProperties;

    function resetSettings() {
        const values = isMiniMaxSpeech ? miniMaxResetValues : commonResetValues;
        values.forEach(([key, value]) => onConfigChange(key, value));
    }

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={`audio-settings-panel-root ${className}`.trim()} style={panelStyle} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? (
                    <header className="audio-settings-header">
                        <div className="audio-settings-heading">
                            <h3 className="audio-settings-title">音频参数</h3>
                            <p className="audio-settings-subtitle">调节当前节点的声音表现与输出规格</p>
                        </div>
                        <button type="button" className="audio-settings-reset" onClick={resetSettings} aria-label="重置音频参数">
                            <RotateCcw className="audio-settings-reset-icon size-3.5" />
                            <span className="audio-settings-reset-label">重置</span>
                        </button>
                    </header>
                ) : null}

                <SettingGroup title="基础调节">
                    <RangeField
                        label="语速"
                        value={config.audioSpeed || "1"}
                        min={speedRange.min}
                        max={speedRange.max}
                        step={0.05}
                        suffix="x"
                        onChange={(value) => onConfigChange("audioSpeed", value)}
                        onBlur={(value) => onConfigChange("audioSpeed", normalizeAudioSpeedValue(value, interfaceType))}
                    />
                    {isMiniMaxSpeech ? (
                        <>
                            <RangeField
                                label="声调"
                                value={config.audioPitch || "0"}
                                min={-12}
                                max={12}
                                step={1}
                                onChange={(value) => onConfigChange("audioPitch", value)}
                                onBlur={(value) => onConfigChange("audioPitch", normalizeMiniMaxPitchValue(value))}
                            />
                            <RangeField
                                label="音量"
                                value={config.audioVolume || "1"}
                                min={0.1}
                                max={10}
                                step={0.1}
                                onChange={(value) => onConfigChange("audioVolume", value)}
                                onBlur={(value) => onConfigChange("audioVolume", normalizeMiniMaxVolumeValue(value))}
                            />
                        </>
                    ) : null}
                </SettingGroup>

                {isMiniMaxSpeech ? (
                    <SettingGroup title="表达方式">
                        <div className="audio-settings-field-grid audio-settings-field-grid--two">
                            <LabeledSelect label="情绪" value={config.audioEmotion} options={miniMaxEmotionOptionsForModel(model)} onChange={(value) => onConfigChange("audioEmotion", value)} />
                            <LabeledSelect label="语言增强" value={config.audioLanguageBoost} options={miniMaxLanguageOptions} onChange={(value) => onConfigChange("audioLanguageBoost", value)} />
                        </div>
                    </SettingGroup>
                ) : (
                    <SettingGroup title="声音指令">
                        <label className="audio-settings-field">
                            <span className="audio-settings-field-label">表达要求</span>
                            <textarea
                                value={config.audioInstructions || ""}
                                placeholder="例如：自然、温暖、适合旁白"
                                className="audio-settings-instructions thin-scrollbar"
                                onChange={(event) => onConfigChange("audioInstructions", event.target.value)}
                                onMouseDown={(event) => event.stopPropagation()}
                            />
                        </label>
                    </SettingGroup>
                )}

                <SettingGroup title="输出设置">
                    <div className="audio-settings-format-grid" role="group" aria-label="音频格式">
                        {formatOptions.map((item) => (
                            <OptionPill key={item.value} selected={format === item.value} onClick={() => onConfigChange("audioFormat", item.value)}>
                                {item.label}
                            </OptionPill>
                        ))}
                    </div>
                    {isMiniMaxSpeech ? (
                        <div className="audio-settings-field-grid audio-settings-field-grid--three">
                            <LabeledSelect label="采样率" value={config.audioSampleRate} options={miniMaxSampleRateOptions} onChange={(value) => onConfigChange("audioSampleRate", value)} />
                            <LabeledSelect label="码率" value={config.audioBitrate} options={miniMaxBitrateOptions} disabled={format !== "mp3"} onChange={(value) => onConfigChange("audioBitrate", value)} />
                            <LabeledSelect label="声道" value={config.audioChannel} options={miniMaxChannelOptions} onChange={(value) => onConfigChange("audioChannel", value)} />
                        </div>
                    ) : null}
                </SettingGroup>

                {isMiniMaxSpeech ? <p className="audio-settings-protocol-note">停顿与语气词从输入框上方插入，并按 MiniMax 官方文本标记提交。</p> : null}
            </div>
        </ImageSettingsTheme>
    );
}

function RangeField({
    label,
    value,
    min,
    max,
    step,
    suffix,
    onChange,
    onBlur,
}: {
    label: string;
    value: string;
    min: number;
    max: number;
    step: number;
    suffix?: string;
    onChange: (value: string) => void;
    onBlur: (value: string) => void;
}) {
    const numericValue = Number(value);
    const safeValue = Number.isFinite(numericValue) ? numericValue : min;

    return (
        <label className="audio-settings-range-field">
            <span className="audio-settings-range-label">{label}</span>
            <input
                type="range"
                min={min}
                max={max}
                step={step}
                value={safeValue}
                className="audio-settings-range"
                aria-label={`${label}滑杆`}
                onChange={(event) => onChange(event.target.value)}
                onMouseDown={(event) => event.stopPropagation()}
            />
            <span className="audio-settings-range-value">
                <input
                    type="number"
                    min={min}
                    max={max}
                    step={step}
                    value={value}
                    className="audio-settings-number"
                    aria-label={label}
                    onChange={(event) => onChange(event.target.value)}
                    onBlur={(event) => onBlur(event.target.value)}
                    onMouseDown={(event) => event.stopPropagation()}
                />
                {suffix ? <span className="audio-settings-range-suffix">{suffix}</span> : null}
            </span>
        </label>
    );
}

function OptionPill({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: ReactNode }) {
    return (
        <button type="button" className={`audio-settings-option ${selected ? "audio-settings-option--selected" : ""}`} aria-pressed={selected} onMouseDown={(event) => event.stopPropagation()} onClick={onClick}>
            {children}
        </button>
    );
}

function LabeledSelect({ label, value, options, disabled = false, onChange }: { label: string; value: string; options: readonly SelectOption[]; disabled?: boolean; onChange: (value: string) => void }) {
    return (
        <label className="audio-settings-field">
            <span className="audio-settings-field-label">{label}</span>
            <select
                className="audio-settings-select"
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

function SettingGroup({ title, children }: { title: string; children: ReactNode }) {
    return (
        <section className="audio-settings-group">
            <h4 className="audio-settings-group-title">{title}</h4>
            <div className="audio-settings-group-content">{children}</div>
        </section>
    );
}
