import "./setup-happy-dom";

import { afterEach, describe, expect, mock, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { LegalDocumentKind } from "../src/constants/legal-documents";

type LegalResponse = {
    document: LegalDocumentKind;
    content: string;
    updatedAt: string;
};

const legalRequests: LegalDocumentKind[] = [];
let requestLegalDocument: (document: LegalDocumentKind) => Promise<LegalResponse>;

mock.module("@/services/api/site-settings", () => ({
    publicSiteSettingsQueryKey: ["public-site-settings"],
    publicLegalDocumentQueryKey: (document: LegalDocumentKind) => ["public-legal-document", document],
    getPublicSiteSettings: async () => ({
        siteName: "HMaigc",
        homeHeroSlogan: "让算力更有想象力！",
        logoUrl: "",
        footerCopyright: "",
        icpRegistrationNumber: "",
        icpRegistrationUrl: "",
        publicSecurityRegistrationNumber: "",
        publicSecurityRegistrationUrl: "",
        homeBannerEnabled: false,
        homeBannerLabel: "",
        homeBannerText: "",
        homeBannerPrimaryActionLabel: "",
        homeBannerPrimaryActionUrl: "",
        homeBannerSecondaryActionLabel: "",
        homeBannerSecondaryActionUrl: "",
        homeBannerFrequency: "always",
        marketingPopupEnabled: false,
        marketingPopupImageUrl: "",
        marketingPopupTitle: "",
        marketingPopupDescription: "",
        marketingPopupActionLabel: "",
        marketingPopupActionUrl: "",
        marketingPopupFrequency: "once",
        updatedAt: "",
    }),
    getPublicLegalDocument: (document: LegalDocumentKind) => {
        legalRequests.push(document);
        return requestLegalDocument(document);
    },
}));

let root: Root | null = null;
let queryClient: QueryClient | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    queryClient?.clear();
    queryClient = null;
    legalRequests.length = 0;
    document.body.replaceChildren();
});

async function renderLegalPage(documentKind: LegalDocumentKind) {
    const [{ SiteSettingsProvider }, { LegalDocumentPage }] = await Promise.all([
        import("../src/components/site/site-settings-provider"),
        import("../src/pages/legal/legal-document-page"),
    ]);
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => {
        root?.render(
            createElement(
                QueryClientProvider,
                { client: queryClient },
                createElement(SiteSettingsProvider, null, createElement(LegalDocumentPage, { document: documentKind })),
            ),
        );
        await Promise.resolve();
    });
    return host;
}

async function waitForText(host: HTMLElement, selector: string, expected: string) {
    for (let attempt = 0; attempt < 40; attempt += 1) {
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 5));
        });
        const text = host.querySelector(selector)?.textContent;
        if (text?.includes(expected)) return;
    }
    throw new Error(`等待 ${selector} 显示 ${expected} 超时`);
}

describe("public legal document loading", () => {
    test("loads only the requested legal document and keeps its loading state local", async () => {
        let resolveDocument: ((value: LegalResponse) => void) | undefined;
        requestLegalDocument = (documentKind) =>
            new Promise<LegalResponse>((resolve) => {
                resolveDocument = resolve;
            });

        const host = await renderLegalPage("privacyPolicy");

        expect(legalRequests).toEqual(["privacyPolicy"]);
        expect(host.querySelector(".legal-document-skeleton")).not.toBeNull();

        await act(async () => {
            resolveDocument?.({ document: "privacyPolicy", content: "<p>独立加载的隐私政策正文</p>", updatedAt: "2026-08-24T00:00:00Z" });
        });
        await waitForText(host, ".legal-document-body", "独立加载的隐私政策正文");
        expect(host.querySelector(".legal-document-body")?.textContent).toContain("独立加载的隐私政策正文");
    });

    test("shows the real legal request failure and retries the same document", async () => {
        let attempt = 0;
        requestLegalDocument = async (documentKind) => {
            attempt += 1;
            if (attempt === 1) throw new Error("法律正文服务暂时不可用");
            return { document: documentKind, content: "<p>重试后的用户协议</p>", updatedAt: "2026-08-24T00:00:00Z" };
        };

        const host = await renderLegalPage("userAgreement");
        await waitForText(host, ".legal-document-error", "法律正文服务暂时不可用");

        expect(host.querySelector(".legal-document-error")?.textContent).toContain("法律正文服务暂时不可用");
        const retry = host.querySelector<HTMLButtonElement>(".legal-document-retry");
        await act(async () => {
            retry?.click();
        });
        await waitForText(host, ".legal-document-body", "重试后的用户协议");
        expect(legalRequests).toEqual(["userAgreement", "userAgreement"]);
        expect(host.querySelector(".legal-document-body")?.textContent).toContain("重试后的用户协议");
    });
});
