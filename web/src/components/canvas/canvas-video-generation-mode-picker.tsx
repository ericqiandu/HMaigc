import { useState } from "react";
import { Box, ChevronDown, FileText, Images, Image as ImageIcon, PanelsTopLeft } from "lucide-react";
import { Popover, Tooltip } from "antd";

import { canvasThemes } from "@/lib/canvas-theme";
import { resolveVideoGenerationMode, videoModeMetadataPatch, type VideoReferenceCounts } from "@/lib/canvas/canvas-video-generation-mode";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasNodeMetadata, CanvasVideoGenerationMode } from "@/types/canvas";

type VideoFrameOption = { nodeId: string };

type CanvasVideoGenerationModePickerProps = {
    metadata?: CanvasNodeMetadata;
    frameOptions: VideoFrameOption[];
    referenceCounts: VideoReferenceCounts;
    supportedModes?: readonly CanvasVideoGenerationMode[];
    onMetadataChange: (patch: Partial<CanvasNodeMetadata>) => void;
};

const modes = [
    { value: "text", label: "文生视频", icon: FileText },
    { value: "omni_reference", label: "全能参考", icon: Box },
    { value: "image", label: "图生视频", icon: ImageIcon },
    { value: "first_last_frame", label: "首尾帧", icon: Images },
    { value: "image_reference", label: "图片参考", icon: PanelsTopLeft },
] satisfies Array<{ value: CanvasVideoGenerationMode; label: string; icon: typeof FileText }>;

export function CanvasVideoGenerationModePicker({ metadata, frameOptions, referenceCounts, supportedModes, onMetadataChange }: CanvasVideoGenerationModePickerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [open, setOpen] = useState(false);
    const mode = resolveVideoGenerationMode(metadata);
    const current = modes.find((item) => item.value === mode) || modes[0];
    const CurrentIcon = current.icon;

    const disabledReason = (value: CanvasVideoGenerationMode) => {
        if (supportedModes && !supportedModes.includes(value)) return "当前模型不支持此生成模式";
        if (value === "image" && frameOptions.length < 1) return "请先连接一张图片";
        if (value === "first_last_frame" && frameOptions.length < 2) return "请先连接两张图片";
        if (value === "image_reference" && referenceCounts.image < 1) return "请先添加图片参考";
        if (value === "omni_reference" && referenceCounts.image + referenceCounts.video + referenceCounts.audio < 1) return "请先添加参考素材";
        return undefined;
    };

    const selectMode = (nextMode: CanvasVideoGenerationMode) => {
        if (disabledReason(nextMode)) return;
        onMetadataChange(videoModeMetadataPatch({ mode: nextMode, metadata, frameNodeIds: frameOptions.map((item) => item.nodeId), counts: referenceCounts }));
        setOpen(false);
    };

    const content = (
        <div className="canvas-video-mode-menu">
            <div className="canvas-video-mode-menu-title">视频生成模式</div>
            <div className="canvas-video-mode-list">
                {modes.map((item) => {
                    const Icon = item.icon;
                    const selected = item.value === mode;
                    const reason = disabledReason(item.value);
                    return (
                        <Tooltip key={item.value} title={reason} placement="right" mouseEnterDelay={0.2}>
                            <span className="canvas-video-mode-option-wrap">
                                <button type="button" className="canvas-video-mode-option" disabled={Boolean(reason)} aria-label={reason ? `${item.label}：${reason}` : item.label} data-selected={selected} onClick={() => selectMode(item.value)}>
                                    <Icon className="canvas-video-mode-option-icon" />
                                    <span className="canvas-video-mode-option-label">{item.label}</span>
                                </button>
                            </span>
                        </Tooltip>
                    );
                })}
            </div>
        </div>
    );

    return (
        <Popover open={open} onOpenChange={setOpen} trigger="click" placement="bottomLeft" content={content} rootClassName="canvas-overlay-popover canvas-video-mode-popover" styles={{ content: { padding: 0, background: theme.toolbar.panel } }}>
            <button type="button" className="canvas-video-mode-trigger canvas-media-control" aria-label={`视频生成模式：${current.label}`} aria-expanded={open}>
                <CurrentIcon className="canvas-video-mode-trigger-icon" />
                <span className="canvas-video-mode-trigger-label">{current.label}</span>
                <ChevronDown className="canvas-video-mode-trigger-chevron" />
            </button>
        </Popover>
    );
}
