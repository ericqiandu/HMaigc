import { describe, expect, test } from "bun:test";
import { readdir } from "node:fs/promises";

const homeHeader = await Bun.file(new URL("../src/pages/home/updream/updream-header.tsx", import.meta.url)).text();
const workspaceHeader = await Bun.file(new URL("../src/components/layout/workspace-top-bar.tsx", import.meta.url)).text();
const sharedBrand = Bun.file(new URL("../src/components/layout/site-brand-link.tsx", import.meta.url));
const sharedBrandStyles = Bun.file(new URL("../src/components/layout/site-brand-link.css", import.meta.url));
const sharedAccount = Bun.file(new URL("../src/components/layout/site-account-actions.tsx", import.meta.url));
const sharedAccountStyles = Bun.file(new URL("../src/components/layout/site-account-actions.css", import.meta.url));
const sharedReferral = Bun.file(new URL("../src/components/account/referral-reward-center.tsx", import.meta.url));
const sharedReferralStyles = Bun.file(new URL("../src/components/account/referral-reward-center.css", import.meta.url));
const legacyReferral = Bun.file(new URL("../src/pages/home/updream/referral-reward-center.tsx", import.meta.url));
const legacyReferralStyles = Bun.file(new URL("../src/pages/home/updream/referral-reward-center.css", import.meta.url));
const homeAccount = Bun.file(new URL("../src/pages/home/updream/updream-account-actions.tsx", import.meta.url));
const workspaceAccount = Bun.file(new URL("../src/components/layout/workspace-sidebar-footer.tsx", import.meta.url));
const homeStyles = Bun.file(new URL("../src/pages/home/updream/updream-home.css", import.meta.url));
const designTokens = Bun.file(new URL("../src/styles/design-tokens.css", import.meta.url));
const canvasTopBar = await Bun.file(new URL("../src/pages/canvas/canvas-project-top-bar.tsx", import.meta.url)).text();
const workspaceShell = await Bun.file(new URL("../src/components/layout/app-top-nav.tsx", import.meta.url)).text();
const layoutDirectory = new URL("../src/components/layout/", import.meta.url);
const layoutSources = await Promise.all((await readdir(layoutDirectory)).filter((fileName) => fileName.endsWith(".tsx")).map(async (fileName) => [fileName, await Bun.file(new URL(fileName, layoutDirectory)).text()] as const));

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
        expect(source).toContain('import "./site-account-actions.css";');
        expect(source).toContain("useMembershipAction");
        expect(source).toContain("site-membership-icon");
        expect(source).not.toContain("Gem");
    });

    test("shared referral UI has an account owner without a homepage reverse dependency", async () => {
        expect(await sharedReferral.exists()).toBe(true);
        expect(await sharedReferralStyles.exists()).toBe(true);
        expect(await legacyReferral.exists()).toBe(false);
        expect(await legacyReferralStyles.exists()).toBe(false);

        const accountSource = await sharedAccount.text();
        const referralSource = await sharedReferral.text();
        expect(accountSource).toContain("@/components/account/referral-reward-center");
        expect(referralSource).toContain('export const OPEN_REFERRAL_CENTER_EVENT = "open-ai-canvas:open-referral-center";');
        for (const [fileName, source] of layoutSources) {
            expect(source, `${fileName} must not import from a homepage owner`).not.toMatch(/from\s+["']@\/pages\/home\//);
        }
    });

    test("hidden guest membership copy keeps an accessible link name", async () => {
        const source = await sharedAccount.text();
        expect(source).toMatch(/<Link to="\/membership" className="site-account-upgrade[^"]*" aria-label="升级会员">/);
    });

    test("every visible shared account action keeps a 44px touch target through 639px", async () => {
        const styles = await sharedAccountStyles.text();
        const touchStart = styles.indexOf("@media (max-width: 639px)");
        const compactStart = styles.indexOf("@media (max-width: 520px)");
        expect(touchStart).toBeGreaterThanOrEqual(0);
        expect(compactStart).toBeGreaterThan(touchStart);
        const touchContract = styles.slice(touchStart, compactStart);
        for (const selector of [".site-account-upgrade", ".site-account-auth", ".site-account-notifications", ".site-account-balance", ".site-account-member", ".site-account-trigger", ".site-account-pill", ".referral-reward-trigger"]) {
            expect(touchContract, `${selector} must participate in the 639px touch contract`).toContain(selector);
        }
        expect(touchContract).toContain("min-width: 44px");
        expect(touchContract).toContain("min-height: 44px");
    });

    test("shared account actions keep desktop density and mobile touch targets", async () => {
        const styles = await sharedAccountStyles.text();
        expect(styles).toContain("height: var(--space-7)");
        expect(styles).toContain("@media (max-width: 639px)");
        expect(styles).toContain("@media (max-width: 520px)");
        expect(styles).toContain("min-height: 44px");
        expect(styles).toContain("z-index: var(--layer-header)");
        expect(styles).toContain("border: 1px solid var(--border-light)");
        expect(styles).toContain("gap: var(--space-2)");
        expect(styles).toContain("padding-right: var(--space-3)");
        expect(styles).toContain("padding-left: var(--space-3)");
        expect(styles).not.toMatch(/\b(?:6|13)px\b/);
        expect(styles).not.toContain("box-shadow:");
        expect(styles).not.toContain("#172033");
        expect(styles).not.toContain("#ffffff");
    });

    test("shared header and account menu interactions use one semantic focus-visible ring", async () => {
        const styles = await sharedAccountStyles.text();
        const tokens = await designTokens.text();
        expect(tokens).toContain("--focus-ring: var(--brand-primary)");
        for (const selector of [
            ".site-account-upgrade",
            ".site-account-auth",
            ".site-account-notifications",
            ".site-account-balance",
            ".site-account-member",
            ".site-account-trigger",
            ".referral-reward-trigger",
            ".site-account-menu-link",
            ".site-account-theme-switch",
            ".site-account-logout",
        ]) {
            expect(styles, `${selector} must expose an explicit focus-visible selector`).toContain(`${selector}:focus-visible`);
        }
        expect(styles).toContain("outline: 2px solid var(--focus-ring)");
        expect(styles).toContain("outline-offset: 2px");
    });

    test("shared logout uses semantic danger styling without private Tailwind colors", async () => {
        const source = await sharedAccount.text();
        const styles = await sharedAccountStyles.text();
        expect(source).not.toMatch(/(?:hover:)?(?:bg|text)-red-(?:500|600)/);
        expect(styles).toMatch(/\.site-account-logout\s*\{[^}]*color:\s*var\(--status-danger\)/);
        expect(styles).toMatch(/\.site-account-logout:hover\s*\{[^}]*color-mix\(in srgb, var\(--status-danger\)/);
    });

    test("shared markup and homepage CSS no longer retain dead homepage owners", async () => {
        const source = await sharedAccount.text();
        const styles = await homeStyles.text();
        expect(source).not.toContain("updream-membership-icon-layer");
        expect(source).toContain("site-membership-icon-layer");
        expect(styles).not.toContain(".updream-header-logo");
    });

    test("workspace reuses the homepage account actions without a parallel menu", async () => {
        expect(workspaceHeader).toContain("<SiteAccountActions />");
        expect(workspaceHeader).not.toContain("WorkspaceSidebarFooter");
        expect(await workspaceAccount.exists()).toBe(false);
    });

    test("canvas editor and independent surfaces stay outside the workspace header", () => {
        expect(workspaceShell).toContain('pathname === "/membership"');
        expect(workspaceShell).toContain('pathname === "/credit-store"');
        expect(workspaceShell).toContain('pathname.startsWith("/admin")');
        expect(workspaceShell).toContain("/^\\/canvas\\/[^/]+/.test(pathname)");
        expect(workspaceShell).not.toContain('pathname === "/canvas"');
        expect(canvasTopBar).not.toContain("SiteAccountActions");
        expect(canvasTopBar).not.toContain("SiteBrandLink");
    });
});
