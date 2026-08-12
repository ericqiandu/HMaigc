import { modelOptionName, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

export const seedanceResolutionOptions = [
    { value: "480p", label: "480P" },
    { value: "720p", label: "720P" },
    { value: "1080p", label: "1080P" },
    { value: "4k", label: "4K" },
] as const;

export type SeedanceModelMode = "fast" | "pro" | "mini" | "seedance2.5" | "unknown";

const seedanceBaseResolutionOptions = seedanceResolutionOptions.filter((option) => option.value === "480p" || option.value === "720p");

export const seedanceRatioOptions = [
    { value: "16:9", label: "横屏" },
    { value: "9:16", label: "竖屏" },
    { value: "1:1", label: "方形" },
    { value: "4:3", label: "标准横屏" },
    { value: "3:4", label: "标准竖屏" },
    { value: "21:9", label: "宽银幕" },
    { value: "adaptive", label: "自适应" },
] as const;

const seedancePixels = {
    "480p": {
        "16:9": "864x496",
        "4:3": "752x560",
        "1:1": "640x640",
        "3:4": "560x752",
        "9:16": "496x864",
        "21:9": "992x432",
    },
    "720p": {
        "16:9": "1280x720",
        "4:3": "1112x834",
        "1:1": "960x960",
        "3:4": "834x1112",
        "9:16": "720x1280",
        "21:9": "1470x630",
    },
    "1080p": {
        "16:9": "1920x1080",
        "4:3": "1664x1248",
        "1:1": "1440x1440",
        "3:4": "1248x1664",
        "9:16": "1080x1920",
        "21:9": "2206x946",
    },
    "4k": {
        "16:9": "3840x2160",
        "4:3": "3328x2496",
        "1:1": "2880x2880",
        "3:4": "2496x3328",
        "9:16": "2160x3840",
        "21:9": "4412x1892",
    },
} as const;

export function isSeedanceVideoConfig(config: AiConfig | Pick<AiConfig, "model" | "videoModel" | "baseUrl">) {
    const requestConfig = "channels" in config ? resolveModelRequestConfig(config, config.model || config.videoModel) : config;
    return isSeedanceVideoModel(modelOptionName(requestConfig.model || requestConfig.videoModel)) || isArkPlanBaseUrl(requestConfig.baseUrl);
}

export function isSeedanceVideoModel(model: string) {
    const value = model.toLowerCase();
    return value.includes("seedance") || value.includes("doubao-seedance");
}

export function isSeedanceFastModel(model: string) {
    return seedanceModelMode(model) === "fast";
}

export function seedanceModelMode(model: string): SeedanceModelMode {
    const value = model.trim().toLowerCase();
    if (!isSeedanceVideoModel(value)) return "unknown";
    if (value === "doubao-seedance-2-5-260628") return "seedance2.5";
    const tokens = value.replaceAll("_", "-").replaceAll(" ", "-").split("-").filter(Boolean);
    if (tokens.includes("fast")) return "fast";
    if (tokens.includes("mini")) return "mini";
    if (tokens.includes("pro")) return "pro";
    if (value === "doubao-seedance-2-0-260128" || value === "seedance-2.0" || value === "seedance2.0") return "pro";
    return "unknown";
}

export function seedanceResolutionOptionsForModel(model: string) {
    const mode = seedanceModelMode(model);
    return mode === "seedance2.5" ? seedanceBaseResolutionOptions : mode === "fast" || mode === "mini" ? seedanceResolutionOptions.filter((option) => option.value !== "4k") : seedanceResolutionOptions;
}

export type SeedanceReferenceLimits = Readonly<{
    images: number;
    videos: number;
    audios: number;
    totalVideoDurationSeconds: number;
    totalAudioDurationSeconds: number;
}>;

export function seedanceReferenceError(label: string, limits: SeedanceReferenceLimits, images: ReferenceImage[], videos: ReferenceVideo[], audios: ReferenceAudio[]) {
    if (images.length > limits.images) return `${label} 最多支持 ${limits.images} 张参考图片，当前 ${images.length} 张`;
    if (videos.length > limits.videos) return `${label} 最多支持 ${limits.videos} 个参考视频，当前 ${videos.length} 个`;
    if (audios.length > limits.audios) return `${label} 最多支持 ${limits.audios} 段参考音频，当前 ${audios.length} 段`;
    const videoError = seedanceDurationError(videos, limits.totalVideoDurationSeconds, `${label} 参考视频`);
    if (videoError) return videoError;
    return seedanceDurationError(audios, limits.totalAudioDurationSeconds, `${label} 参考音频`);
}

function seedanceDurationError(items: Array<ReferenceVideo | ReferenceAudio>, maximumSeconds: number, label: string) {
    let totalMilliseconds = 0;
    for (let index = 0; index < items.length; index += 1) {
        const durationMs = items[index].durationMs;
        if (!durationMs) continue;
        if (durationMs < 2_000 || durationMs > maximumSeconds * 1_000) return `${label} ${index + 1} 时长必须为 2–${maximumSeconds} 秒`;
        totalMilliseconds += durationMs;
    }
    if (totalMilliseconds > maximumSeconds * 1_000) return `${label}总时长不能超过 ${maximumSeconds} 秒`;
    return "";
}

export function isArkPlanBaseUrl(baseUrl: string) {
    return baseUrl.toLowerCase().includes("ark.cn-beijing.volces.com/api/plan/v3") || baseUrl.toLowerCase().includes("/api/plan/v3");
}

export function normalizeSeedanceResolution(value: string, model = "") {
    const normalized = normalizeResolutionToken(value);
    const supportedOptions = seedanceResolutionOptionsForModel(model);
    return supportedOptions.some((item) => item.value === normalized) ? normalized : "720p";
}

export function normalizeResolutionToken(value: string) {
    const normalizedValue = String(value || "")
        .trim()
        .toLowerCase();
    if (normalizedValue === "low") return "480p";
    if (normalizedValue === "auto" || normalizedValue === "high" || normalizedValue === "medium") return "720p";
    if (normalizedValue === "4k") return "4k";
    const resolution = normalizedValue.replace(/p$/i, "") || "720";
    return `${resolution}p`;
}

export function normalizeSeedanceDuration(value: string, model = "") {
    const duration = Math.round(Number(value));
    const maximum = seedanceModelMode(model) === "seedance2.5" ? 30 : 15;
    return Math.max(4, Math.min(maximum, Number.isFinite(duration) ? duration : 6));
}

export function normalizeSeedanceRatio(value: string) {
    if (!value || value === "auto" || value === "adaptive") return "adaptive";
    if (seedanceRatioOptions.some((item) => item.value === value)) return value;
    const match = value.match(/^(\d+)x(\d+)$/);
    if (!match) return "adaptive";
    const width = Number(match[1]);
    const height = Number(match[2]);
    if (!width || !height) return "adaptive";
    const ratio = width / height;
    const options = [
        ["16:9", 16 / 9],
        ["4:3", 4 / 3],
        ["1:1", 1],
        ["3:4", 3 / 4],
        ["9:16", 9 / 16],
        ["21:9", 21 / 9],
    ] as const;
    return options.reduce((best, item) => (Math.abs(item[1] - ratio) < Math.abs(best[1] - ratio) ? item : best), options[0])[0];
}

export function seedancePixelLabel(resolution: string, ratio: string) {
    const normalizedResolution = normalizeSeedanceResolution(resolution) as keyof typeof seedancePixels;
    const normalizedRatio = normalizeSeedanceRatio(ratio) as keyof (typeof seedancePixels)[typeof normalizedResolution] | "adaptive";
    if (normalizedRatio === "adaptive") return "自动匹配";
    return seedancePixels[normalizedResolution][normalizedRatio] || "";
}

export function boolConfig(value: string | undefined, fallback: boolean) {
    if (value === "true") return true;
    if (value === "false") return false;
    return fallback;
}

export function seedanceReferenceLabel(kind: "image" | "video" | "audio", index: number) {
    if (kind === "image") return `图片${index + 1}`;
    if (kind === "video") return `视频${index + 1}`;
    return `音频${index + 1}`;
}

export function buildSeedancePromptText(prompt: string, _images: ReferenceImage[], _videos: ReferenceVideo[], _audios: ReferenceAudio[]) {
    return prompt.trim();
}
