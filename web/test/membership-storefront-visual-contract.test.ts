import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dir, "..");

describe("会员商城交付稿视觉契约", () => {
    test("保留原稿横幅、双层切换和深色套餐卡布局", () => {
        const page = readFileSync(resolve(root, "src/pages/membership/index.tsx"), "utf8");
        const pricing = readFileSync(resolve(root, "src/pages/membership/membership-storefront-pricing.tsx"), "utf8");
        const promo = readFileSync(resolve(root, "src/pages/membership/membership-storefront-promo.tsx"), "utf8");
        expect(page).toContain('className="membership-storefront-page min-h-screen bg-[#070b11] font-sans antialiased"');
        expect(page).not.toContain("membership-storefront-page-title");
        expect(page).not.toContain("membership-storefront-close");
        expect(promo).toContain("membership-storefront-promo");
        expect(pricing).toContain("membership-storefront-audience-tabs mt-10 flex justify-center gap-14");
        expect(pricing).toContain("membership-storefront-cycle-switch mx-auto flex rounded-full");
        expect(pricing).toContain("border-[#232c38] bg-[#0f151e]");
        expect(pricing).toContain('const featured = plan.tier === "ultra"');
        expect(pricing).toContain("cycleOfferLabel(allPlans, audience, availableCycle)");
        expect(pricing).toContain('className="membership-storefront-plan-recommendation"');
        expect(page).toContain('navigate("/credit-store")');
    });
});
