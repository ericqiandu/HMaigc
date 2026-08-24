import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { flushSync } from "react-dom";
import { MemoryRouter } from "react-router";

const configuredSlogan = "从后台同步的首页口号";
const configuredSiteSettings = {
    siteName: "HMaigc",
    homeHeroSlogan: configuredSlogan,
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
};

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("home hero slogan", () => {
    test("renders the LCP slogan before mounting the interactive Agent composer", async () => {
        const [{ SiteSettingsProvider }, { UpdreamHero }] = await Promise.all([
            import("../src/components/site/site-settings-provider"),
            import("../src/pages/home/updream/updream-hero"),
        ]);
        const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        queryClient.setQueryData(["public-site-settings"], configuredSiteSettings);
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        act(() => {
            flushSync(() => {
                root?.render(
                    createElement(
                        QueryClientProvider,
                        { client: queryClient },
                        createElement(
                            SiteSettingsProvider,
                            null,
                            createElement(App, null, createElement(MemoryRouter, null, createElement(UpdreamHero))),
                        ),
                    ),
                );
            });
        });

        expect(document.querySelector(".updream-hero-title")?.textContent).toBe(configuredSlogan);
        expect(document.querySelector(".updream-home-agent-composer-loading")).not.toBeNull();
        expect(document.querySelector(".canvas-agent-composer-textarea")).toBeNull();
        queryClient.clear();
    });

    test("renders the slogan returned by public site settings", async () => {
        const [{ SiteSettingsProvider }, { UpdreamHero }] = await Promise.all([
            import("../src/components/site/site-settings-provider"),
            import("../src/pages/home/updream/updream-hero"),
        ]);
        const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
        queryClient.setQueryData(["public-site-settings"], configuredSiteSettings);
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () => {
            root?.render(
                createElement(
                    QueryClientProvider,
                    { client: queryClient },
                    createElement(
                        SiteSettingsProvider,
                        null,
                        createElement(App, null, createElement(MemoryRouter, null, createElement(UpdreamHero))),
                    ),
                ),
            );
        });

        expect(document.querySelector(".updream-hero-title")?.textContent).toBe(configuredSlogan);
        queryClient.clear();
    });
});
