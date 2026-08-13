import type { CanvasNodeMetadata } from "@/types/canvas";
import { modelOptionName, resolveModelChannel, type AiConfig, type ProviderModelCapabilities } from "@/stores/use-config-store";

type ImageDimensions = { width: number; height: number };

const IMAGE_RESOLUTION_LONG_EDGE: Readonly<Record<string, number>> = {
    "1K": 1824,
    "2K": 2048,
    "4K": 3840,
};

export function resolveImageModelCapabilities(config: AiConfig, selectedModel = config.model || config.imageModel): ProviderModelCapabilities {
    const capabilities = findImageModelCapabilities(config, selectedModel);
    const model = modelOptionName(selectedModel);
    if (!capabilities) {
        throw new Error(`图片模型 ${model || "未选择"} 缺少后台发布的图片能力契约`);
    }
    return capabilities;
}

export function findImageModelCapabilities(config: AiConfig, selectedModel = config.model || config.imageModel): ProviderModelCapabilities | null {
    const model = modelOptionName(selectedModel);
    const channel = resolveModelChannel(config, selectedModel);
    const capabilities = channel.modelCosts?.find((item) => item.model === model)?.providerCapabilities;
    return capabilities?.modelKey === model && capabilities.capability === "image" ? capabilities : null;
}

export function normalizeImageConfigForModel(config: AiConfig, selectedModel = config.model || config.imageModel): AiConfig {
    const capabilities = resolveImageModelCapabilities(config, selectedModel);
    const ratio = imageCanvasAspectLabel(config.size, capabilities.ratios);
    const resolution = imageCanvasResolutionLabel(config.size, capabilities.resolutions);
    const quality = normalizedOption(config.quality, capabilities.qualities);
    const count = normalizedCount(config.count, capabilities.outputCounts);
    const size = imageSizeValue(ratio, resolution);
    return {
        ...config,
        model: selectedModel,
        size,
        quality,
        count: String(count),
        transparentBackground: "false",
    };
}

export function imageModelMetadataPatch(config: AiConfig, selectedModel: string): Partial<CanvasNodeMetadata> {
    const resetConfig = normalizeImageConfigForModel({ ...config, size: "", quality: "", count: "", transparentBackground: "false" }, selectedModel);
    return {
        model: selectedModel,
        size: resetConfig.size,
        quality: resetConfig.quality,
        count: Number(resetConfig.count),
        transparentBackground: "false",
    };
}

export function imageCanvasSettingsSummary(config: AiConfig, showCount: boolean) {
    const capabilities = resolveImageModelCapabilities(config);
    const parts: string[] = [];
    if (capabilities.ratios.length) parts.push(imageCanvasAspectLabel(config.size, capabilities.ratios));
    if (capabilities.qualities.length) parts.push(imageCanvasQualityLabel(normalizedOption(config.quality, capabilities.qualities)));
    if (capabilities.resolutions.length) parts.push(imageCanvasResolutionLabel(config.size, capabilities.resolutions));
    if (showCount && capabilities.outputCounts.length > 1) parts.push(`${normalizedCount(config.count, capabilities.outputCounts)}张`);
    return parts.join(" · ") || "无可调参数";
}

export function imageCanvasQualityLabel(value: string) {
    if (value === "low") return "低画质";
    if (value === "medium") return "标准画质";
    if (value === "high") return "高画质";
    return value;
}

export function imageCanvasResolutionLabel(size: string | undefined, resolutions: readonly string[]) {
    if (!resolutions.length) return "";
    const dimensions = parseDimensions(size);
    if (!dimensions) return resolutions[0];
    const targetPixels = dimensions.width * dimensions.height;
    const targetRatio = `${dimensions.width}:${dimensions.height}`;
    return resolutions.reduce((closest, candidate) => {
        const candidatePixels = dimensionsPixelsForResolution(targetRatio, candidate);
        const closestPixels = dimensionsPixelsForResolution(targetRatio, closest);
        if (!candidatePixels) return closest;
        if (!closestPixels) return candidate;
        return Math.abs(candidatePixels - targetPixels) < Math.abs(closestPixels - targetPixels) ? candidate : closest;
    });
}

export function imageCanvasAspectLabel(size: string | undefined, ratios: readonly string[]) {
    if (!ratios.length) return "";
    const exact = size?.trim();
    if (exact && ratios.includes(exact)) return exact;
    const dimensions = parseDimensions(size);
    if (!dimensions) return ratios[0];
    const target = dimensions.width / dimensions.height;
    return ratios.reduce((closest, candidate) => (ratioDistance(candidate, target) < ratioDistance(closest, target) ? candidate : closest));
}

export function buildImageDimensions(ratio: string, resolution: string) {
    const parsedRatio = parseRatio(ratio);
    const normalizedResolution = resolution.toUpperCase();
    const configuredLongestEdge = IMAGE_RESOLUTION_LONG_EDGE[normalizedResolution];
    if (!parsedRatio || !configuredLongestEdge) throw new Error(`图片参数契约包含不支持的尺寸组合：${ratio} / ${resolution}`);
    const square = parsedRatio.width === parsedRatio.height;
    const landscape = parsedRatio.width > parsedRatio.height;
    const longestEdge = normalizedResolution === "1K" && square ? 1024 : configuredLongestEdge;
    const shortestEdge = alignDimension((longestEdge * Math.min(parsedRatio.width, parsedRatio.height)) / Math.max(parsedRatio.width, parsedRatio.height));
    let width = square ? longestEdge : landscape ? longestEdge : shortestEdge;
    let height = square ? longestEdge : landscape ? shortestEdge : longestEdge;
    const maxPixels = 8_294_400;
    if (width * height > maxPixels) {
        const scale = Math.sqrt(maxPixels / (width * height));
        width = floorDimension(width * scale);
        height = floorDimension(height * scale);
    }
    return `${width}x${height}`;
}

export function imageSizeValue(ratio: string, resolution: string) {
    if (ratio && resolution) return buildImageDimensions(ratio, resolution);
    return ratio || resolution;
}

function normalizedOption(value: string | undefined, options: readonly string[]) {
    if (!options.length) return "";
    return value && options.includes(value) ? value : options[0];
}

function normalizedCount(value: string | undefined, options: readonly number[]) {
    if (!options.length) return 1;
    const parsed = Math.max(1, Math.floor(Number(value) || 1));
    return options.includes(parsed) ? parsed : options[0];
}

function parseDimensions(size?: string): ImageDimensions | null {
    const match = /^(\d+)x(\d+)$/i.exec(size?.trim() || "");
    if (!match) return null;
    const width = Number(match[1]);
    const height = Number(match[2]);
    return width > 0 && height > 0 ? { width, height } : null;
}

function parseRatio(ratio: string): ImageDimensions | null {
    const [width, height, extra] = ratio.split(":").map(Number);
    return !extra && width > 0 && height > 0 ? { width, height } : null;
}

function ratioDistance(ratio: string, target: number) {
    const parsed = parseRatio(ratio);
    return parsed ? Math.abs(parsed.width / parsed.height - target) : Number.POSITIVE_INFINITY;
}

function dimensionsPixelsForResolution(ratio: string, resolution: string) {
    try {
        const dimensions = parseDimensions(buildImageDimensions(ratio, resolution));
        return dimensions ? dimensions.width * dimensions.height : 0;
    } catch {
        return 0;
    }
}

function alignDimension(value: number) {
    return Math.max(64, Math.round(value / 16) * 16);
}

function floorDimension(value: number) {
    return Math.max(64, Math.floor(value / 16) * 16);
}
