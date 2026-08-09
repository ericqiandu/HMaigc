import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { generationSectionsFromForm, generationSectionsToForm } from "../src/pages/admin/membership/membership-storefront-form-domain";

const pageSource = readFileSync(resolve(import.meta.dir, "../src/pages/admin/membership/membership-page.tsx"), "utf8");
const panelSource = readFileSync(resolve(import.meta.dir, "../src/pages/admin/membership/membership-storefront-admin-panel.tsx"), "utf8");
const apiSource = readFileSync(resolve(import.meta.dir, "../src/services/api/membership.ts"), "utf8");

describe("会员商城后台配置契约", () => {
    test("会员管理提供独立商城展示入口并使用服务端配置接口", () => {
        expect(pageSource).toContain('key: "storefront"');
        expect(pageSource).toContain("<MembershipStorefrontAdminPanel />");
        expect(panelSource).toContain("getAdminMembershipStorefront");
        expect(panelSource).toContain("updateAdminMembershipStorefront");
        expect(apiSource).toContain('api.get("/admin/membership/storefront")');
        expect(apiSource).toContain('api.put("/admin/membership/storefront", input)');
    });

    test("后台覆盖会员页全部展示参数且不复制套餐计费字段", () => {
        for (const field of ["promotion", "copy", "activities", "commonFeatures", "exclusiveFeatures", "planHighlights", "generationColumns", "generationSections", "generationFootnote", "membershipNotes", "faqs"]) {
            expect(panelSource).toContain(field);
        }
        expect(panelSource).not.toContain("priceCents");
        expect(panelSource).not.toContain("creditsPerPeriod");
        expect(panelSource).not.toContain("imageConcurrency");
        expect(panelSource).not.toContain("videoConcurrency");
    });

    test("生成数量按行编辑，保留带千位逗号的单个数值", () => {
        const sections = [
            {
                title: "视频模型",
                rows: [{ model: "Seedance", icon: "♪", unit: "秒", values: ["604", "1,215", "2,444"] }],
            },
        ];

        const formSections = generationSectionsToForm(sections);
        expect(formSections[0]?.rows[0]?.values).toBe("604\n1,215\n2,444");
        expect(generationSectionsFromForm(formSections)).toEqual(sections);
        expect(panelSource).toContain("可保留数值中的千位逗号");
    });
});
