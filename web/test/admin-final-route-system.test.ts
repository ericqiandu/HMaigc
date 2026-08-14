import { describe, expect, test } from "bun:test";
import postcss from "postcss";

import { ADMIN_NAVIGATION_GROUPS } from "../src/pages/admin/components/admin-navigation";
import { formatCompactNumberInput } from "../src/pages/admin/components/admin-form-system";
import { MODEL_CENTER_SECTIONS } from "../src/pages/admin/components/admin-model-center-tabs";

const readSource = (path: string) => Bun.file(new URL(path, import.meta.url)).text();

function declarationsWithinMobileMedia(source: string, selector: string) {
    const declarations = new Map<string, string>();
    postcss.parse(source).walkAtRules("media", (media) => {
        if (!media.params.includes("max-width: 639px")) return;
        media.walkRules((rule) => {
            if (!rule.selectors.includes(selector)) return;
            rule.walkDecls((item) => declarations.set(item.prop, item.value));
        });
    });
    return declarations;
}

describe("admin final route system", () => {
    test("keeps high-density editors on the shared form and modal contract", async () => {
        const [shell, membership, creditStore, creditOperations, redemptionCodes, formStyles] = await Promise.all([
            readSource("../src/pages/admin/components/admin-shell.tsx"),
            readSource("../src/pages/admin/membership/membership-page.tsx"),
            readSource("../src/pages/admin/credit-store/credit-store-page.tsx"),
            readSource("../src/pages/admin/components/credit-operations-panel.tsx"),
            readSource("../src/pages/admin/components/redemption-codes-panel.tsx"),
            readSource("../src/pages/admin/admin-form-system.css"),
        ]);

        expect(shell).toContain('import "../admin-form-system.css"');
        expect(membership).toContain("AdminFormSection");
        expect(membership).toContain("admin-operation-modal admin-membership-plan-modal");
        expect(creditStore).toContain("AdminFormSection");
        expect(creditStore).toContain("admin-operation-modal admin-credit-product-modal");
        expect(creditOperations).toContain("precision={6} step={0.000001} formatter={formatCompactNumberInput}");
        expect(creditOperations).toContain("precision={4} step={0.0001} formatter={formatCompactNumberInput}");
        expect(redemptionCodes).toContain("precision={6} step={0.000001} formatter={formatCompactNumberInput}");
        expect(formStyles).toContain(".admin-form-section");
        expect(formStyles).toContain(".admin-operation-modal .ant-modal-body");
    });

    test("shows decimal facts compactly without changing their persisted precision contract", () => {
        expect(formatCompactNumberInput(100)).toBe("100");
        expect(formatCompactNumberInput("10.000000")).toBe("10");
        expect(formatCompactNumberInput("1.250000")).toBe("1.25");
        expect(formatCompactNumberInput(undefined)).toBe("");
    });

    test("keeps every production admin destination in one unique navigation contract", () => {
        const navigationPaths = ADMIN_NAVIGATION_GROUPS.flatMap((group) => group.items.map((item) => item.path));
        const modelCenterPaths = MODEL_CENTER_SECTIONS.map((section) => section.path);

        expect(navigationPaths).toEqual([
            "/admin",
            "/admin/users",
            "/admin/membership",
            "/admin/referrals",
            "/admin/models",
            "/admin/voices",
            "/admin/super-resolution-pricing",
            "/admin/storyboard-prompts",
            "/admin/credit-operations",
            "/admin/credit-store",
            "/admin/redemption-codes",
            "/admin/announcements",
            "/admin/settings/legal",
            "/admin/settings/site",
            "/admin/settings/access",
            "/admin/settings/payment",
            "/admin/settings/email",
            "/admin/settings/storage",
            "/admin/settings/runtime-policy",
            "/admin/logs",
            "/admin/operations",
        ]);
        expect(new Set(navigationPaths).size).toBe(navigationPaths.length);
        expect(modelCenterPaths).toEqual(["/admin/models", "/admin/models/kuaizi", "/admin/models/pricing"]);
    });

    test("keeps every navigation destination registered by the production router", async () => {
        const router = await readSource("../src/router.tsx");
        const navigationPaths = ADMIN_NAVIGATION_GROUPS.flatMap((group) => group.items.map((item) => item.path));
        const routePaths = new Set([...navigationPaths, ...MODEL_CENTER_SECTIONS.map((section) => section.path)]);

        for (const path of routePaths) {
            const registeredPath = path === "/admin" ? "/admin" : path.slice("/admin/".length);
            expect(router).toContain(`path: "${registeredPath}"`);
        }
    });

    test("keeps mobile overflow navigation and log details on the shared 44px touch contract", async () => {
        const responsiveStyles = await readSource("../src/pages/admin/admin-responsive.css");
        const tabsMore = declarationsWithinMobileMedia(responsiveStyles, ".admin-workspace .ant-tabs-nav-more");
        const logDetails = declarationsWithinMobileMedia(responsiveStyles, ".admin-workspace .admin-log-detail-button.ant-btn");

        expect(tabsMore.get("min-height")).toBe("44px");
        expect(logDetails.get("min-height")).toBe("44px");
    });
});
