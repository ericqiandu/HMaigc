import { resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export const klingDurationOptions = [5, 10] as const;
export const klingResolutionOptions = [
    { value: "720p", label: "720P" },
    { value: "1080p", label: "1080P" },
] as const;
export const klingRatioOptions = [
    { value: "16:9", label: "16:9" },
    { value: "9:16", label: "9:16" },
    { value: "1:1", label: "1:1" },
] as const;

export function isKlingVideoConfig(config: AiConfig) {
    const resolved = resolveModelRequestConfig(config, config.model || config.videoModel);
    return resolved.interfaceType === "kling-video";
}

export function normalizeKlingResolution(value: string) {
    const normalized = value.trim().toLowerCase();
    return normalized === "1080" || normalized === "1080p" || normalized === "pro" ? "1080p" : "720p";
}

export function normalizeKlingDuration(value: string) {
    return Number(value) === 10 ? 10 : 5;
}
