import { describe, expect, test } from "bun:test";

const PAGE_FILES = [
    "../src/pages/admin/components/admin-announcements-panel.tsx",
    "../src/pages/admin/components/credit-operations-panel.tsx",
    "../src/pages/admin/components/redemption-codes-panel.tsx",
    "../src/pages/admin/credit-store/credit-store-page.tsx",
    "../src/pages/admin/logs/logs-page.tsx",
] as const;

describe("admin operations unified layout", () => {
    test("hard-cuts every remaining operations page to the shared Pro data layout", async () => {
        const sources = await Promise.all(PAGE_FILES.map((path) => Bun.file(new URL(path, import.meta.url)).text()));

        for (const source of sources) {
            expect(source).toContain("AdminDataLayout");
            expect(source).toContain("AdminContentSection");
        }
    });

    test("removes legacy nested settings cards and private table headings", async () => {
        const [announcements, credits, redemptions, store, logs] = await Promise.all(PAGE_FILES.map((path) => Bun.file(new URL(path, import.meta.url)).text()));

        expect(credits).not.toContain("SettingsSectionCard");
        expect(redemptions).not.toContain("SettingsSectionCard");
        expect(announcements).not.toContain("admin-announcements-heading");
        expect(redemptions).not.toContain("admin-redemption-records-heading");
        expect(store).toContain('className="admin-credit-store-content-section"');
        expect(logs).toContain('className="admin-log-content-section"');
    });
});
