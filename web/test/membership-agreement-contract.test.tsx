import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { legalDocumentDefinitions, legalDocumentRoutes } from "../src/constants/legal-documents";
import { LegalDocumentView } from "../src/pages/legal/legal-document-page";
import type { SiteSettings, UpdateLegalSettingsInput } from "../src/services/api/site-settings";

const typedSiteSetting = { membershipAgreement: "" } satisfies Pick<SiteSettings, "membershipAgreement">;
const typedLegalUpdate = {
    userAgreement: "<p>用户协议</p>",
    privacyPolicy: "<p>隐私政策</p>",
    membershipAgreement: "<p>会员协议</p>",
} satisfies UpdateLegalSettingsInput;

describe("membership agreement contract", () => {
    test("shares one typed admin and public document definition", () => {
        expect(typedSiteSetting.membershipAgreement).toBe("");
        expect(typedLegalUpdate.membershipAgreement).toBe("<p>会员协议</p>");
        expect(legalDocumentRoutes.membershipAgreement).toBe("/legal/membership-agreement");
        expect(legalDocumentDefinitions.map((document) => document.kind)).toEqual(["userAgreement", "privacyPolicy", "membershipAgreement"]);
        expect(legalDocumentDefinitions[2]).toMatchObject({
            kind: "membershipAgreement",
            title: "HMaigc会员服务协议",
            emptyMessage: "会员服务协议尚未发布",
        });
    });

    test("renders the published HMaigc membership agreement", () => {
        const markup = renderToStaticMarkup(
            createElement(LegalDocumentView, {
                document: "membershipAgreement",
                content: "<h2>会员购买规则</h2><p>这是平台已发布的真实会员条款。</p>",
                loading: false,
                error: null,
                onRetry: () => undefined,
            }),
        );

        expect(markup).toContain("HMaigc会员服务协议");
        expect(markup).toContain("legal-document-body");
        expect(markup).not.toContain("会员服务协议尚未发布");
        expect(markup).not.toContain("法律内容加载失败");
    });

    test("distinguishes unpublished content from a load failure", () => {
        const unpublished = renderToStaticMarkup(
            createElement(LegalDocumentView, {
                document: "membershipAgreement",
                content: "",
                loading: false,
                error: null,
                onRetry: () => undefined,
            }),
        );
        expect(unpublished).toContain("会员服务协议尚未发布");
        expect(unpublished).not.toContain("法律内容加载失败");

        const failed = renderToStaticMarkup(
            createElement(LegalDocumentView, {
                document: "membershipAgreement",
                content: "",
                loading: false,
                error: new Error("公开站点接口不可用"),
                onRetry: () => undefined,
            }),
        );
        expect(failed).toContain("法律内容加载失败");
        expect(failed).toContain("公开站点接口不可用");
        expect(failed).not.toContain("会员服务协议尚未发布");
    });

    test("keeps previously loaded agreement content visible during a refresh failure", () => {
        const markup = renderToStaticMarkup(
            createElement(LegalDocumentView, {
                document: "membershipAgreement",
                content: "<p>上次成功读取的会员协议</p>",
                loading: false,
                error: new Error("刷新公开协议失败"),
                onRetry: () => undefined,
            }),
        );

        expect(markup).toContain("法律内容加载失败");
        expect(markup).toContain("legal-document-body");
        expect(markup).not.toContain("会员服务协议尚未发布");
    });
});
