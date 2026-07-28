import { resolveModelChannel, type AiConfig } from "@/stores/use-config-store";

export const VIDEO_SUPER_RESOLUTION_SCENES = [
    { value: "aigc", label: "AIGC" },
    { value: "short_series", label: "短剧" },
    { value: "ugc", label: "UGC" },
    { value: "old_film", label: "老片修复" },
] as const;

export const VIDEO_SUPER_RESOLUTION_VERSIONS = [
    { value: "standard", label: "标准" },
    { value: "professional", label: "专业" },
] as const;

const resolutionRank: Record<string, number> = { "480p": 1, "720p": 2, "1080p": 3, "2k": 4, "4k": 5 };

export function supportsVideoSuperResolution(config: AiConfig) {
    const selectedModel = config.videoModel || config.model;
    if (!selectedModel.trim()) return false;
    return resolveModelChannel(config, selectedModel).interfaceType === "ai-open-platform-video";
}

export function videoSuperResolutionTargets(baseResolution: string) {
    const normalized = normalizeResolution(baseResolution);
    const baseRank = resolutionRank[normalized];
    return ["720p", "1080p", "2k", "4k"].filter((value) => resolutionRank[value] > baseRank);
}

function normalizeResolution(value: string) {
    const normalized = value.toLowerCase().trim();
    if (normalized === "480" || normalized === "480p") return "480p";
    if (normalized === "720" || normalized === "720p") return "720p";
    if (normalized === "1080" || normalized === "1080p") return "1080p";
    return normalized;
}
