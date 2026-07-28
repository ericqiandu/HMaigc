import { useEffect, useRef, useState, type RefObject } from "react";
import { createPortal } from "react-dom";
import { Settings2 } from "lucide-react";
import { Button } from "antd";

import { CanvasImageGenerationSettings, imageCanvasAspectLabel, imageCanvasQualityLabel, imageCanvasResolutionLabel } from "@/components/canvas/canvas-image-generation-settings";
import { canvasThemes } from "@/lib/canvas-theme";
import { cn } from "@/lib/utils";
import { useThemeStore } from "@/stores/use-theme-store";
import type { AiConfig } from "@/stores/use-config-store";

type CanvasImageSettingsPopoverProps = {
    config: AiConfig;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
    onMissingConfig?: () => void;
    onOpenChange?: (open: boolean) => void;
    buttonClassName?: string;
    getPopupContainer?: (triggerNode: HTMLElement) => HTMLElement;
    placement?: "topLeft" | "top" | "topRight" | "bottomLeft" | "bottom" | "bottomRight";
    autoAdjustOverflow?: boolean;
    showCount?: boolean;
};

export function CanvasImageSettingsPopover({ config, onConfigChange, onOpenChange, buttonClassName, placement = "topLeft", showCount = true }: CanvasImageSettingsPopoverProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const buttonRef = useRef<HTMLSpanElement>(null);
    const panelRef = useRef<HTMLDivElement>(null);
    const [open, setOpen] = useState(false);
    const [buttonRect, setButtonRect] = useState<DOMRect | null>(null);
    const quality = config.quality || "auto";
    const count = Math.max(1, Math.min(15, Math.floor(Math.abs(Number(config.count)) || 1)));
    const transparentLabel = config.transparentBackground === "true" ? " · 透明" : "";
    const settingsSummary = `${imageCanvasAspectLabel(config.size)} · ${imageCanvasQualityLabel(quality)} · ${imageCanvasResolutionLabel(config.size)}`;
    const summary = showCount ? `${settingsSummary} · ${count}张${transparentLabel}` : `${settingsSummary}${transparentLabel}`;
    const updateOpen = (nextOpen: boolean) => {
        setOpen(nextOpen);
        onOpenChange?.(nextOpen);
    };

    useEffect(() => {
        if (!open) return;
        const syncPosition = () => setButtonRect(buttonRef.current?.getBoundingClientRect() || null);
        const closeOnOutsidePointer = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (buttonRef.current?.contains(target) || panelRef.current?.contains(target)) return;
            if (document.activeElement instanceof HTMLElement && panelRef.current?.contains(document.activeElement)) document.activeElement.blur();
            setOpen(false);
            onOpenChange?.(false);
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
    }, [onOpenChange, open]);

    const panel = open && buttonRect ? <ImageSettingsPortal buttonRect={buttonRect} panelRef={panelRef} placement={placement} theme={theme} config={config} showCount={showCount} onConfigChange={onConfigChange} /> : null;

    return (
        <>
            <span ref={buttonRef} className="canvas-image-settings-trigger-wrap inline-flex min-w-0">
                <Button
                    size="small"
                    type="text"
                    className={cn("canvas-image-settings-trigger", buttonClassName || "!h-8 !max-w-[180px] !justify-start !rounded-full !px-2.5")}
                    style={{ background: theme.node.fill, color: theme.node.text }}
                    icon={<Settings2 className="canvas-image-settings-trigger-icon size-3.5" />}
                    onClick={() => updateOpen(!open)}
                >
                    <span className="canvas-image-settings-trigger-summary truncate">{summary}</span>
                </Button>
            </span>
            {panel}
        </>
    );
}

function ImageSettingsPortal({
    buttonRect,
    panelRef,
    placement,
    theme,
    config,
    showCount,
    onConfigChange,
}: {
    buttonRect: DOMRect;
    panelRef: RefObject<HTMLDivElement | null>;
    placement: CanvasImageSettingsPopoverProps["placement"];
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    config: AiConfig;
    showCount: boolean;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
}) {
    const width = 342;
    const gap = 8;
    const margin = 12;
    const panelHeight = 463;
    const alignRight = placement?.endsWith("Right");
    const alignCenter = placement === "top" || placement === "bottom";
    const left = alignCenter ? buttonRect.left + buttonRect.width / 2 - width / 2 : alignRight ? buttonRect.right - width : buttonRect.left;
    const topPlacement = placement?.startsWith("top");
    const preferredTop = topPlacement ? buttonRect.top - panelHeight - gap : buttonRect.bottom + gap;
    const top = Math.max(margin, Math.min(window.innerHeight - panelHeight - margin, preferredTop));
    const style = {
        position: "fixed",
        zIndex: 1200,
        width,
        left: Math.max(margin, Math.min(window.innerWidth - width - margin, left)),
        top,
        maxHeight: `calc(100vh - ${margin * 2}px)`,
        background: theme.spatial.elevated,
        border: `1px solid ${theme.toolbar.border}`,
        borderRadius: 16,
        boxShadow: `0 24px 72px ${theme.spatial.shadow}, inset 0 1px 0 rgba(255,255,255,.08)`,
        padding: 12,
        overflowY: "auto",
        color: theme.node.text,
    } as const;

    return createPortal(
        <div
            ref={panelRef}
            className="canvas-image-settings-popover aceternity-floating-panel backdrop-blur-2xl"
            style={style}
            onPointerDown={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={(event) => event.stopPropagation()}
        >
            <CanvasImageGenerationSettings config={config} onConfigChange={onConfigChange} theme={theme} showCount={showCount} />
        </div>,
        document.body,
    );
}
