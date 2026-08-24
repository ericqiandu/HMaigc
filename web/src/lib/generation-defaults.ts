export const DEFAULT_MEDIA_ASPECT_RATIO = "16:9";
export const DEFAULT_VIDEO_RESOLUTION = "720p";

export function resolveMediaAspectRatio(explicitSize: string | undefined): string {
    return explicitSize?.trim() || DEFAULT_MEDIA_ASPECT_RATIO;
}

export function resolveVideoResolution(explicitResolution: string | undefined): string {
    return explicitResolution?.trim() || DEFAULT_VIDEO_RESOLUTION;
}
