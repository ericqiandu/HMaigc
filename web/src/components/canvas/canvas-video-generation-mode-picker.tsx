import { useState } from "react";
import { Check, ChevronDown, FileText, Images, Image as ImageIcon } from "lucide-react";
import { Popover } from "antd";

import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasNodeMetadata } from "@/types/canvas";

type VideoFrameOption = {
    nodeId: string;
};

type CanvasVideoGenerationModePickerProps = {
    metadata?: CanvasNodeMetadata;
    frameOptions: VideoFrameOption[];
    onMetadataChange: (patch: Partial<CanvasNodeMetadata>) => void;
};

type VideoGenerationMode = "text" | "image" | "firstLast";

const modes = [
    { value: "text", label: "文生视频", icon: FileText },
    { value: "image", label: "图生视频", icon: ImageIcon },
    { value: "firstLast", label: "首尾帧", icon: Images },
] as const;

export function CanvasVideoGenerationModePicker({ metadata, frameOptions, onMetadataChange }: CanvasVideoGenerationModePickerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [open, setOpen] = useState(false);
    const mode = resolveMode(metadata);
    const current = modes.find((item) => item.value === mode) || modes[0];
    const CurrentIcon = current.icon;

    const selectMode = (nextMode: VideoGenerationMode) => {
        if (nextMode === "text") {
            onMetadataChange({ videoStartFrameNodeId: undefined, videoEndFrameNodeId: undefined });
            setOpen(false);
            return;
        }
        const currentStart = metadata?.videoStartFrameNodeId;
        const firstFrame = frameOptions.find((item) => item.nodeId === currentStart)?.nodeId || frameOptions[0]?.nodeId;
        if (!firstFrame) return;
        if (nextMode === "image") {
            onMetadataChange({ videoStartFrameNodeId: firstFrame, videoEndFrameNodeId: undefined });
            setOpen(false);
            return;
        }
        const currentEnd = metadata?.videoEndFrameNodeId;
        const endFrame = frameOptions.find((item) => item.nodeId === currentEnd && item.nodeId !== firstFrame)?.nodeId || frameOptions.find((item) => item.nodeId !== firstFrame)?.nodeId;
        if (!endFrame) return;
        onMetadataChange({ videoStartFrameNodeId: firstFrame, videoEndFrameNodeId: endFrame });
        setOpen(false);
    };

    const content = (
        <div className="canvas-video-mode-menu w-[162px] p-1">
            <div className="canvas-video-mode-menu-title px-2 pb-1 pt-0.5">视频生成模式</div>
            {modes.map((item) => {
                const Icon = item.icon;
                const selected = item.value === mode;
                const disabled = item.value === "image" ? frameOptions.length < 1 : item.value === "firstLast" ? frameOptions.length < 2 : false;
                return (
                    <button
                        key={item.value}
                        type="button"
                        className="canvas-video-mode-option flex h-8 w-full items-center gap-2 px-2 text-left transition-colors"
                        disabled={disabled}
                        style={{ background: selected ? theme.toolbar.itemHover : "transparent", color: selected ? theme.node.text : theme.node.muted }}
                        onClick={() => selectMode(item.value)}
                    >
                        <Icon className="size-3.5 shrink-0" />
                        <span className="min-w-0 flex-1 truncate">{item.label}</span>
                        {selected ? <Check className="size-3.5 shrink-0" /> : null}
                    </button>
                );
            })}
        </div>
    );

    return (
        <Popover
            open={open}
            onOpenChange={setOpen}
            trigger="click"
            placement="topLeft"
            content={content}
            overlayClassName="canvas-video-mode-popover"
            styles={{ content: { padding: 0, background: theme.toolbar.panel, border: `1px solid ${theme.toolbar.border}` } }}
        >
            <button type="button" className="canvas-video-mode-trigger canvas-media-control inline-flex h-8 shrink-0 items-center gap-1 px-2 transition-colors" aria-label={`视频生成模式：${current.label}`}>
                <CurrentIcon className="size-3.5 shrink-0" />
                <span>{current.label}</span>
                <ChevronDown className="size-3 shrink-0 opacity-55" />
            </button>
        </Popover>
    );
}

function resolveMode(metadata?: CanvasNodeMetadata): VideoGenerationMode {
    if (metadata?.videoStartFrameNodeId && metadata.videoEndFrameNodeId) return "firstLast";
    if (metadata?.videoStartFrameNodeId) return "image";
    return "text";
}
