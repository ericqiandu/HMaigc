import { findImageModelCapabilities, imageCanvasAspectLabel, imageCanvasQualityLabel, imageCanvasResolutionLabel, imageSizeValue } from "@/lib/image-model-capabilities";
import { cn } from "@/lib/utils";
import type { CanvasTheme } from "@/lib/canvas-theme";
import type { AiConfig } from "@/stores/use-config-store";
import { CanvasGenerationSettingsOption, CanvasGenerationSettingsRatioOption, CanvasGenerationSettingsSection } from "./canvas-generation-settings-ui";

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
    const resolution = imageCanvasResolutionLabel(config.size, capabilities.resolutions, capabilities.resolutionPixels);
    const parsedCount = Math.max(1, Math.floor(Number(config.count) || 1));
    const count = capabilities.outputCounts.includes(parsedCount) ? parsedCount : capabilities.outputCounts[0] || 1;

    const updateDimensions = (nextRatio: string, nextResolution: string) => {
        onConfigChange("size", imageSizeValue(nextRatio, nextResolution, capabilities.resolutionPixels));
    };

    return (
        <div className="canvas-generation-settings canvas-image-generation-settings space-y-5">
            {capabilities.watermarkCapability === "unsupported" ? (
                <CanvasGenerationSettingsSection label="AI 水印" theme={theme}>
                    <p className="canvas-image-watermark-note text-[11px] leading-5" style={{ color: theme.node.muted }}>
                        该模型不支持水印控制，结果由模型服务商决定
                    </p>
                </CanvasGenerationSettingsSection>
            ) : null}

            {capabilities.qualities.length ? (
                <CanvasGenerationSettingsSection label="画质" theme={theme}>
                    <div className="canvas-image-quality-grid grid grid-cols-3 gap-2">
                        {capabilities.qualities.map((option) => (
                            <CanvasGenerationSettingsOption key={option} active={quality === option} label={imageCanvasQualityLabel(option)} theme={theme} className="canvas-image-quality-option h-8" onClick={() => onConfigChange("quality", option)} />
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>
            ) : null}

            {capabilities.resolutions.length ? (
                <CanvasGenerationSettingsSection label="清晰度" theme={theme}>
                    <div className="canvas-image-resolution-grid grid grid-cols-3 gap-2">
                        {capabilities.resolutions.map((option) => (
                            <CanvasGenerationSettingsOption key={option} active={resolution === option} label={option} theme={theme} className="canvas-image-resolution-option h-8" onClick={() => updateDimensions(ratio, option)} />
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>
            ) : null}

            {capabilities.ratios.length ? (
                <CanvasGenerationSettingsSection label="比例" theme={theme}>
                    <div className="canvas-image-ratio-grid grid grid-cols-5 gap-2">
                        {capabilities.ratios.map((option) => (
                            <CanvasGenerationSettingsRatioOption key={option} className="canvas-image-ratio-option" active={ratio === option} label={option} ratio={option} theme={theme} onClick={() => updateDimensions(option, resolution)} />
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>
            ) : null}

            {showCount && capabilities.outputCounts.length > 1 ? (
                <CanvasGenerationSettingsSection label="生成数量" theme={theme}>
                    <div className={cn("canvas-image-count-grid grid gap-2", capabilities.outputCounts.length === 4 ? "grid-cols-4" : "grid-cols-3")}>
                        {capabilities.outputCounts.map((option) => (
                            <CanvasGenerationSettingsOption key={option} active={count === option} label={`${option}张`} theme={theme} className="canvas-image-count-option h-8" onClick={() => onConfigChange("count", String(option))} />
                        ))}
                    </div>
                </CanvasGenerationSettingsSection>
            ) : null}
        </div>
    );
}

export { buildImageDimensions } from "@/lib/image-model-capabilities";
