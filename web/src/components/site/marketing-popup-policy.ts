import type { SiteSettings } from "@/services/api/site-settings";

export type MarketingPopupStorage = Pick<Storage, "getItem" | "setItem">;

export type MarketingPopupCampaign = Pick<
    SiteSettings,
    | "marketingPopupImageUrl"
    | "marketingPopupTitle"
    | "marketingPopupDescription"
    | "marketingPopupActionLabel"
    | "marketingPopupActionUrl"
    | "marketingPopupFrequency"
>;

export function marketingPopupCampaignKey(userId: string, campaign: MarketingPopupCampaign, date = new Date()) {
    const fingerprint = hashCampaign(campaign);
    const prefix = `hmaigc:marketing-popup:${userId}:${fingerprint}`;
    if (campaign.marketingPopupFrequency === "daily") {
        return `${prefix}:${localDateKey(date)}`;
    }
    return prefix;
}

export function marketingPopupStorage(frequency: SiteSettings["marketingPopupFrequency"], local: MarketingPopupStorage, session: MarketingPopupStorage) {
    return frequency === "session" ? session : local;
}

export function shouldShowMarketingPopup(storage: MarketingPopupStorage, key: string) {
    return storage.getItem(key) !== "exposed";
}

export function recordMarketingPopupExposure(storage: MarketingPopupStorage, key: string) {
    storage.setItem(key, "exposed");
}

export function isMarketingPopupRoute(pathname: string) {
    return pathname !== "/admin" && !pathname.startsWith("/admin/");
}

function hashCampaign(campaign: MarketingPopupCampaign) {
    const source = JSON.stringify(campaign);
    let hash = 2166136261;
    for (let index = 0; index < source.length; index += 1) {
        hash ^= source.charCodeAt(index);
        hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(36);
}

function localDateKey(date: Date) {
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, "0");
    const day = String(date.getDate()).padStart(2, "0");
    return `${year}-${month}-${day}`;
}
