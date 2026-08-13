import type { ReactNode } from "react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import { cn } from "@/lib/utils";

type CanvasGenerationSettingsSectionProps = {
    label: ReactNode;
    theme: CanvasTheme;
    children: ReactNode;
};

type CanvasGenerationSettingsOptionProps = {
    active: boolean;
    label: ReactNode;
    theme: CanvasTheme;
    className?: string;
    disabled?: boolean;
    onClick: () => void;
};

type CanvasGenerationSettingsRatioOptionProps = Omit<CanvasGenerationSettingsOptionProps, "label"> & {
    label: string;
    ratio: string;
};

export function CanvasGenerationSettingsSection({ label, theme, children }: CanvasGenerationSettingsSectionProps) {
    return (
        <section className="canvas-generation-settings-section space-y-1.5">
            <h4 className="canvas-generation-settings-label" style={{ color: theme.node.muted }}>
                {label}
            </h4>
            {children}
        </section>
    );
}

export function CanvasGenerationSettingsOption({ active, label, theme, className, disabled = false, onClick }: CanvasGenerationSettingsOptionProps) {
    return (
        <button
            type="button"
            className={cn("canvas-generation-settings-option rounded-lg transition-colors", active ? "is-active" : "hover:brightness-110", className)}
            style={generationSettingButtonStyle(theme, active)}
            disabled={disabled}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onClick}
            aria-pressed={active}
        >
            <span className="canvas-generation-settings-option-label">{label}</span>
        </button>
    );
}

export function CanvasGenerationSettingsRatioOption({ active, label, ratio, theme, className, disabled = false, onClick }: CanvasGenerationSettingsRatioOptionProps) {
    return (
        <button
            type="button"
            className={cn("canvas-generation-settings-ratio-option flex h-[63px] min-w-0 flex-col items-center justify-center gap-1.5 rounded-lg transition-colors", active ? "is-active" : "hover:brightness-110", className)}
            style={generationSettingButtonStyle(theme, active)}
            disabled={disabled}
            onMouseDown={(event) => event.stopPropagation()}
            onClick={onClick}
            aria-pressed={active}
        >
            <RatioPreview ratio={ratio} active={active} />
            <span className="canvas-generation-settings-ratio-label whitespace-nowrap">{label}</span>
        </button>
    );
}

function RatioPreview({ ratio, active }: { ratio: string; active: boolean }) {
    const [width, height] = ratio.split(":").map(Number);
    const valid = Number.isFinite(width) && Number.isFinite(height) && width > 0 && height > 0;
    const longest = 16;
    const scale = valid ? longest / Math.max(width, height) : 1;

    return (
        <span className="canvas-generation-settings-ratio-preview grid h-4 place-items-center" aria-hidden="true">
            {valid ? (
                <span
                    className={cn("canvas-generation-settings-ratio-shape block rounded-[2px] border", active ? "border-current" : "border-current/70")}
                    style={{ width: Math.max(5, Math.round(width * scale)), height: Math.max(5, Math.round(height * scale)) }}
                />
            ) : (
                <span className="canvas-generation-settings-ratio-auto text-[9px] opacity-70">A</span>
            )}
        </span>
    );
}

function generationSettingButtonStyle(theme: CanvasTheme, active: boolean) {
    return {
        color: active ? theme.accent.primary : theme.node.muted,
        background: active ? theme.accent.primarySoft : theme.toolbar.itemHover,
        border: `1px solid ${active ? theme.spatial.glowStrong : theme.toolbar.border}`,
    };
}
