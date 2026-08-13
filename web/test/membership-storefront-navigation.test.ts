import { describe, expect, test } from "bun:test";

import { STOREFRONT_EXIT_DESTINATION, shouldExitStorefront } from "../src/lib/storefront-navigation";

describe("storefront navigation", () => {
    test("会员开通页和积分超市关闭后统一返回首页", () => {
        expect(STOREFRONT_EXIT_DESTINATION).toBe("/");
    });

    test("Escape 仅在没有内层弹窗接管关闭时退出商城", () => {
        expect(shouldExitStorefront("Escape", false)).toBe(true);
        expect(shouldExitStorefront("Escape", true)).toBe(false);
        expect(shouldExitStorefront("Enter", false)).toBe(false);
    });
});
