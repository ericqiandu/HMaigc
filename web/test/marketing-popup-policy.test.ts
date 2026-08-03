import { describe, expect, test } from "bun:test";

import { isMarketingPopupRoute, marketingPopupCampaignKey, marketingPopupStorage, recordMarketingPopupExposure, shouldShowMarketingPopup, type MarketingPopupCampaign } from "../src/components/site/marketing-popup-policy";

const campaign: MarketingPopupCampaign = {
    marketingPopupImageUrl: "/api/public/site/marketing-image?v=1",
    marketingPopupTitle: "旗舰模型预售上线",
    marketingPopupDescription: "限时活动",
    marketingPopupActionLabel: "立即参与",
    marketingPopupActionUrl: "https://hmaigc.ai/membership",
    marketingPopupFrequency: "once",
};

function memoryStorage() {
    const values = new Map<string, string>();
    return {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
    };
}

describe("marketing popup policy", () => {
    test("shows once per user and campaign after the exposure is recorded", () => {
        const storage = memoryStorage();
        const key = marketingPopupCampaignKey("user-a", campaign);
        expect(shouldShowMarketingPopup(storage, key)).toBe(true);
        recordMarketingPopupExposure(storage, key);
        expect(shouldShowMarketingPopup(storage, key)).toBe(false);
        expect(marketingPopupCampaignKey("user-b", campaign)).not.toBe(key);
    });

    test("creates a new campaign key when campaign content changes", () => {
        const original = marketingPopupCampaignKey("user-a", campaign);
        const changed = marketingPopupCampaignKey("user-a", { ...campaign, marketingPopupTitle: "新一轮活动" });
        expect(changed).not.toBe(original);
    });

    test("uses the local date for daily frequency", () => {
        const daily = { ...campaign, marketingPopupFrequency: "daily" as const };
        expect(marketingPopupCampaignKey("user-a", daily, new Date(2026, 7, 3, 9))).toContain("2026-08-03");
        expect(marketingPopupCampaignKey("user-a", daily, new Date(2026, 7, 4, 9))).not.toBe(marketingPopupCampaignKey("user-a", daily, new Date(2026, 7, 3, 9)));
    });

    test("uses session storage only for session frequency", () => {
        const local = memoryStorage();
        const session = memoryStorage();
        expect(marketingPopupStorage("session", local, session)).toBe(session);
        expect(marketingPopupStorage("once", local, session)).toBe(local);
        expect(marketingPopupStorage("daily", local, session)).toBe(local);
    });

    test("allows authenticated user routes but excludes the administration workspace", () => {
        expect(isMarketingPopupRoute("/")).toBe(true);
        expect(isMarketingPopupRoute("/canvas/example")).toBe(true);
        expect(isMarketingPopupRoute("/admin")).toBe(false);
        expect(isMarketingPopupRoute("/admin/settings/site")).toBe(false);
    });
});
