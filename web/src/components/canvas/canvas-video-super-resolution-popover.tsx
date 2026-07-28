import { Sparkles } from "lucide-react";
import { Button, Popover, Switch, Tooltip } from "antd";

import { canvasThemes } from "@/lib/canvas-theme";
import {
    supportsVideoSuperResolution,
    videoSuperResolutionTargets,
    VIDEO_SUPER_RESOLUTION_SCENES,
    VIDEO_SUPER_RESOLUTION_VERSIONS,
} from "@/lib/video-super-resolution";
import { useThemeStore } from "@/stores/use-theme-store";
import type { AiConfig } from "@/stores/use-config-store";

type CanvasVideoSuperResolutionPopoverProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
};

export function CanvasVideoSuperResolutionPopover({ config, onConfigChange }: CanvasVideoSuperResolutionPopoverProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const supported = supportsVideoSuperResolution(config);
    const enabled = supported && config.videoSuperResolutionEnabled === "true";
    const targets = videoSuperResolutionTargets(config.vquality);

    const updateEnabled = (checked: boolean) => {
        if (checked && !targets.includes(config.videoSuperResolutionResolution)) {
            onConfigChange("videoSuperResolutionResolution", targets[0] || "");
        }
        onConfigChange("videoSuperResolutionEnabled", String(checked));
    };

    const trigger = (
        <Button
            type="text"
            disabled={!supported}
            className="canvas-video-super-resolution-trigger !grid !h-8 !w-8 shrink-0 !place-items-center !rounded-lg !border-0 !p-0 !shadow-none"
            style={{
                background: enabled ? theme.accent.primarySoft : "transparent",
                color: enabled ? theme.accent.primary : theme.node.muted,
            }}
            aria-label={supported ? "超分增强" : "当前模型不支持超分增强"}
            icon={<Sparkles className="size-3.5" />}
        />
    );

    if (!supported) {
        return <Tooltip title="当前模型不支持超分增强"><span className="canvas-video-super-resolution-disabled inline-flex">{trigger}</span></Tooltip>;
    }

    return (
        <Popover
            trigger="click"
            placement="top"
            arrow={false}
            rootClassName="canvas-video-super-resolution-popover"
            content={(
                <div className="canvas-video-super-resolution-panel w-[280px] space-y-3 p-1" style={{ color: theme.node.text }}>
                    <div className="canvas-video-super-resolution-heading flex items-center justify-between gap-3">
                        <div className="canvas-video-super-resolution-heading-copy min-w-0">
                            <div className="canvas-video-super-resolution-title text-[13px] font-semibold">超分增强</div>
                            <div className="canvas-video-super-resolution-description mt-0.5 text-[10px]" style={{ color: theme.node.muted }}>使用专有模型提升视频清晰度</div>
                        </div>
                        <Switch className="canvas-video-super-resolution-switch" size="small" checked={enabled} disabled={targets.length === 0} onChange={updateEnabled} />
                    </div>

                    {targets.length === 0 ? (
                        <div className="canvas-video-super-resolution-empty text-[11px]" style={{ color: theme.node.muted }}>当前分辨率没有更高的超分目标</div>
                    ) : (
                        <div className="canvas-video-super-resolution-targets grid grid-cols-3 gap-1.5">
                            {targets.map((value) => (
                                <button
                                    key={value}
                                    type="button"
                                    disabled={!enabled}
                                    className="canvas-video-super-resolution-target h-8 rounded-md border text-[11px] font-medium disabled:opacity-35"
                                    style={{
                                        borderColor: config.videoSuperResolutionResolution === value ? theme.node.text : theme.node.stroke,
                                        background: config.videoSuperResolutionResolution === value ? "rgb(255 255 255 / 12%)" : "transparent",
                                    }}
                                    onClick={() => onConfigChange("videoSuperResolutionResolution", value)}
                                >
                                    {value.toUpperCase()}
                                </button>
                            ))}
                        </div>
                    )}

                    <div className="canvas-video-super-resolution-selects grid grid-cols-2 gap-2">
                        <select
                            className="canvas-video-super-resolution-scene h-8 min-w-0 rounded-md border bg-transparent px-2 text-[11px] outline-none disabled:opacity-35"
                            style={{ borderColor: theme.node.stroke }}
                            disabled={!enabled}
                            value={config.videoSuperResolutionScene}
                            aria-label="超分场景"
                            onChange={(event) => onConfigChange("videoSuperResolutionScene", event.target.value)}
                        >
                            {VIDEO_SUPER_RESOLUTION_SCENES.map((item) => <option className="canvas-video-super-resolution-scene-option" key={item.value} value={item.value}>{item.label}</option>)}
                        </select>
                        <select
                            className="canvas-video-super-resolution-version h-8 min-w-0 rounded-md border bg-transparent px-2 text-[11px] outline-none disabled:opacity-35"
                            style={{ borderColor: theme.node.stroke }}
                            disabled={!enabled}
                            value={config.videoSuperResolutionVersion}
                            aria-label="超分版本"
                            onChange={(event) => onConfigChange("videoSuperResolutionVersion", event.target.value)}
                        >
                            {VIDEO_SUPER_RESOLUTION_VERSIONS.map((item) => <option className="canvas-video-super-resolution-version-option" key={item.value} value={item.value}>{item.label}</option>)}
                        </select>
                    </div>

                    <label className="canvas-video-super-resolution-fps flex h-8 items-center gap-2 text-[11px]">
                        <span className="canvas-video-super-resolution-fps-label shrink-0" style={{ color: theme.node.muted }}>输出帧率</span>
                        <input
                            className="canvas-video-super-resolution-fps-input min-w-0 flex-1 rounded-md border bg-transparent px-2 py-1 outline-none disabled:opacity-35"
                            style={{ borderColor: theme.node.stroke }}
                            type="number"
                            min={1}
                            max={120}
                            disabled={!enabled}
                            placeholder="1–120，可选"
                            value={config.videoSuperResolutionFps}
                            onChange={(event) => onConfigChange("videoSuperResolutionFps", event.target.value)}
                        />
                    </label>
                </div>
            )}
        >
            {trigger}
        </Popover>
    );
}
