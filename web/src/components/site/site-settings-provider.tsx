import { useQuery } from "@tanstack/react-query";
import { createContext, useContext, useEffect, type ReactNode } from "react";

import { staticAssetURL } from "@/lib/static-assets";
import { getPublicSiteSettings, publicSiteSettingsQueryKey, type PublicSiteSettings } from "@/services/api/site-settings";

const bootstrapSiteSettings: PublicSiteSettings = {
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
};

type SiteSettingsContextValue = {
    settings: PublicSiteSettings;
    loading: boolean;
    error: Error | null;
    refresh: () => Promise<void>;
};

const SiteSettingsContext = createContext<SiteSettingsContextValue | null>(null);

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
    const query = useQuery({
        queryKey: publicSiteSettingsQueryKey,
        queryFn: getPublicSiteSettings,
        staleTime: 60_000,
    });
    const settings = query.data ?? bootstrapSiteSettings;

    useEffect(() => {
        document.title = settings.siteName;
        const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
        if (favicon) {
            favicon.href = settings.logoUrl || staticAssetURL("/logo.svg");
        }
    }, [settings.logoUrl, settings.siteName]);

    useEffect(() => {
        if (query.error) {
            console.error("站点配置加载失败", query.error);
        }
    }, [query.error]);

    return (
        <SiteSettingsContext.Provider
            value={{
                settings,
                loading: query.isLoading,
                error: query.error instanceof Error ? query.error : null,
                refresh: async () => {
                    await query.refetch();
                },
            }}
        >
            {children}
        </SiteSettingsContext.Provider>
    );
}

export function useSiteSettings() {
    const context = useContext(SiteSettingsContext);
    if (!context) throw new Error("useSiteSettings 必须在 SiteSettingsProvider 中使用");
    return context;
}

export function siteLogoURL(settings: Pick<PublicSiteSettings, "logoUrl">) {
    return settings.logoUrl || staticAssetURL("/logo.svg");
}
