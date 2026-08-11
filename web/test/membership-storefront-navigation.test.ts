import { describe, expect, test } from "bun:test";

import { membershipStorefrontExitIntent, shouldExitMembershipStorefront } from "../src/pages/membership/membership-storefront-navigation";

describe("membership storefront navigation", () => {
    test("returns to the previous page only when browser history exposes a positive index", () => {
        expect(membershipStorefrontExitIntent({ idx: 3 })).toBe("back");
        expect(membershipStorefrontExitIntent({ idx: 0 })).toBe("home");
        expect(membershipStorefrontExitIntent({ idx: -1 })).toBe("home");
        expect(membershipStorefrontExitIntent({ idx: Number.NaN })).toBe("home");
        expect(membershipStorefrontExitIntent({ idx: "3" })).toBe("home");
        expect(membershipStorefrontExitIntent({})).toBe("home");
        expect(membershipStorefrontExitIntent(null)).toBe("home");
    });

    test("Escape exits only when the payment dialog does not own dismissal", () => {
        expect(shouldExitMembershipStorefront("Escape", false)).toBe(true);
        expect(shouldExitMembershipStorefront("Escape", true)).toBe(false);
        expect(shouldExitMembershipStorefront("Enter", false)).toBe(false);
    });
});
