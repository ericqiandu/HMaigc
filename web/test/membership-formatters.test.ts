import { describe, expect, test } from "bun:test";

import { publicPlanName } from "@/pages/membership/membership-formatters";

describe("publicPlanName", () => {
    test("uses public product names for legacy default plans", () => {
        expect(publicPlanName({ name: "Pro", tier: "pro" })).toBe("标准版");
        expect(publicPlanName({ name: "团队 Max", tier: "max" })).toBe("高级版");
        expect(publicPlanName({ name: "Ultra", tier: "ultra" })).toBe("至尊版");
    });

    test("preserves administrator configured plan names", () => {
        expect(publicPlanName({ name: "品牌定制版", tier: "max" })).toBe("品牌定制版");
    });
});
