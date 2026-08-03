import { resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export const miniMaxH3DurationOptions = Array.from({ length: 12 }, (_, index) => index + 4);
export const miniMaxH3ResolutionOptions = [
    { value: "768p", label: "768P" },
    { value: "2k", label: "2K" },
] as const;

export function isMiniMaxH3VideoConfig(config: AiConfig) {
    const resolved = resolveModelRequestConfig(config, config.model || config.videoModel);
    return resolved.interfaceType === "minimax-video";
}

export function normalizeMiniMaxH3Resolution(value: string) {
    return value.trim().toLowerCase() === "2k" || value.trim().toLowerCase() === "1440p" ? "2k" : "768p";
}

export function normalizeMiniMaxH3Duration(value: string) {
    const duration = Math.round(Number(value));
    return Math.max(4, Math.min(15, Number.isFinite(duration) ? duration : 6));
}
