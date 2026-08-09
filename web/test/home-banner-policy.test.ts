import { describe, expect, test } from "bun:test";

import { homeBannerCampaignKey, homeBannerStorage, recordHomeBannerExposure, shouldShowHomeBanner, type HomeBannerCampaign, type HomeBannerStorage } from "@/pages/home/updream/home-banner-policy";

const campaign: HomeBannerCampaign = {
    homeBannerLabel: "招募中",
    homeBannerText: "增长伙伴招募计划",
    homeBannerPrimaryActionLabel: "立即加入",
    homeBannerPrimaryActionUrl: "https://hmaigc.ai/join",
    homeBannerSecondaryActionLabel: "",
    homeBannerSecondaryActionUrl: "",
    homeBannerFrequency: "once",
};

function memoryStorage(): HomeBannerStorage & { values: Map<string, string> } {
    const values = new Map<string, string>();
    return {
        values,
        getItem: (key) => values.get(key) ?? null,
        setItem: (key, value) => void values.set(key, value),
    };
}

describe("home banner exposure policy", () => {
    test("shows a campaign once after its first recorded exposure", () => {
        const storage = memoryStorage();
        const key = homeBannerCampaignKey(campaign);
        expect(shouldShowHomeBanner("once", storage, key)).toBe(true);
        recordHomeBannerExposure("once", storage, key);
        expect(shouldShowHomeBanner("once", storage, key)).toBe(false);
    });

    test("creates a fresh daily exposure key on the next local day", () => {
        const daily = { ...campaign, homeBannerFrequency: "daily" as const };
        const first = homeBannerCampaignKey(daily, new Date(2026, 7, 9, 9));
        const next = homeBannerCampaignKey(daily, new Date(2026, 7, 10, 9));
        expect(first).not.toBe(next);
    });

    test("uses session storage only for session frequency", () => {
        const local = memoryStorage();
        const session = memoryStorage();
        expect(homeBannerStorage("session", local, session)).toBe(session);
        expect(homeBannerStorage("once", local, session)).toBe(local);
        expect(homeBannerStorage("daily", local, session)).toBe(local);
    });

    test("always frequency does not persist exposure", () => {
        const storage = memoryStorage();
        const key = homeBannerCampaignKey({ ...campaign, homeBannerFrequency: "always" });
        recordHomeBannerExposure("always", storage, key);
        expect(storage.values.size).toBe(0);
        expect(shouldShowHomeBanner("always", storage, key)).toBe(true);
    });

    test("content changes start a new campaign", () => {
        expect(homeBannerCampaignKey(campaign)).not.toBe(homeBannerCampaignKey({ ...campaign, homeBannerText: "新一轮增长伙伴招募计划" }));
    });
});
