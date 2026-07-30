import { type CSSProperties, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Check, ChevronDown, Image as ImageIcon } from "lucide-react";

import { canvasThemes, type CanvasTheme } from "@/lib/canvas-theme";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasNodeMetadata } from "@/types/canvas";

type VideoFrameOption = {
    nodeId: string;
    label: string;
    title: string;
    previewUrl?: string;
};

type CompactMenuItem = {
    value: string;
    label: string;
    previewUrl?: string;
};

type CanvasVideoPromptToolsProps = {
    metadata?: CanvasNodeMetadata;
    frameOptions: VideoFrameOption[];
    onMetadataChange: (patch: Partial<CanvasNodeMetadata>) => void;
};

const EMPTY_FRAME_VALUE = "__none__";
const MENU_GAP = 6;
const MENU_MARGIN = 8;
const MENU_ITEM_HEIGHT = 28;
const CONTROL_TEXT_STYLE: CSSProperties = { fontFamily: "inherit", fontSize: 11, fontWeight: 400, letterSpacing: 0, lineHeight: 1 };

export function CanvasVideoPromptTools({ metadata, frameOptions, onMetadataChange }: CanvasVideoPromptToolsProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const startFrame = metadata?.videoStartFrameNodeId || EMPTY_FRAME_VALUE;
    const endFrame = metadata?.videoEndFrameNodeId || EMPTY_FRAME_VALUE;

    const setFrame = (key: "videoStartFrameNodeId" | "videoEndFrameNodeId", value: string) => {
        const next = value === EMPTY_FRAME_VALUE ? undefined : value;
        if (key === "videoStartFrameNodeId") {
            onMetadataChange(next ? { videoStartFrameNodeId: next } : { videoStartFrameNodeId: undefined, videoEndFrameNodeId: undefined });
            return;
        }
        onMetadataChange({ videoEndFrameNodeId: next });
    };

    if (!frameOptions.length || (startFrame === EMPTY_FRAME_VALUE && endFrame === EMPTY_FRAME_VALUE)) return null;

    return (
        <div
            className="canvas-video-frame-strip thin-scrollbar flex h-[42px] min-w-0 shrink-0 items-center gap-1.5 overflow-x-auto px-2.5 pt-1.5"
            role="group"
            aria-label="视频参考帧"
            data-canvas-no-zoom
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
        >
            {startFrame !== EMPTY_FRAME_VALUE ? (
                <FrameMenu label="首帧" badge="首" value={startFrame} options={frameOptions} theme={theme} onChange={(value) => setFrame("videoStartFrameNodeId", value)} />
            ) : null}
            {endFrame !== EMPTY_FRAME_VALUE ? (
                <>
                    <span className="canvas-video-frame-direction shrink-0 text-[11px]" style={{ color: theme.node.faint }} aria-hidden="true">→</span>
                    <FrameMenu label="尾帧" badge="尾" value={endFrame} options={frameOptions} theme={theme} onChange={(value) => setFrame("videoEndFrameNodeId", value)} />
                </>
            ) : null}
        </div>
    );
}

function FrameMenu({ label, badge, value, options, theme, onChange }: { label: string; badge: string; value: string; options: VideoFrameOption[]; theme: CanvasTheme; onChange: (value: string) => void }) {
    const selected = options.find((item) => item.nodeId === value);
    const items = [{ value: EMPTY_FRAME_VALUE, label: "不指定" }, ...options.map((option) => ({ value: option.nodeId, label: `${option.label} · ${option.title}`, previewUrl: option.previewUrl }))];
    return (
        <CompactMenuButton
            theme={theme}
            title={label}
            badge={badge}
            previewUrl={selected?.previewUrl}
            value={value}
            items={items}
            menuWidth={220}
            maxMenuHeight={208}
            onSelect={onChange}
        />
    );
}

function CompactMenuButton({
    theme,
    title,
    badge,
    previewUrl,
    value,
    items,
    menuWidth,
    maxMenuHeight,
    onSelect,
}: {
    theme: CanvasTheme;
    title: string;
    badge: string;
    previewUrl?: string;
    value?: string;
    items: CompactMenuItem[];
    menuWidth: number;
    maxMenuHeight: number;
    onSelect: (value: string) => void;
}) {
    const triggerRef = useRef<HTMLButtonElement>(null);
    const menuRef = useRef<HTMLDivElement>(null);
    const [open, setOpen] = useState(false);
    const [position, setPosition] = useState<{ left: number; top: number; width: number; maxHeight: number } | null>(null);

    const updatePosition = () => {
        const rect = triggerRef.current?.getBoundingClientRect();
        if (!rect) return;
        const menuHeight = Math.min(maxMenuHeight, items.length * MENU_ITEM_HEIGHT + 8);
        const effectiveMenuWidth = Math.max(menuWidth, Math.round(rect.width));
        const openAbove = rect.bottom + MENU_GAP + menuHeight > window.innerHeight && rect.top > menuHeight + MENU_GAP;
        const maxLeft = Math.max(MENU_MARGIN, window.innerWidth - effectiveMenuWidth - MENU_MARGIN);
        const maxTop = Math.max(MENU_MARGIN, window.innerHeight - menuHeight - MENU_MARGIN);
        const left = Math.min(Math.max(MENU_MARGIN, rect.left), maxLeft);
        const top = openAbove ? Math.max(MENU_MARGIN, rect.top - menuHeight - MENU_GAP) : Math.min(Math.max(MENU_MARGIN, rect.bottom + MENU_GAP), maxTop);
        setPosition({ left, top, maxHeight: menuHeight, width: effectiveMenuWidth });
    };

    useLayoutEffect(() => {
        if (open) updatePosition();
    }, [open, items.length, maxMenuHeight, menuWidth]);

    useEffect(() => {
        if (!open) return;
        const close = (event: PointerEvent) => {
            const target = event.target instanceof Node ? event.target : null;
            if (target && (triggerRef.current?.contains(target) || menuRef.current?.contains(target))) return;
            setOpen(false);
        };
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key === "Escape") setOpen(false);
        };
        const reposition = () => updatePosition();
        document.addEventListener("pointerdown", close, true);
        document.addEventListener("keydown", closeOnEscape, true);
        window.addEventListener("resize", reposition);
        window.addEventListener("scroll", reposition, true);
        return () => {
            document.removeEventListener("pointerdown", close, true);
            document.removeEventListener("keydown", closeOnEscape, true);
            window.removeEventListener("resize", reposition);
            window.removeEventListener("scroll", reposition, true);
        };
    }, [open, items.length, maxMenuHeight, menuWidth]);

    const buttonStyle: CSSProperties = {
        background: theme.toolbar.itemHover,
        color: theme.node.text,
        outlineColor: theme.accent.primary,
    };
    const menuStyle: CSSProperties | undefined = position
        ? {
              left: position.left,
              top: position.top,
              width: position.width,
              maxHeight: position.maxHeight,
              background: theme.toolbar.panel,
              borderColor: theme.toolbar.border,
              color: theme.node.text,
              boxShadow: "0 10px 26px rgba(15,23,42,.14)",
          }
        : undefined;
    const listStyle: CSSProperties | undefined = position ? { maxHeight: Math.max(0, position.maxHeight - 8) } : undefined;

    const menu =
        open && position && typeof document !== "undefined"
            ? createPortal(
                  <div
                      ref={menuRef}
                      data-canvas-no-zoom
                      className="fixed z-[1500] overflow-hidden rounded-[12px] border p-1 backdrop-blur-xl"
                      style={menuStyle}
                      onMouseDown={(event) => event.stopPropagation()}
                      onPointerDown={(event) => event.stopPropagation()}
                      onWheel={(event) => event.stopPropagation()}
                  >
                      <div className="thin-scrollbar overflow-y-auto" style={listStyle}>
                          {items.map((item) => {
                              const selected = value === item.value;
                              return (
                                  <button
                                      key={item.value}
                                      type="button"
                                      className="flex h-7 w-full min-w-0 items-center gap-1.5 rounded-lg px-2 text-left transition"
                                      style={{ ...CONTROL_TEXT_STYLE, background: selected ? theme.toolbar.itemHover : "transparent", color: selected ? theme.toolbar.activeText : theme.node.text }}
                                      onMouseEnter={(event) => {
                                          event.currentTarget.style.background = theme.toolbar.itemHover;
                                      }}
                                      onMouseLeave={(event) => {
                                          event.currentTarget.style.background = selected ? theme.toolbar.itemHover : "transparent";
                                      }}
                                      onClick={() => {
                                          onSelect(item.value);
                                          setOpen(false);
                                      }}
                                  >
                                      {item.previewUrl ? <img src={item.previewUrl} alt="" className="size-4 shrink-0 rounded object-cover" /> : null}
                                      <span className="min-w-0 flex-1 truncate">{item.label}</span>
                                      {selected ? <Check className="size-3.5 shrink-0" /> : null}
                                  </button>
                              );
                          })}
                      </div>
                  </div>,
                  document.body,
              )
            : null;

    return (
        <>
            <button
                ref={triggerRef}
                type="button"
                className="canvas-video-frame-trigger group relative size-[34px] shrink-0 overflow-hidden rounded-md border-0 p-0 shadow-none transition hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1"
                style={buttonStyle}
                title={`更换${title}`}
                aria-label={`更换${title}`}
                onClick={() => setOpen((value) => !value)}
                onMouseDown={(event) => event.stopPropagation()}
                onPointerDown={(event) => event.stopPropagation()}
            >
                {previewUrl ? (
                    <img src={previewUrl} alt="" className="canvas-video-frame-preview size-full object-cover" />
                ) : (
                    <span className="canvas-video-frame-placeholder grid size-full place-items-center">
                        <ImageIcon className="size-3.5 opacity-70" />
                    </span>
                )}
                <span className="canvas-video-frame-badge absolute left-0.5 top-0.5 grid size-3.5 place-items-center rounded-full bg-black/65 text-[8px] font-semibold text-white backdrop-blur-sm">
                    {badge}
                </span>
                <span className="canvas-video-frame-chevron absolute bottom-0.5 right-0.5 grid size-3.5 place-items-center rounded-full bg-black/65 text-white opacity-0 backdrop-blur-sm transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
                    <ChevronDown className="size-2.5" />
                </span>
            </button>
            {menu}
        </>
    );
}
