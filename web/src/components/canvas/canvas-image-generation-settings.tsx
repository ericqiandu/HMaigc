import { findImageModelCapabilities, imageCanvasAspectLabel, imageCanvasQualityLabel, imageCanvasResolutionLabel, imageSizeValue } from "@/lib/image-model-capabilities";
import { cn } from "@/lib/utils";
import type { CanvasTheme } from "@/lib/canvas-theme";
import type { AiConfig } from "@/stores/use-config-store";

type CanvasImageGenerationSettingsProps = {
    config: AiConfig;
    theme: CanvasTheme;
    showCount: boolean;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
};

export function CanvasImageGenerationSettings({ config, theme, showCount, onConfigChange }: CanvasImageGenerationSettingsProps) {
    const capabilities = findImageModelCapabilities(config);
    if (!capabilities) {
        return <p className="canvas-image-capability-error px-1 py-2 text-xs opacity-60">后台尚未发布该图片模型的参数能力</p>;
    }
    const quality = capabilities.qualities.includes(config.quality) ? config.quality : capabilities.qualities[0] || "";
    const ratio = imageCanvasAspectLabel(config.size, capabilities.ratios);
    const resolution = imageCanvasResolutionLabel(config.size, capabilities.resolutions);
    const parsedCount = Math.max(1, Math.floor(Number(config.count) || 1));
    const count = capabilities.outputCounts.includes(parsedCount) ? parsedCount : capabilities.outputCounts[0] || 1;

    const updateDimensions = (nextRatio: string, nextResolution: string) => {
        onConfigChange("size", imageSizeValue(nextRatio, nextResolution));
    };

    return (
        <div className="canvas-image-generation-settings space-y-5">
            {capabilities.qualities.length ? (
                <SettingsSection label="画质">
                    <div className="canvas-image-quality-grid grid grid-cols-3 gap-2">
                        {capabilities.qualities.map((option) => (
                            <SettingsButton key={option} active={quality === option} label={imageCanvasQualityLabel(option)} theme={theme} className="canvas-image-quality-option h-8" onClick={() => onConfigChange("quality", option)} />
                        ))}
                    </div>
                </SettingsSection>
            ) : null}

            {capabilities.resolutions.length ? (
                <SettingsSection label="清晰度">
                    <div className="canvas-image-resolution-grid grid grid-cols-3 gap-2">
                        {capabilities.resolutions.map((option) => (
                            <SettingsButton key={option} active={resolution === option} label={option} theme={theme} className="canvas-image-resolution-option h-8" onClick={() => updateDimensions(ratio, option)} />
                        ))}
                    </div>
                </SettingsSection>
            ) : null}

            {capabilities.ratios.length ? (
                <SettingsSection label="比例">
                    <div className="canvas-image-ratio-grid grid grid-cols-5 gap-2">
                        {capabilities.ratios.map((option) => (
                            <button
                                key={option}
                                type="button"
                                className={cn("canvas-image-ratio-option flex h-[63px] flex-col items-center justify-center gap-1.5 rounded-lg transition-colors", ratio === option ? "is-active" : "hover:brightness-110")}
                                style={settingButtonStyle(theme, ratio === option)}
                                onClick={() => updateDimensions(option, resolution)}
                                aria-pressed={ratio === option}
                            >
                                <RatioIcon ratio={option} active={ratio === option} />
                                <span className="canvas-image-ratio-label">{option}</span>
                            </button>
                        ))}
                    </div>
                </SettingsSection>
            ) : null}

            {showCount && capabilities.outputCounts.length > 1 ? (
                <SettingsSection label="生成数量">
                    <div className={cn("canvas-image-count-grid grid gap-2", capabilities.outputCounts.length === 4 ? "grid-cols-4" : "grid-cols-3")}>
                        {capabilities.outputCounts.map((option) => (
                            <SettingsButton key={option} active={count === option} label={`${option}张`} theme={theme} className="canvas-image-count-option h-8" onClick={() => onConfigChange("count", String(option))} />
                        ))}
                    </div>
                </SettingsSection>
            ) : null}
        </div>
    );
}

function SettingsSection({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <section className="canvas-image-settings-section space-y-1.5">
            <h4 className="canvas-image-settings-label opacity-55">{label}</h4>
            {children}
        </section>
    );
}

function SettingsButton({ active, label, theme, className, onClick }: { active: boolean; label: string; theme: CanvasTheme; className: string; onClick: () => void }) {
    return (
        <button type="button" className={cn("canvas-image-settings-option rounded-lg transition-colors", active ? "is-active" : "hover:brightness-110", className)} style={settingButtonStyle(theme, active)} onClick={onClick} aria-pressed={active}>
            <span className="canvas-image-settings-option-label">{label}</span>
        </button>
    );
}

function RatioIcon({ ratio, active }: { ratio: string; active: boolean }) {
    const [width, height] = ratio.split(":").map(Number);
    const valid = width > 0 && height > 0;
    const longest = 16;
    const scale = valid ? longest / Math.max(width, height) : 1;
    return (
        <span
            className={cn("canvas-image-ratio-icon block rounded-[2px] border", active ? "border-current" : "border-current/70")}
            style={{ width: valid ? Math.max(5, Math.round(width * scale)) : 12, height: valid ? Math.max(5, Math.round(height * scale)) : 12 }}
            aria-hidden="true"
        />
    );
}

function settingButtonStyle(theme: CanvasTheme, active: boolean) {
    return {
        color: active ? theme.accent.primary : theme.node.muted,
        background: active ? theme.accent.primarySoft : theme.toolbar.itemHover,
        border: `1px solid ${active ? theme.spatial.glowStrong : theme.toolbar.border}`,
    };
}

export { buildImageDimensions } from "@/lib/image-model-capabilities";
