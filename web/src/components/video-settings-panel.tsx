import { type KeyboardEvent, type ReactNode, useEffect, useId, useState } from "react";

import { ImageSettingsTheme } from "@/components/image-settings-panel";
import { type CanvasTheme } from "@/lib/canvas-theme";
import { boolConfig, normalizeSeedanceRatio, seedanceRatioOptions } from "@/lib/seedance-video";
import { normalizeVideoResolution } from "@/lib/video-generation-options";
import { normalizeVideoConfigForModel, resolveVideoModelCapabilities, videoRatiosForMode } from "@/lib/video-model-capabilities";
import { type AiConfig } from "@/stores/use-config-store";
import type { CanvasVideoGenerationMode } from "@/types/canvas";
import { CanvasGenerationSettingsOption, CanvasGenerationSettingsRatioOption, CanvasGenerationSettingsSection } from "@/components/canvas/canvas-generation-settings-ui";

type VideoSettingsPanelProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    theme: CanvasTheme;
    showTitle?: boolean;
    className?: string;
    generationMode?: CanvasVideoGenerationMode;
};

export function VideoSettingsPanel({ config, onConfigChange, theme, showTitle = true, className = "canvas-generation-settings w-[316px] space-y-5", generationMode }: VideoSettingsPanelProps) {
    const capabilities = resolveVideoModelCapabilities(config);
    const normalizedConfig = normalizeVideoConfigForModel(config, generationMode);
    const ratioOptions = videoRatiosForMode(capabilities, generationMode);
    const resolution = normalizedConfig.vquality;
    const ratio = ratioOptions.some((option) => option.value === normalizedConfig.size) ? normalizedConfig.size : ratioOptions[0].value;
    const durationOptions = capabilities.durations;
    const duration = Number(normalizedConfig.videoSeconds);
    const generateAudio = boolConfig(config.videoGenerateAudio, true);
    const generationCount = normalizeVideoCount(config.count, capabilities.outputCounts);

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={className} style={{ color: theme.node.text }} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? <div className="canvas-video-settings-title text-sm font-semibold">视频设置</div> : null}

                <CanvasGenerationSettingsSection label="比例" theme={theme}>
                    <div className="canvas-video-ratio-grid grid gap-2">
                        {ratioOptions.map((item) => (
                            <RatioOption key={item.value} label={item.label} ratio={item.value} selected={ratio === item.value} theme={theme} onClick={() => onConfigChange("size", item.value)} />
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>

                <CanvasGenerationSettingsSection label="清晰度" theme={theme}>
                    <div className="canvas-video-resolution-grid grid gap-2">
                        {capabilities.resolutions.map((item) => (
                            <OptionButton key={item.value} selected={resolution === item.value} theme={theme} onClick={() => onConfigChange("vquality", item.value)}>
                                {item.label}
                            </OptionButton>
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>

                <CanvasGenerationSettingsSection label="视频时长" theme={theme}>
                    <VideoDurationInput duration={duration} durationOptions={durationOptions} customDurationRange={capabilities.customDurationRange} theme={theme} onCommit={(value) => onConfigChange("videoSeconds", String(value))} />
                </CanvasGenerationSettingsSection>

                {capabilities.referenceLimits ? (
                    <CanvasGenerationSettingsSection label="参考素材" theme={theme}>
                        <div className="canvas-video-reference-limits text-[11px] leading-5" style={{ color: theme.node.muted }}>
                            最多 {capabilities.referenceLimits.images} 图 + {capabilities.referenceLimits.videos} 视频 + {capabilities.referenceLimits.audios} 音频
                            <span className="canvas-video-reference-duration-limits block">
                                视频累计不超过 {capabilities.referenceLimits.totalVideoDurationSeconds} 秒 · 音频累计不超过 {capabilities.referenceLimits.totalAudioDurationSeconds} 秒
                            </span>
                        </div>
                    </CanvasGenerationSettingsSection>
                ) : null}

                {capabilities.supportedTools.includes("web_search") ? (
                    <CanvasGenerationSettingsSection label="联网搜索" theme={theme}>
                        <UnsupportedSetting reason="接口支持；独立计费尚未配置，暂不开放" />
                    </CanvasGenerationSettingsSection>
                ) : null}

                {capabilities.watermarkCapability === "unsupported" ? (
                    <CanvasGenerationSettingsSection label="AI 水印" theme={theme}>
                        <UnsupportedSetting reason="该模型不支持水印控制，结果由模型服务商决定" />
                    </CanvasGenerationSettingsSection>
                ) : null}

                <CanvasGenerationSettingsSection label="生成音频" theme={theme}>
                    {capabilities.supportsGeneratedAudio ? (
                        <div className="canvas-video-audio-grid grid grid-cols-2 gap-2">
                            <OptionButton selected={generateAudio} theme={theme} onClick={() => onConfigChange("videoGenerateAudio", "true")}>
                                开启
                            </OptionButton>
                            <OptionButton selected={!generateAudio} theme={theme} onClick={() => onConfigChange("videoGenerateAudio", "false")}>
                                关闭
                            </OptionButton>
                        </div>
                    ) : (
                        <UnsupportedSetting reason={capabilities.unsupportedReasons.generatedAudio || "当前模型不支持同步生成音频"} />
                    )}
                </CanvasGenerationSettingsSection>

                <CanvasGenerationSettingsSection label="生成数量" theme={theme}>
                    <div className="canvas-video-count-grid grid gap-2">
                        {capabilities.outputCounts.map((value) => (
                            <OptionButton key={value} selected={generationCount === value} theme={theme} onClick={() => onConfigChange("count", String(value))}>
                                {value}个
                            </OptionButton>
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>
            </div>
        </ImageSettingsTheme>
    );
}

type VideoDurationInputProps = {
    duration: number;
    durationOptions: readonly number[];
    customDurationRange?: Readonly<{ min: number; max: number }>;
    theme: CanvasTheme;
    onCommit: (value: number) => void;
};

function VideoDurationInput({ duration, durationOptions, customDurationRange, theme, onCommit }: VideoDurationInputProps) {
    const inputId = useId();
    const errorId = `${inputId}-error`;
    const [draft, setDraft] = useState(String(duration));
    const [error, setError] = useState("");
    const discreteDurationIndex = Math.max(0, durationOptions.indexOf(duration));
    const sliderMin = customDurationRange?.min ?? 0;
    const sliderMax = customDurationRange?.max ?? Math.max(0, durationOptions.length - 1);
    const sliderValue = customDurationRange ? duration : discreteDurationIndex;

    useEffect(() => {
        setDraft(String(duration));
        setError("");
    }, [duration]);

    const commitDraft = () => {
        const validation = validateVideoDuration(draft, durationOptions, customDurationRange);
        if (!validation.valid) {
            setError(validation.message);
            return;
        }
        setDraft(String(validation.value));
        setError("");
        if (validation.value !== duration) onCommit(validation.value);
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
        if (event.key !== "Enter") return;
        event.preventDefault();
        event.currentTarget.blur();
    };

    return (
        <div className="canvas-video-duration-control space-y-1.5">
            <div className="canvas-video-duration-row flex h-8 items-center gap-3">
                <input
                    className="canvas-video-duration-slider min-w-0 flex-1 cursor-pointer accent-current"
                    type="range"
                    min={sliderMin}
                    max={sliderMax}
                    step={1}
                    value={sliderValue}
                    aria-label="调整视频时长"
                    style={{ color: theme.node.text }}
                    onChange={(event) => {
                        const sliderSelection = Number(event.target.value);
                        const nextDuration = customDurationRange ? sliderSelection : durationOptions[sliderSelection];
                        if (!Number.isSafeInteger(nextDuration)) return;
                        setDraft(String(nextDuration));
                        setError("");
                        if (nextDuration !== duration) onCommit(nextDuration);
                    }}
                />
                <div className="canvas-video-duration-value relative flex h-7 w-[62px] shrink-0 items-center">
                    <input
                        id={inputId}
                        className="canvas-video-duration-input h-7 w-full rounded-md border-0 px-2 pr-5 text-center text-[12px] font-semibold tabular-nums outline-none transition focus-visible:ring-1 focus-visible:ring-current/25"
                        type="text"
                        inputMode="numeric"
                        autoComplete="off"
                        value={draft}
                        aria-label="视频时长"
                        aria-invalid={Boolean(error)}
                        aria-describedby={error ? errorId : undefined}
                        style={{ background: theme.node.fill, boxShadow: error ? "inset 0 0 0 1px #ef4444" : "none", color: theme.node.text }}
                        onChange={(event) => {
                            const nextDraft = event.target.value.trim();
                            if (nextDraft === "" || /^\d+$/.test(nextDraft)) {
                                setDraft(nextDraft);
                                setError("");
                            }
                        }}
                        onBlur={commitDraft}
                        onKeyDown={handleKeyDown}
                    />
                    <span className="canvas-video-duration-unit pointer-events-none absolute right-1.5 text-[11px] font-medium" style={{ color: theme.node.muted }}>
                        s
                    </span>
                </div>
            </div>
            {error ? (
                <div id={errorId} className="canvas-video-duration-error text-[11px] leading-4 text-red-500" role="alert">
                    {error}
                </div>
            ) : null}
        </div>
    );
}

type VideoDurationValidation = Readonly<{ valid: true; value: number } | { valid: false; message: string }>;

export function validateVideoDuration(value: string, durationOptions: readonly number[], customDurationRange?: Readonly<{ min: number; max: number }>): VideoDurationValidation {
    if (!/^\d+$/.test(value.trim())) return { valid: false, message: "请输入整数秒数" };
    const duration = Number(value);
    if (!Number.isSafeInteger(duration)) return { valid: false, message: "请输入有效的整数秒数" };
    if (customDurationRange) {
        if (duration < customDurationRange.min || duration > customDurationRange.max) {
            return { valid: false, message: `当前模型仅支持 ${customDurationRange.min}–${customDurationRange.max} 秒` };
        }
        return { valid: true, value: duration };
    }
    if (!durationOptions.includes(duration)) return { valid: false, message: `当前模型仅支持 ${durationOptions.join(" / ")} 秒` };
    return { valid: true, value: duration };
}

export function videoResolutionLabel(value: string) {
    const normalized = value.trim().toLowerCase();
    if (normalized === "768" || normalized === "768p") return "768P";
    if (normalized === "2k" || normalized === "4k" || normalized === "8k") return normalized.toUpperCase();
    return `${normalizeVideoResolution(value)}P`;
}

export function videoSizeLabel(value: string) {
    const ratio = normalizeSeedanceRatio(value);
    if (ratio === "adaptive") return "Auto";
    return seedanceRatioOptions.find((item) => item.value === ratio)?.label || ratio;
}

export function videoSecondsLabel(value: string) {
    const duration = Number(value);
    if (!Number.isSafeInteger(duration) || duration <= 0) throw new Error(`无效的视频时长：${value}`);
    return `${duration}s`;
}

function normalizeVideoCount(value: string, options: readonly number[]) {
    const count = Math.max(1, Math.floor(Math.abs(Number(value)) || 1));
    return options.reduce((nearest, option) => (Math.abs(option - count) < Math.abs(nearest - count) ? option : nearest), options[0]);
}

function UnsupportedSetting({ reason }: { reason: string }) {
    return (
        <div className="canvas-video-unsupported-setting" role="note">
            <span className="canvas-video-unsupported-label">不支持</span>
            <span className="canvas-video-unsupported-reason">{reason}</span>
        </div>
    );
}

function OptionButton({ selected, disabled = false, theme, onClick, children }: { selected: boolean; disabled?: boolean; theme: CanvasTheme; onClick: () => void; children: ReactNode }) {
    return (
        <CanvasGenerationSettingsOption
            active={selected}
            disabled={disabled}
            label={children}
            theme={theme}
            className="canvas-video-option h-8 cursor-pointer whitespace-nowrap px-1 text-[12px] font-medium leading-none disabled:cursor-not-allowed disabled:opacity-30"
            onClick={onClick}
        />
    );
}

function RatioOption({ label, ratio, selected, theme, onClick }: { label: string; ratio: string; selected: boolean; theme: CanvasTheme; onClick: () => void }) {
    return <CanvasGenerationSettingsRatioOption active={selected} label={label} ratio={ratio} theme={theme} className="canvas-video-ratio-option px-1 text-[12px] font-medium leading-none" onClick={onClick} />;
}
