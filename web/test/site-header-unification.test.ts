import { describe, expect, test } from "bun:test";
import { readdir } from "node:fs/promises";

import {
    collectCustomPropertyDefinitions,
    collectPositiveClassDeclarations,
    collectRouteScopedGemUsages,
    findComponentClassTokens,
    findMembershipSvgSignatures,
    inspectMembershipRoutes,
    readTsxSources,
    type NamedSource,
} from "./support/site-header-contract-parser";

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
const sharedAccountSource: NamedSource = { fileName: "components/layout/site-account-actions.tsx", source: await sharedAccount.text() };
const productionTsxSources = await readTsxSources(new URL("../src/", import.meta.url));
const membershipSvgOwners = findMembershipSvgSignatures(productionTsxSources);

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
    });

    test("membership SVG structure has exactly one recursive production owner", () => {
        expect(membershipSvgOwners).toHaveLength(1);
        expect(membershipSvgOwners[0].fileName).toBe("components/layout/site-account-actions.tsx");
        expect(membershipSvgOwners[0].viewBox).toBe("0 0 1024 1024");
        expect(membershipSvgOwners[0].pathCount).toBe(11);
        expect(membershipSvgOwners[0].layerClassTokens).toEqual([
            ["site-membership-icon-layer", "site-membership-icon-layer-1"],
            ["site-membership-icon-layer", "site-membership-icon-layer-2"],
            ["site-membership-icon-layer", "site-membership-icon-layer-3"],
            ["site-membership-icon-layer", "site-membership-icon-layer-4"],
            ["site-membership-icon-layer", "site-membership-icon-layer-5"],
            ["site-membership-icon-layer", "site-membership-icon-layer-6"],
            ["site-membership-icon-layer", "site-membership-icon-layer-7"],
            ["site-membership-icon-layer", "site-membership-icon-layer-8"],
            ["site-membership-icon-layer", "site-membership-icon-layer-9"],
            ["site-membership-icon-layer", "site-membership-icon-layer-10"],
            ["site-membership-icon-layer", "site-membership-icon-layer-11"],
        ]);
    });

    test("every shared membership icon owner keeps the 16px size-4 contract", () => {
        const owner = membershipSvgOwners.find(({ fileName }) => fileName === sharedAccountSource.fileName);
        expect(owner).toBeDefined();
        if (!owner) throw new Error("shared membership SVG owner is missing");
        expect(owner.componentName).toBeDefined();
        const usages = findComponentClassTokens(sharedAccountSource, owner.componentName ?? "");
        expect(usages).toHaveLength(3);
        for (const usage of usages) {
            expect(usage).toContain("size-4");
        }
    });

    test("shared membership routes render the structural owner without Lucide Gem", () => {
        const owner = membershipSvgOwners.find(({ fileName }) => fileName === sharedAccountSource.fileName);
        expect(owner).toBeDefined();
        if (!owner) throw new Error("shared membership SVG owner is missing");
        expect(owner.componentName).toBeDefined();
        expect(inspectMembershipRoutes(sharedAccountSource, owner.componentName ?? "")).toEqual({
            entryCount: 3,
            entriesUsingSignatureOwner: 3,
            gemUsages: 0,
        });
    });

    test("recursive production membership routes contain no Lucide Gem usages", () => {
        expect(collectRouteScopedGemUsages(productionTsxSources)).toEqual([]);
    });

    test("route-scoped Gem inspection reports a membership entry usage", () => {
        const fixture: NamedSource = {
            fileName: "nested/membership-gem-fixture.tsx",
            source: `
                import { Gem as MembershipGem } from "lucide-react";

                export const MembershipFixture = () => (
                    <Link to="/membership" className="membership-link">
                        <MembershipGem className="membership-icon" />
                    </Link>
                );
            `,
        };
        expect(collectRouteScopedGemUsages([fixture])).toEqual([{ fileName: fixture.fileName, gemUsages: 1 }]);
    });

    test("Lucide Gem outside a membership route remains unrelated", () => {
        const fixture: NamedSource = {
            fileName: "unrelated-gem-fixture.tsx",
            source: `
                import { Gem } from "lucide-react";

                const HeaderMembershipDiamond = ({ className }: { className: string }) => <svg className={className} />;
                export const HeaderFixture = () => (
                    <div className="header-fixture">
                        <Link to="/membership" className="membership-link">
                            <HeaderMembershipDiamond className="membership-icon" />
                        </Link>
                        <Gem className="unrelated-decoration" />
                    </div>
                );
            `,
        };
        expect(inspectMembershipRoutes(fixture, "HeaderMembershipDiamond")).toEqual({ entryCount: 1, entriesUsingSignatureOwner: 1, gemUsages: 0 });
        expect(collectRouteScopedGemUsages([fixture])).toEqual([]);
    });

    test("credit accent has one root owner and no dark-theme override", async () => {
        expect(collectCustomPropertyDefinitions(await designTokens.text(), "--credit-accent")).toEqual([{ important: false, selectors: [":root"], value: "#5f6fff" }]);
    });

    test("selector AST ignores negated names, attribute strings, and declaration-free hover rules", () => {
        const misleadingCss = `
            :not(.site-account-balance-icon),
            [data-icon=".site-account-balance-icon"] {
                color: red !important;
                fill: red;
            }

            .site-account-balance-icon:hover {
                opacity: 0.8;
            }

            .site-account-balance-icon .child,
            .wrapper:has(.site-account-balance-icon) {
                color: red !important;
                fill: red;
            }
        `;
        expect(collectPositiveClassDeclarations(misleadingCss, "site-account-balance-icon", ["color", "fill"])).toEqual([]);
    });

    test("every positive credit-icon declaration preserves the unique color and fill contract", async () => {
        const declarations = collectPositiveClassDeclarations(await sharedAccountStyles.text(), "site-account-balance-icon", ["color", "fill"])
            .map(({ important, property, value }) => ({ important, property, value }))
            .sort((left, right) => left.property.localeCompare(right.property));
        expect(declarations).toEqual([
            { important: false, property: "color", value: "var(--credit-accent)" },
            { important: false, property: "fill", value: "currentcolor" },
        ]);

        expect(findComponentClassTokens(sharedAccountSource, "Zap")).toEqual([["site-account-balance-icon", "size-4"]]);
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
        expect(styles).not.toMatch(/\b6px\b/);
        expect(styles).not.toContain("#172033");
        expect(styles).not.toContain("#ffffff");
    });

    test("shared account popover owns one typography and interaction contract", async () => {
        const source = await sharedAccount.text();
        const styles = await sharedAccountStyles.text();

        expect(source).toContain('rootClassName="site-account-popover"');
        expect(source).not.toContain("text-sm font-semibold");
        expect(source).not.toContain("text-[11px]");
        expect(source).not.toContain("text-xs");
        expect(source).not.toContain("h-9");
        expect(source).not.toContain("h-10");

        expect(styles).toMatch(/\.site-account-menu\s*\{[^}]*font-family:\s*var\(--font-family-sans\)/);
        expect(styles).toMatch(/\.site-account-display-name\s*\{[^}]*font-size:\s*var\(--text-sm\)[^}]*line-height:\s*20px[^}]*font-weight:\s*var\(--font-semibold\)/);
        expect(styles).toMatch(/\.site-account-username,\s*\n\.site-account-balance-label\s*\{[^}]*font-size:\s*var\(--text-xs\)[^}]*line-height:\s*17px[^}]*font-weight:\s*var\(--font-regular\)/);
        expect(styles).toMatch(/\.site-account-balance-number\s*\{[^}]*font-size:\s*13px[^}]*line-height:\s*20px[^}]*font-weight:\s*var\(--font-semibold\)/);
        expect(styles).toMatch(/\.site-account-menu-link,\s*\n\.site-account-theme,\s*\n\.site-account-logout\s*\{[^}]*min-height:\s*32px[^}]*font-size:\s*13px[^}]*line-height:\s*20px[^}]*font-weight:\s*var\(--font-regular\)/);
        expect(styles).toMatch(/\.site-account-menu-icon,\s*\n\.site-account-logout-icon\s*\{[^}]*width:\s*16px[^}]*height:\s*16px/);
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
