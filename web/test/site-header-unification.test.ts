import { describe, expect, test } from "bun:test";

const homeHeader = await Bun.file(new URL("../src/pages/home/updream/updream-header.tsx", import.meta.url)).text();
const workspaceHeader = await Bun.file(new URL("../src/components/layout/workspace-top-bar.tsx", import.meta.url)).text();
const sharedBrand = Bun.file(new URL("../src/components/layout/site-brand-link.tsx", import.meta.url));
const sharedBrandStyles = Bun.file(new URL("../src/components/layout/site-brand-link.css", import.meta.url));
const sharedAccount = Bun.file(new URL("../src/components/layout/site-account-actions.tsx", import.meta.url));
const sharedAccountStyles = Bun.file(new URL("../src/components/layout/site-account-actions.css", import.meta.url));
const homeAccount = Bun.file(new URL("../src/pages/home/updream/updream-account-actions.tsx", import.meta.url));

describe("site header unification", () => {
    test("home and workspace share one brand owner", async () => {
        expect(await sharedBrand.exists()).toBe(true);
        expect(homeHeader).toContain("<SiteBrandLink />");
        expect(workspaceHeader).toContain("<SiteBrandLink />");
        expect(homeHeader).not.toContain("siteLogoURL(");
        expect(workspaceHeader).not.toContain("siteLogoURL(");
    });

    test("shared brand owns the homepage dimensions", async () => {
        const styles = await sharedBrandStyles.text();
        expect(styles).toContain("height: var(--space-7)");
        expect(styles).toContain("width: 22px");
        expect(styles).toContain("width: 26px");
        expect(styles).toContain("min-height: 44px");
    });

    test("homepage account actions have a shared owner", async () => {
        expect(await sharedAccount.exists()).toBe(true);
        expect(homeHeader).toContain("<SiteAccountActions />");
        expect(await homeAccount.exists()).toBe(false);
        const source = await sharedAccount.text();
        expect(source).toContain("useMembershipAction");
        expect(source).toContain("site-membership-icon");
        expect(source).not.toContain("Gem");
    });

    test("shared account actions keep desktop density and mobile touch targets", async () => {
        const styles = await sharedAccountStyles.text();
        expect(styles).toContain("height: var(--space-7)");
        expect(styles).toContain("@media (max-width: 520px)");
        expect(styles).toContain("min-height: 44px");
        expect(styles).not.toContain("#172033");
        expect(styles).not.toContain("#ffffff");
    });
});
