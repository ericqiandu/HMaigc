import { X } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAssistantReference } from "@/types/canvas";

export function CanvasAssistantReferenceChip({ item, label, onRemove }: { item: CanvasAssistantReference; label?: string; onRemove?: () => void }) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const title = (item.title || item.text || "文本节点").replace(/\s+/g, " ").trim();
    const marker = title.slice(0, 1) || "文";
    return (
        <div
            className="canvas-assistant-reference-chip group/chip relative inline-flex h-8 min-w-0 max-w-[190px] shrink-0 items-center gap-1.5 text-sm"
            style={{ color: theme.node.text }}
            title={title}
        >
            {item.dataUrl ? (
                <span className="canvas-assistant-reference-preview relative block size-7 shrink-0">
                    <img src={item.dataUrl} alt="" className="canvas-assistant-reference-image size-7 rounded-md object-cover" />
                    {label ? <span className="canvas-assistant-reference-index absolute left-0.5 top-0.5 rounded bg-black/60 px-1 py-0.5 text-[8px] font-medium leading-none text-white">{label}</span> : null}
                </span>
            ) : (
                <span
                    className="canvas-assistant-reference-marker grid size-7 shrink-0 place-items-center rounded-md text-xs font-medium"
                    style={{ background: theme.node.panel }}
                    aria-hidden="true"
                >
                    {marker}
                </span>
            )}
            <span className="canvas-assistant-reference-title min-w-0 truncate text-xs font-medium">{title}</span>
            {onRemove ? (
                <button
                    type="button"
                    className="canvas-assistant-reference-remove absolute -right-1 -top-1 grid size-4 place-items-center rounded-full border opacity-0 shadow-sm transition group-hover/chip:opacity-100"
                    style={{ background: theme.toolbar.panel, borderColor: theme.node.stroke }}
                    onClick={onRemove}
                    aria-label={`移除引用：${title}`}
                >
                    <X className="canvas-assistant-reference-remove-icon size-3" />
                </button>
            ) : null}
        </div>
    );
}
