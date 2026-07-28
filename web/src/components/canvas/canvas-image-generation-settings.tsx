import { cn } from "@/lib/utils";
import type { CanvasTheme } from "@/lib/canvas-theme";
import type { AiConfig } from "@/stores/use-config-store";

type CanvasImageGenerationSettingsProps = {
    config: AiConfig;
    theme: CanvasTheme;
    showCount: boolean;
    onConfigChange: (key: keyof AiConfig, value: string) => void;
};

type ImageResolution = "1K" | "2K" | "4K";

const qualityOptions = [
    { value: "low", label: "低画质" },
    { value: "medium", label: "标准画质" },
    { value: "high", label: "高画质" },
] as const;

const resolutionOptions: ImageResolution[] = ["1K", "2K", "4K"];
const ratioOptions = ["1:1", "1:2", "2:1", "9:16", "16:9", "3:4", "4:3", "3:2", "2:3", "5:4", "4:5", "21:9", "9:21"] as const;
const countOptions = [1, 2, 4] as const;

export function CanvasImageGenerationSettings({ config, theme, showCount, onConfigChange }: CanvasImageGenerationSettingsProps) {
    const quality = normalizeQuality(config.quality);
    const ratio = imageCanvasAspectLabel(config.size);
    const resolution = imageCanvasResolutionLabel(config.size);
    const count = Math.max(1, Math.min(15, Math.floor(Math.abs(Number(config.count)) || 1)));
    const counts = countOptions.includes(count as (typeof countOptions)[number]) ? countOptions : [...countOptions, count];

    const updateDimensions = (nextRatio: string, nextResolution: ImageResolution) => {
        onConfigChange("size", buildImageDimensions(nextRatio, nextResolution));
    };

    return (
        <div className="canvas-image-generation-settings space-y-3.5">
            <SettingsSection label="画质">
                <div className="canvas-image-quality-grid grid grid-cols-3 gap-2">
                    {qualityOptions.map((option) => (
                        <SettingsButton key={option.value} active={quality === option.value} label={option.label} theme={theme} className="canvas-image-quality-option h-8" onClick={() => onConfigChange("quality", option.value)} />
                    ))}
                </div>
            </SettingsSection>

            <SettingsSection label="清晰度">
                <div className="canvas-image-resolution-grid grid grid-cols-3 gap-2">
                    {resolutionOptions.map((option) => (
                        <SettingsButton key={option} active={resolution === option} label={option} theme={theme} className="canvas-image-resolution-option h-8 font-semibold" onClick={() => updateDimensions(ratio, option)} />
                    ))}
                </div>
            </SettingsSection>

            <SettingsSection label="比例">
                <div className="canvas-image-ratio-grid grid grid-cols-5 gap-2">
                    {ratioOptions.map((option) => (
                        <button
                            key={option}
                            type="button"
                            className={cn("canvas-image-ratio-option flex h-[62px] flex-col items-center justify-center gap-1.5 rounded-lg text-[11px] transition-colors", ratio === option ? "font-semibold" : "opacity-60 hover:opacity-90")}
                            style={settingButtonStyle(theme, ratio === option)}
                            onClick={() => updateDimensions(option, resolution)}
                        >
                            <RatioIcon ratio={option} active={ratio === option} />
                            <span className="canvas-image-ratio-label">{option}</span>
                        </button>
                    ))}
                </div>
            </SettingsSection>

            {showCount ? (
                <SettingsSection label="生成数量">
                    <div className={cn("canvas-image-count-grid grid gap-2", counts.length === 4 ? "grid-cols-4" : "grid-cols-3")}>
                        {counts.map((option) => (
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
            <h4 className="canvas-image-settings-label text-[12px] font-medium opacity-55">{label}</h4>
            {children}
        </section>
    );
}

function SettingsButton({ active, label, theme, className, onClick }: { active: boolean; label: string; theme: CanvasTheme; className: string; onClick: () => void }) {
    return (
        <button type="button" className={cn("canvas-image-settings-option rounded-lg text-[12px] transition-colors", active ? "font-semibold" : "opacity-60 hover:opacity-90", className)} style={settingButtonStyle(theme, active)} onClick={onClick}>
            <span className="canvas-image-settings-option-label">{label}</span>
        </button>
    );
}

function RatioIcon({ ratio, active }: { ratio: string; active: boolean }) {
    const [width, height] = ratio.split(":").map(Number);
    const longest = 16;
    const scale = longest / Math.max(width, height);
    return (
        <span
            className={cn("canvas-image-ratio-icon block rounded-[2px] border", active ? "border-current" : "border-current/70")}
            style={{ width: Math.max(5, Math.round(width * scale)), height: Math.max(5, Math.round(height * scale)) }}
            aria-hidden="true"
        />
    );
}

function settingButtonStyle(theme: CanvasTheme, active: boolean) {
    return {
        color: theme.node.text,
        background: active ? theme.toolbar.activeBg : theme.toolbar.itemHover,
        border: `1px solid ${active ? theme.node.text : theme.toolbar.border}`,
    };
}

function normalizeQuality(value?: string) {
    return value === "low" || value === "high" ? value : "medium";
}

export function imageCanvasQualityLabel(value?: string) {
    if (value === "low") return "低画质";
    if (value === "high") return "高画质";
    return "标准画质";
}

export function imageCanvasResolutionLabel(size?: string): ImageResolution {
    const dimensions = parseDimensions(size);
    if (!dimensions) return "1K";
    const longest = Math.max(dimensions.width, dimensions.height);
    if (longest >= 3000) return "4K";
    if (longest >= 1900) return "2K";
    return "1K";
}

export function imageCanvasAspectLabel(size?: string) {
    const dimensions = parseDimensions(size);
    if (!dimensions) return "1:1";
    const target = dimensions.width / dimensions.height;
    return ratioOptions.reduce((closest, candidate) => {
        const [width, height] = candidate.split(":").map(Number);
        const [closestWidth, closestHeight] = closest.split(":").map(Number);
        return Math.abs(width / height - target) < Math.abs(closestWidth / closestHeight - target) ? candidate : closest;
    });
}

function parseDimensions(size?: string) {
    if (!size) return null;
    const match = /^(\d+)x(\d+)$/i.exec(size.trim());
    if (!match) return null;
    const width = Number(match[1]);
    const height = Number(match[2]);
    return width > 0 && height > 0 ? { width, height } : null;
}

function buildImageDimensions(ratio: string, resolution: ImageResolution) {
    const [widthRatio, heightRatio] = ratio.split(":").map(Number);
    const square = widthRatio === heightRatio;
    const landscape = widthRatio > heightRatio;
    const longestEdge = resolution === "4K" ? 3840 : resolution === "2K" ? 2048 : square ? 1024 : 1824;
    const shortestEdge = alignDimension((longestEdge * Math.min(widthRatio, heightRatio)) / Math.max(widthRatio, heightRatio));
    const width = square ? longestEdge : landscape ? longestEdge : shortestEdge;
    const height = square ? longestEdge : landscape ? shortestEdge : longestEdge;
    return `${width}x${height}`;
}

function alignDimension(value: number) {
    return Math.max(64, Math.round(value / 8) * 8);
}
