import { useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Settings2, Volume2 } from "lucide-react";
import { Button } from "antd";

import { VideoSettingsPanel, videoSecondsLabel, videoSizeLabel } from "@/components/video-settings-panel";
import { canvasThemes } from "@/lib/canvas-theme";
import { normalizeVideoConfigForModel, resolveVideoModelCapabilities } from "@/lib/video-model-capabilities";
import { useThemeStore } from "@/stores/use-theme-store";
import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasVideoGenerationMode } from "@/types/canvas";

type CanvasVideoSettingsPopoverProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    buttonClassName?: string;
    placement?: "topLeft" | "top" | "topRight" | "bottomLeft" | "bottom" | "bottomRight";
    generationMode?: CanvasVideoGenerationMode;
};

export function CanvasVideoSettingsPopover({ config, onConfigChange, buttonClassName, placement = "topLeft", generationMode }: CanvasVideoSettingsPopoverProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const buttonRef = useRef<HTMLSpanElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const [open, setOpen] = useState(false);
    const [buttonRect, setButtonRect] = useState<DOMRect | null>(null);
    const capabilities = resolveVideoModelCapabilities(config);
    const normalizedConfig = normalizeVideoConfigForModel(config, generationMode);
    const resolutionLabel = capabilities.resolutions.find((option) => option.value === normalizedConfig.vquality)?.label || normalizedConfig.vquality.toUpperCase();
    const outputCount = Number(normalizedConfig.count);

    useEffect(() => {
        if (!open) return;
        const syncPosition = () => setButtonRect(buttonRef.current?.getBoundingClientRect() || null);
        const closeOnOutsidePointer = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (buttonRef.current?.contains(target) || panelRef.current?.contains(target)) return;
            setOpen(false);
        };

        syncPosition();
        window.addEventListener("resize", syncPosition);
        window.addEventListener("scroll", syncPosition, true);
        window.addEventListener("pointerdown", closeOnOutsidePointer, true);
        return () => {
            window.removeEventListener("resize", syncPosition);
            window.removeEventListener("scroll", syncPosition, true);
            window.removeEventListener("pointerdown", closeOnOutsidePointer, true);
        };
    }, [open]);

    const panel = open && buttonRect ? <VideoSettingsPortal buttonRect={buttonRect} panelRef={panelRef} placement={placement} theme={theme} config={config} onConfigChange={onConfigChange} generationMode={generationMode} /> : null;

    return (
        <>
            <span ref={buttonRef} className="inline-flex min-w-0">
                <Button
                    size="small"
                    type="text"
                    className={buttonClassName || "!h-8 !max-w-[170px] !justify-start !rounded-full !px-2.5"}
                    style={{ background: theme.node.fill, color: theme.node.text }}
                    icon={<Settings2 className="size-3.5" />}
                    onClick={() => setOpen((current) => !current)}
                >
                    <span className="inline-flex min-w-0 items-center gap-1 truncate">
                        <span className="truncate">
                            {videoSizeLabel(normalizedConfig.size)} · {resolutionLabel} · {videoSecondsLabel(normalizedConfig.videoSeconds)} · {outputCount}个
                        </span>
                        {capabilities.supportsGeneratedAudio && normalizedConfig.videoGenerateAudio !== "false" ? <Volume2 className="size-3.5 shrink-0" /> : null}
                    </span>
                </Button>
            </span>
            {panel}
        </>
    );
}

function VideoSettingsPortal({
    buttonRect,
    panelRef,
    placement,
    theme,
    config,
    onConfigChange,
    generationMode,
}: {
    buttonRect: DOMRect;
    panelRef: RefObject<HTMLDivElement | null>;
    placement: CanvasVideoSettingsPopoverProps["placement"];
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    generationMode?: CanvasVideoGenerationMode;
}) {
    const gap = 8;
    const margin = 12;
    const width = Math.min(342, window.innerWidth - margin * 2);
    const alignRight = placement?.endsWith("Right");
    const alignCenter = placement === "top" || placement === "bottom";
    const left = alignCenter ? buttonRect.left + buttonRect.width / 2 - width / 2 : alignRight ? buttonRect.right - width : buttonRect.left;
    const topPlacement = placement?.startsWith("top");
    const estimatedHeight = 460;
    const viewportHeight = window.innerHeight;
    const topSpace = buttonRect.top - gap - margin;
    const bottomSpace = viewportHeight - buttonRect.bottom - gap - margin;
    const placeAbove = topPlacement ? topSpace >= estimatedHeight || topSpace >= bottomSpace : bottomSpace < estimatedHeight && topSpace > bottomSpace;
    const availableViewportHeight = Math.max(0, viewportHeight - margin * 2);
    const hasAnchoredSpace = placeAbove ? topSpace >= estimatedHeight : bottomSpace >= estimatedHeight;
    const centeredTop = Math.max(
        margin,
        Math.min(
            Math.max(margin, viewportHeight - estimatedHeight - margin),
            buttonRect.top + buttonRect.height / 2 - estimatedHeight / 2,
        ),
    );
    const style = {
        position: "fixed",
        zIndex: 1200,
        width,
        left: Math.max(margin, Math.min(window.innerWidth - width - margin, left)),
        ...(hasAnchoredSpace
            ? placeAbove
                ? { bottom: viewportHeight - buttonRect.top + gap, maxHeight: topSpace }
                : { top: buttonRect.bottom + gap, maxHeight: bottomSpace }
            : { top: centeredTop, maxHeight: availableViewportHeight }),
        background: theme.spatial.elevated,
        border: `1px solid ${theme.toolbar.border}`,
        borderRadius: 12,
        boxShadow: `0 12px 36px ${theme.spatial.shadow}`,
        padding: 12,
        overflowY: "auto",
        overflowX: "hidden",
        overscrollBehavior: "contain",
        color: theme.node.text,
    } as const;

    return createPortal(
        <div
            ref={panelRef}
            className="canvas-overlay-panel canvas-video-settings-popover aceternity-floating-panel backdrop-blur-2xl"
            style={style}
            onPointerDown={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={(event) => event.stopPropagation()}
        >
            <VideoSettingsPanel config={config} onConfigChange={(key, value) => onConfigChange(key, value)} theme={theme} showTitle={false} className="canvas-video-generation-settings space-y-3.5" generationMode={generationMode} />
        </div>,
        document.body,
    );
}
