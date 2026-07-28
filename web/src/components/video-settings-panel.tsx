import { type ReactNode } from "react";

import { ImageSettingsTheme } from "@/components/image-settings-panel";
import { type CanvasTheme } from "@/lib/canvas-theme";
import {
    boolConfig,
    isSeedanceFastModel,
    isSeedanceVideoConfig,
    normalizeSeedanceRatio,
    normalizeSeedanceResolution,
    seedanceDurationOptions,
    seedanceRatioOptions,
} from "@/lib/seedance-video";
import { normalizeVideoDuration, normalizeVideoResolution } from "@/lib/video-generation-options";
import { modelOptionName, type AiConfig } from "@/stores/use-config-store";

const videoResolutionOptions = [
    { value: "480p", label: "480P" },
    { value: "720p", label: "720P" },
    { value: "1080p", label: "1080P" },
    { value: "4k", label: "4K" },
] as const;

const videoRatioOptions = [
    { value: "adaptive", label: "Auto" },
    { value: "16:9", label: "16:9" },
    { value: "4:3", label: "4:3" },
    { value: "1:1", label: "1:1" },
    { value: "3:4", label: "3:4" },
    { value: "9:16", label: "9:16" },
    { value: "21:9", label: "21:9" },
] as const;

const videoCountOptions = [1, 2, 4] as const;

type VideoSettingsPanelProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    theme: CanvasTheme;
    showTitle?: boolean;
    className?: string;
};

export function VideoSettingsPanel({ config, onConfigChange, theme, showTitle = true, className = "w-[316px] space-y-3" }: VideoSettingsPanelProps) {
    const model = modelOptionName(config.model || config.videoModel);
    const seedance = isSeedanceVideoConfig(config);
    const resolution = seedance ? normalizeSeedanceResolution(config.vquality, model) : `${normalizeVideoResolution(config.vquality)}p`;
    const ratio = normalizeSeedanceRatio(config.size);
    const duration = Number(normalizeVideoDuration(config.videoSeconds));
    const durationIndex = Math.max(0, seedanceDurationOptions.indexOf(duration as (typeof seedanceDurationOptions)[number]));
    const generateAudio = boolConfig(config.videoGenerateAudio, true);
    const generationCount = normalizeVideoCount(config.count);

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={className} style={{ color: theme.node.text }} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? <div className="canvas-video-settings-title text-sm font-semibold">视频设置</div> : null}

                <SettingGroup title="比例" color={theme.node.muted}>
                    <div className="canvas-video-ratio-grid grid grid-cols-5 gap-2">
                        {videoRatioOptions.map((item) => (
                            <RatioOption
                                key={item.value}
                                label={item.label}
                                ratio={item.value}
                                selected={ratio === item.value}
                                theme={theme}
                                onClick={() => onConfigChange("size", item.value)}
                            />
                        ))}
                    </div>
                </SettingGroup>

                <SettingGroup title="清晰度" color={theme.node.muted}>
                    <div className="canvas-video-resolution-grid grid grid-cols-4 gap-2">
                        {videoResolutionOptions.map((item) => {
                            const disabled = item.value === "4k" || (item.value === "1080p" && isSeedanceFastModel(model));
                            return (
                                <OptionButton
                                    key={item.value}
                                    selected={resolution === item.value}
                                    disabled={disabled}
                                    theme={theme}
                                    onClick={() => onConfigChange("vquality", item.value)}
                                >
                                    {item.label}
                                </OptionButton>
                            );
                        })}
                    </div>
                </SettingGroup>

                <SettingGroup title="视频时长" color={theme.node.muted}>
                    <div className="canvas-video-duration-row flex h-8 items-center gap-3">
                        <input
                            className="canvas-video-duration-slider min-w-0 flex-1 cursor-pointer"
                            type="range"
                            min={0}
                            max={seedanceDurationOptions.length - 1}
                            step={1}
                            value={durationIndex}
                            aria-label="视频时长"
                            onChange={(event) => onConfigChange("videoSeconds", String(seedanceDurationOptions[Number(event.target.value)]))}
                        />
                        <output className="canvas-video-duration-value grid h-7 w-12 shrink-0 place-items-center rounded-md bg-white/5 text-[12px] font-semibold">
                            {duration}
                        </output>
                        <span className="canvas-video-duration-unit -ml-2 text-[11px]" style={{ color: theme.node.muted }}>s</span>
                    </div>
                </SettingGroup>

                <SettingGroup title="生成音频" color={theme.node.muted}>
                    <div className="canvas-video-audio-grid grid grid-cols-2 gap-2">
                        <OptionButton selected={generateAudio} theme={theme} onClick={() => onConfigChange("videoGenerateAudio", "true")}>开启</OptionButton>
                        <OptionButton selected={!generateAudio} theme={theme} onClick={() => onConfigChange("videoGenerateAudio", "false")}>关闭</OptionButton>
                    </div>
                </SettingGroup>

                <SettingGroup title="生成数量" color={theme.node.muted}>
                    <div className="canvas-video-count-grid grid grid-cols-3 gap-2">
                        {videoCountOptions.map((value) => (
                            <OptionButton key={value} selected={generationCount === value} theme={theme} onClick={() => onConfigChange("count", String(value))}>
                                {value}个
                            </OptionButton>
                        ))}
                    </div>
                </SettingGroup>
            </div>
        </ImageSettingsTheme>
    );
}

export function videoResolutionLabel(value: string) {
    return `${normalizeVideoResolution(value)}P`;
}

export function videoSizeLabel(value: string) {
    const ratio = normalizeSeedanceRatio(value);
    if (ratio === "adaptive") return "Auto";
    return seedanceRatioOptions.find((item) => item.value === ratio)?.label || ratio;
}

export function videoSecondsLabel(value: string) {
    return `${normalizeVideoDuration(value)}s`;
}

function normalizeVideoCount(value: string) {
    const count = Math.max(1, Math.floor(Math.abs(Number(value)) || 1));
    return videoCountOptions.reduce((nearest, option) => Math.abs(option - count) < Math.abs(nearest - count) ? option : nearest, videoCountOptions[0]);
}

function OptionButton({
    selected,
    disabled = false,
    theme,
    onClick,
    children,
}: {
    selected: boolean;
    disabled?: boolean;
    theme: CanvasTheme;
    onClick: () => void;
    children: ReactNode;
}) {
    return (
        <button
            type="button"
            data-selected={selected}
            disabled={disabled}
            className="canvas-video-option-button h-8 cursor-pointer whitespace-nowrap rounded-lg border px-1 text-[12px] font-medium leading-none transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-30"
            style={{
                background: selected ? "rgb(255 255 255 / 12%)" : "transparent",
                borderColor: selected ? theme.node.text : theme.node.stroke,
                color: selected ? theme.node.text : theme.node.muted,
            }}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onClick}
        >
            {children}
        </button>
    );
}

function RatioOption({
    label,
    ratio,
    selected,
    theme,
    onClick,
}: {
    label: string;
    ratio: string;
    selected: boolean;
    theme: CanvasTheme;
    onClick: () => void;
}) {
    const preview = ratioPreview(ratio);
    return (
        <button
            type="button"
            data-selected={selected}
            className="canvas-video-ratio-option flex h-16 min-w-0 cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border px-1 text-[12px] font-medium leading-none transition hover:brightness-110"
            style={{
                background: selected ? "rgb(255 255 255 / 12%)" : "transparent",
                borderColor: selected ? theme.node.text : theme.node.stroke,
                color: selected ? theme.node.text : theme.node.muted,
            }}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onClick}
        >
            <span className="canvas-video-ratio-preview grid h-4 place-items-center">
                {preview ? <span className="canvas-video-ratio-preview-shape rounded-[2px] border" style={{ width: preview.width, height: preview.height, borderColor: "currentColor" }} /> : <span className="canvas-video-ratio-auto-mark text-[9px] opacity-70">A</span>}
            </span>
            <span className="canvas-video-ratio-label whitespace-nowrap">{label}</span>
        </button>
    );
}

function SettingGroup({ title, color, children }: { title: string; color: string; children: ReactNode }) {
    return (
        <section className="canvas-video-setting-group space-y-2">
            <div className="canvas-video-setting-label text-[12px] font-medium leading-4" style={{ color }}>{title}</div>
            {children}
        </section>
    );
}

function ratioPreview(ratio: string) {
    if (ratio === "16:9") return { width: 17, height: 10 };
    if (ratio === "9:16") return { width: 9, height: 16 };
    if (ratio === "1:1") return { width: 13, height: 13 };
    if (ratio === "4:3") return { width: 16, height: 12 };
    if (ratio === "3:4") return { width: 12, height: 16 };
    if (ratio === "21:9") return { width: 18, height: 8 };
    return null;
}
