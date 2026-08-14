import { describe, expect, test } from "bun:test";

const readSource = (path: string) => Bun.file(new URL(path, import.meta.url)).text();

describe("admin settings and operations layout", () => {
    test("uses exactly one shared settings workspace on every settings route", async () => {
        const [site, access, email, payment, storage, legal, runtime, routes] = await Promise.all([
            readSource("../src/pages/admin/settings/site-settings-page.tsx"),
            readSource("../src/pages/admin/components/access-settings-panel.tsx"),
            readSource("../src/pages/admin/components/email-settings-panel.tsx"),
            readSource("../src/pages/admin/settings/payment-settings-page.tsx"),
            readSource("../src/pages/admin/settings/storage-settings-page.tsx"),
            readSource("../src/pages/admin/settings/legal-settings-page.tsx"),
            readSource("../src/pages/admin/settings/runtime-policy-settings-page.tsx"),
            readSource("../src/pages/admin/admin-route-pages.tsx"),
        ]);

        for (const source of [site, access, email, payment, storage, legal, runtime]) {
            expect(source).toContain("admin-settings-page");
        }

        expect(routes).not.toContain('<div className="admin-settings-page"><Suspense fallback={<PageFallback label="邮件配置" />}');
    });

    test("hard-cuts operations to the shared Pro data layout", async () => {
        const operations = await readSource("../src/pages/admin/operations/operations-page.tsx");

        expect(operations).toContain("AdminDataLayout");
        expect(operations).toContain("AdminContentSection");
        expect(operations).toContain('className="operations-overview-section"');
        expect(operations).not.toContain("SettingsSectionCard");
        expect(operations).not.toContain('className="operations-page admin-data-page space-y-5"');
    });
});
