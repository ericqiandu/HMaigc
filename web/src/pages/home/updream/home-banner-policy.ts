import type { SiteSettings } from "@/services/api/site-settings";

export type HomeBannerStorage = Pick<Storage, "getItem" | "setItem">;

export type HomeBannerCampaign = Pick<SiteSettings, "homeBannerLabel" | "homeBannerText" | "homeBannerPrimaryActionLabel" | "homeBannerPrimaryActionUrl" | "homeBannerSecondaryActionLabel" | "homeBannerSecondaryActionUrl" | "homeBannerFrequency">;

export function homeBannerCampaignKey(campaign: HomeBannerCampaign, date = new Date()) {
    const prefix = `hmaigc:home-banner:${hashCampaign(campaign)}`;
    return campaign.homeBannerFrequency === "daily" ? `${prefix}:${localDateKey(date)}` : prefix;
}

export function homeBannerStorage(frequency: SiteSettings["homeBannerFrequency"], local: HomeBannerStorage, session: HomeBannerStorage) {
    return frequency === "session" ? session : local;
}

export function shouldShowHomeBanner(frequency: SiteSettings["homeBannerFrequency"], storage: HomeBannerStorage, key: string) {
    return frequency === "always" || storage.getItem(key) !== "exposed";
}

export function recordHomeBannerExposure(frequency: SiteSettings["homeBannerFrequency"], storage: HomeBannerStorage, key: string) {
    if (frequency !== "always") storage.setItem(key, "exposed");
}

function hashCampaign(campaign: HomeBannerCampaign) {
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
