const staticBaseURL = import.meta.env.BASE_URL || "/";

export function staticAssetURL(assetPath: string): string {
    const value = assetPath.trim();
    if (!value || /^(?:https?:|data:|blob:)/i.test(value)) return value;
    const relativePath = value.replace(/^\/+/, "");
    if (/^https?:\/\//i.test(staticBaseURL)) {
        return new URL(relativePath, ensureTrailingSlash(staticBaseURL)).toString();
    }
    return `${ensureTrailingSlash(staticBaseURL)}${relativePath}`;
}

function ensureTrailingSlash(value: string): string {
    return value.endsWith("/") ? value : `${value}/`;
}
