import { type ReactNode } from "react";
import { Switch } from "antd";

import { ImageSettingsTheme } from "@/components/image-settings-panel";
import { boolConfig, isSeedanceFastModel, isSeedanceVideoConfig, normalizeSeedanceDuration, normalizeSeedanceRatio, normalizeSeedanceResolution, seedanceDurationOptions, seedanceRatioOptions, seedanceResolutionOptions } from "@/lib/seedance-video";
import { type CanvasTheme } from "@/lib/canvas-theme";
import { normalizeVideoDuration, normalizeVideoResolution, VIDEO_DURATION_OPTIONS, VIDEO_RESOLUTION_OPTIONS } from "@/lib/video-generation-options";
import { supportsVideoSuperResolution, videoSuperResolutionTargets, VIDEO_SUPER_RESOLUTION_SCENES, VIDEO_SUPER_RESOLUTION_VERSIONS } from "@/lib/video-super-resolution";
import { modelOptionName, type AiConfig } from "@/stores/use-config-store";

const resolutionOptions = VIDEO_RESOLUTION_OPTIONS.map((value) => ({ value: String(value), label: `${value}P` }));

const sizeOptions = [
    { value: "1280x720", label: "横屏", width: 1280, height: 720 },
    { value: "720x1280", label: "竖屏", width: 720, height: 1280 },
    { value: "1024x1024", label: "方形", width: 1024, height: 1024 },
    { value: "1792x1024", label: "宽屏", width: 1792, height: 1024 },
    { value: "1024x1792", label: "长图", width: 1024, height: 1792 },
    { value: "auto", label: "auto", width: 0, height: 0 },
];

const secondOptions = VIDEO_DURATION_OPTIONS;

type VideoSettingsPanelProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    theme: CanvasTheme;
    showTitle?: boolean;
    className?: string;
};

export function VideoSettingsPanel({ config, onConfigChange, theme, showTitle = true, className = "w-[292px] space-y-3" }: VideoSettingsPanelProps) {
    if (isSeedanceVideoConfig(config)) {
        return <SeedanceVideoSettingsPanel config={config} onConfigChange={onConfigChange} theme={theme} showTitle={showTitle} className={className} />;
    }

    const seconds = normalizeVideoDuration(config.videoSeconds);
    const size = normalizeVideoSizeValue(config.size);
    const dimensions = readSizeDimensions(size);
    const resolution = normalizeVideoResolutionValue(config.vquality);
    const updateDimension = (key: "width" | "height", value: number | null) => {
        const next = Math.max(1, Math.floor(value || dimensions[key] || 720));
        onConfigChange("size", `${key === "width" ? next : dimensions.width}x${key === "height" ? next : dimensions.height}`);
    };

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={className} style={{ color: theme.node.text }} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? <div className="text-sm font-semibold">视频设置</div> : null}
                <SettingGroup title="清晰度" color={theme.node.muted}>
                    <div className="grid grid-cols-3 gap-1.5">
                        {resolutionOptions.map((item) => (
                            <OptionPill key={item.value} selected={resolution === item.value} theme={theme} onClick={() => onConfigChange("vquality", item.value)}>
                                {item.label}
                            </OptionPill>
                        ))}
                    </div>
                </SettingGroup>
                <SettingGroup title="尺寸" color={theme.node.muted}>
                    <div className="grid grid-cols-[1fr_auto_1fr] items-center gap-1.5">
                        <DimensionInput prefix="W" value={dimensions.width} disabled={size === "auto"} theme={theme} onChange={(value) => updateDimension("width", value)} />
                        <span className="text-xs opacity-45">×</span>
                        <DimensionInput prefix="H" value={dimensions.height} disabled={size === "auto"} theme={theme} onChange={(value) => updateDimension("height", value)} />
                    </div>
                    <div className="grid grid-cols-3 gap-1.5">
                        {sizeOptions.map((item) => (
                            <button
                                key={item.value}
                                type="button"
                                data-selected={size === item.value}
                                className="flex h-8 cursor-pointer items-center justify-center gap-1.5 rounded-md border px-1 text-[11px] font-medium transition hover:opacity-80"
                                style={{ background: size === item.value ? theme.accent.primarySoft : "transparent", borderColor: size === item.value ? theme.accent.primary : theme.node.stroke, color: size === item.value ? theme.accent.primary : theme.node.text }}
                                onMouseDown={(event) => event.stopPropagation()}
                                onClick={() => onConfigChange("size", item.value)}
                            >
                                <SizePreview width={item.width} height={item.height} color={size === item.value ? theme.accent.primary : theme.node.text} />
                                <span>{item.label}</span>
                            </button>
                        ))}
                    </div>
                </SettingGroup>
                <SettingGroup title="秒数" color={theme.node.muted}>
                    <div className="grid grid-cols-4 gap-1.5">
                        {secondOptions.map((value) => (
                            <OptionPill key={value} selected={seconds === String(value)} theme={theme} onClick={() => onConfigChange("videoSeconds", String(value))}>
                                {value}s
                            </OptionPill>
                        ))}
                    </div>
                </SettingGroup>
            </div>
        </ImageSettingsTheme>
    );
}

function SeedanceVideoSettingsPanel({ config, onConfigChange, theme, showTitle, className }: VideoSettingsPanelProps) {
    const model = modelOptionName(config.model || config.videoModel);
    const resolution = normalizeSeedanceResolution(config.vquality, model);
    const ratio = normalizeSeedanceRatio(config.size);
    const duration = normalizeSeedanceDuration(config.videoSeconds);
    const generateAudio = boolConfig(config.videoGenerateAudio, true);
    const watermark = boolConfig(config.videoWatermark, false);
    const supportsSuperResolution = supportsVideoSuperResolution(config);
    const superResolutionEnabled = config.videoSuperResolutionEnabled === "true";
    const superResolutionTargets = videoSuperResolutionTargets(resolution);
    const enableSuperResolution = (checked: boolean) => {
        if (checked && !superResolutionTargets.includes(config.videoSuperResolutionResolution)) {
            onConfigChange("videoSuperResolutionResolution", superResolutionTargets[0] || "");
        }
        onConfigChange("videoSuperResolutionEnabled", String(checked));
    };

    return (
        <ImageSettingsTheme theme={theme}>
            <div className={className} style={{ color: theme.node.text }} onMouseDown={(event) => event.stopPropagation()}>
                {showTitle ? <div className="text-sm font-semibold">视频设置</div> : null}
                <SettingGroup title="分辨率" color={theme.node.muted}>
                    <div className="grid grid-cols-3 gap-1.5">
                        {seedanceResolutionOptions.map((item) => {
                            const disabled = item.value === "1080p" && isSeedanceFastModel(model);
                            return (
                                <OptionPill key={item.value} selected={resolution === item.value} disabled={disabled} theme={theme} onClick={() => onConfigChange("vquality", item.value)}>
                                    {item.label}
                                </OptionPill>
                            );
                        })}
                    </div>
                    {isSeedanceFastModel(model) ? <div className="text-[10px] leading-4 opacity-55">fast 模型自动使用 720P</div> : null}
                </SettingGroup>
                <SettingGroup title="比例" color={theme.node.muted}>
                    <div className="grid grid-cols-4 gap-1.5">
                        {seedanceRatioOptions.map((item) => (
                            <button
                                key={item.value}
                                type="button"
                                data-selected={ratio === item.value}
                                className="flex h-11 min-w-0 cursor-pointer flex-col items-center justify-center gap-0.5 rounded-md border px-1 text-[10px] font-medium leading-none transition hover:opacity-80"
                                style={{ background: ratio === item.value ? theme.accent.primarySoft : "transparent", borderColor: ratio === item.value ? theme.accent.primary : theme.node.stroke, color: ratio === item.value ? theme.accent.primary : theme.node.text }}
                                onMouseDown={(event) => event.stopPropagation()}
                                onClick={() => onConfigChange("size", item.value)}
                            >
                                <span className="grid h-4 place-items-center">
                                    <SizePreview width={ratioPreview(item.value).width} height={ratioPreview(item.value).height} color={ratio === item.value ? theme.accent.primary : theme.node.text} />
                                </span>
                                <span className="whitespace-nowrap">{item.label}</span>
                            </button>
                        ))}
                    </div>
                </SettingGroup>
                <SettingGroup title="时长" color={theme.node.muted}>
                    <div className="grid grid-cols-4 gap-1.5">
                        {seedanceDurationOptions.map((value) => (
                            <OptionPill key={value} selected={duration === value} theme={theme} onClick={() => onConfigChange("videoSeconds", String(value))}>
                                {value}s
                            </OptionPill>
                        ))}
                    </div>
                </SettingGroup>
                <SettingGroup title="输出" color={theme.node.muted}>
                    <div className="grid grid-cols-2 gap-3 rounded-md border px-2" style={{ borderColor: theme.node.stroke }}>
                        <SwitchRow label="生成声音" checked={generateAudio} theme={theme} onChange={(checked) => onConfigChange("videoGenerateAudio", String(checked))} />
                        <SwitchRow label="添加水印" checked={watermark} theme={theme} onChange={(checked) => onConfigChange("videoWatermark", String(checked))} />
                    </div>
                </SettingGroup>
                {supportsSuperResolution ? (
                    <SettingGroup title="超分增强" color={theme.node.muted}>
                        <div className="space-y-2">
                            <SwitchRow label="启用专有超分模型" checked={superResolutionEnabled} disabled={superResolutionTargets.length === 0} theme={theme} onChange={enableSuperResolution} />
                            {superResolutionTargets.length === 0 ? <div className="text-[10px] opacity-55">当前基础分辨率已无可用的更高超分目标</div> : null}
                            {superResolutionEnabled && superResolutionTargets.length > 0 ? (
                                <div className="space-y-2">
                                    <div className="grid grid-cols-4 gap-1.5">
                                        {superResolutionTargets.map((value) => (
                                            <OptionPill key={value} selected={config.videoSuperResolutionResolution === value} theme={theme} onClick={() => onConfigChange("videoSuperResolutionResolution", value)}>
                                                {value.toUpperCase()}
                                            </OptionPill>
                                        ))}
                                    </div>
                                    <div className="grid grid-cols-2 gap-1.5">
                                        <select className="h-8 rounded-md border bg-transparent px-2 text-[11px] outline-none" style={{ borderColor: theme.node.stroke }} value={config.videoSuperResolutionScene} onChange={(event) => onConfigChange("videoSuperResolutionScene", event.target.value)}>
                                            {VIDEO_SUPER_RESOLUTION_SCENES.map((item) => <option className="video-super-resolution-scene-option" key={item.value} value={item.value}>{item.label}</option>)}
                                        </select>
                                        <select className="h-8 rounded-md border bg-transparent px-2 text-[11px] outline-none" style={{ borderColor: theme.node.stroke }} value={config.videoSuperResolutionVersion} onChange={(event) => onConfigChange("videoSuperResolutionVersion", event.target.value)}>
                                            {VIDEO_SUPER_RESOLUTION_VERSIONS.map((item) => <option className="video-super-resolution-version-option" key={item.value} value={item.value}>{item.label}</option>)}
                                        </select>
                                    </div>
                                    <label className="flex h-8 items-center gap-2 text-[11px]">
                                        <span className="opacity-60">输出帧率</span>
                                        <input className="min-w-0 flex-1 rounded-md border bg-transparent px-2 py-1 outline-none" style={{ borderColor: theme.node.stroke }} type="number" min={1} max={120} placeholder="可选，1–120" value={config.videoSuperResolutionFps} onChange={(event) => onConfigChange("videoSuperResolutionFps", event.target.value)} />
                                    </label>
                                    {config.videoSuperResolutionVersion === "professional" ? <div className="text-[10px] opacity-55">专业版成本约为标准版的 10 倍，请确认套餐计费配置。</div> : null}
                                </div>
                            ) : null}
                        </div>
                    </SettingGroup>
                ) : null}
            </div>
        </ImageSettingsTheme>
    );
}

export function videoResolutionLabel(value: string) {
    return `${normalizeVideoResolutionValue(value)}P`;
}

export function videoSizeLabel(value: string) {
    const ratio = normalizeSeedanceRatio(value);
    if (value === "adaptive" || value === "auto") return "自适应";
    if (ratio === value) return seedanceRatioOptions.find((item) => item.value === ratio)?.label || ratio;
    const size = normalizeVideoSizeValue(value);
    return sizeOptions.find((item) => item.value === size)?.label || size;
}

export function videoSecondsLabel(value: string) {
    return `${normalizeVideoDuration(value)}s`;
}

export function normalizeVideoSizeValue(value: string) {
    if (value === "auto") return "auto";
    if (/^\d+x\d+$/.test(value || "")) return value;
    return ["9:16", "2:3", "3:4"].includes(value) ? "720x1280" : "1280x720";
}

export function normalizeVideoResolutionValue(value: string) {
    return normalizeVideoResolution(value);
}

function OptionPill({ selected, disabled = false, theme, onClick, children }: { selected: boolean; disabled?: boolean; theme: CanvasTheme; onClick: () => void; children: ReactNode }) {
    return (
        <button type="button" data-selected={selected} disabled={disabled} className="h-8 cursor-pointer whitespace-nowrap rounded-md border px-1 text-[11px] font-medium leading-none transition hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-35" style={{ background: selected ? theme.accent.primarySoft : "transparent", borderColor: selected ? theme.accent.primary : theme.node.stroke, color: selected ? theme.accent.primary : theme.node.text }} onMouseDown={(event) => event.stopPropagation()} onClick={onClick}>
            {children}
        </button>
    );
}

function SettingGroup({ title, color, children }: { title: string; color: string; children: ReactNode }) {
    return (
        <div className="canvas-video-setting-group space-y-1.5">
            <div className="canvas-video-setting-label text-[10px] font-semibold" style={{ color }}>
                {title}
            </div>
            {children}
        </div>
    );
}

function DimensionInput({ prefix, value, disabled, theme, onChange }: { prefix: string; value: number; disabled: boolean; theme: CanvasTheme; onChange: (value: number | null) => void }) {
    return (
        <label className="flex h-8 overflow-hidden rounded-md border text-[11px]" style={{ background: theme.node.fill, borderColor: theme.node.stroke, color: theme.node.text, opacity: disabled ? 0.55 : 1 }}>
            <span className="grid w-7 place-items-center" style={{ color: theme.node.muted }}>
                {prefix}
            </span>
            <input type="number" min={1} disabled={disabled} className="min-w-0 flex-1 bg-transparent px-2 outline-none [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none" value={value || ""} onChange={(event) => onChange(Number(event.target.value) || null)} onMouseDown={(event) => event.stopPropagation()} />
        </label>
    );
}

function SizePreview({ width, height, color }: { width: number; height: number; color: string }) {
    if (!width || !height) return null;
    const longSide = Math.max(width, height);
    const previewWidth = Math.max(7, Math.round((width / longSide) * 16));
    const previewHeight = Math.max(7, Math.round((height / longSide) * 16));
    return <span className="shrink-0 rounded-[2px] border" style={{ width: previewWidth, height: previewHeight, borderColor: color }} />;
}

function ratioPreview(ratio: string) {
    if (ratio === "9:16") return { width: 9, height: 16 };
    if (ratio === "1:1") return { width: 1, height: 1 };
    if (ratio === "4:3") return { width: 4, height: 3 };
    if (ratio === "3:4") return { width: 3, height: 4 };
    if (ratio === "21:9") return { width: 21, height: 9 };
    if (ratio === "adaptive") return { width: 0, height: 0 };
    return { width: 16, height: 9 };
}

function SwitchRow({ label, checked, disabled = false, theme, onChange }: { label: string; checked: boolean; disabled?: boolean; theme: CanvasTheme; onChange: (checked: boolean) => void }) {
    return (
        <div className="flex h-8 items-center justify-between gap-2">
            <span className="min-w-0 whitespace-nowrap text-[11px]" style={{ color: theme.node.text }}>
                {label}
            </span>
            <span className="shrink-0" onMouseDown={(event) => event.stopPropagation()}>
                <Switch className="video-settings-switch" size="small" checked={checked} disabled={disabled} onChange={onChange} />
            </span>
        </div>
    );
}

function readSizeDimensions(size: string) {
    if (size === "auto") return { width: 0, height: 0 };
    const match = size.match(/^(\d+)x(\d+)$/);
    return { width: Number(match?.[1]) || 1280, height: Number(match?.[2]) || 720 };
}
