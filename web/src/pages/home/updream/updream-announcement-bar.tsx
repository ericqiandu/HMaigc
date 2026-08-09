import { X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import { homeBannerCampaignKey, homeBannerStorage, recordHomeBannerExposure, shouldShowHomeBanner } from "./home-banner-policy";

export function UpdreamAnnouncementBar() {
    const [visible, setVisible] = useState(false);
    const [dismissedCampaignKey, setDismissedCampaignKey] = useState("");
    const [desktopViewport, setDesktopViewport] = useState(() => typeof window !== "undefined" && window.matchMedia("(min-width: 768px)").matches);
    const { settings, loading } = useSiteSettings();
    const campaignKey = useMemo(() => homeBannerCampaignKey(settings), [settings]);

    useEffect(() => {
        const media = window.matchMedia("(min-width: 768px)");
        const updateViewport = () => setDesktopViewport(media.matches);
        updateViewport();
        media.addEventListener("change", updateViewport);
        return () => media.removeEventListener("change", updateViewport);
    }, []);

    useEffect(() => {
        if (loading || !desktopViewport || !settings.homeBannerEnabled || !settings.homeBannerText || dismissedCampaignKey === campaignKey) {
            setVisible(false);
            return;
        }
        const storage = homeBannerStorage(settings.homeBannerFrequency, window.localStorage, window.sessionStorage);
        if (!shouldShowHomeBanner(settings.homeBannerFrequency, storage, campaignKey)) {
            setVisible(false);
            return;
        }
        const timer = window.setTimeout(() => {
            recordHomeBannerExposure(settings.homeBannerFrequency, storage, campaignKey);
            setVisible(true);
        }, 0);
        return () => window.clearTimeout(timer);
    }, [campaignKey, desktopViewport, dismissedCampaignKey, loading, settings.homeBannerEnabled, settings.homeBannerFrequency, settings.homeBannerText]);

    if (!visible) return null;

    return (
        <div className="updream-announcement relative flex min-h-10 items-center justify-center bg-[#d9edfc] px-12 py-2 text-[13px]">
            <div className="updream-announcement-content flex flex-wrap items-center justify-center gap-2.5 sm:gap-3">
                {settings.homeBannerLabel ? <span className="updream-announcement-badge rounded-full px-2.5 py-0.5 text-[12px] font-medium text-white">{settings.homeBannerLabel}</span> : null}
                <span className="updream-announcement-copy text-center text-[#1f2d3d]">{settings.homeBannerText}</span>
                {settings.homeBannerPrimaryActionUrl ? (
                    <a className="updream-announcement-submit rounded-full px-4 py-1 text-[12px] font-medium text-white transition-opacity hover:opacity-90" href={settings.homeBannerPrimaryActionUrl}>
                        {settings.homeBannerPrimaryActionLabel}
                    </a>
                ) : null}
                {settings.homeBannerSecondaryActionUrl ? (
                    <a className="updream-announcement-share rounded-full border border-[#c4d7e8] bg-white px-4 py-1 text-[12px] font-medium text-[#1f2d3d] transition-colors hover:bg-[#f2f8fd]" href={settings.homeBannerSecondaryActionUrl}>
                        {settings.homeBannerSecondaryActionLabel}
                    </a>
                ) : null}
            </div>
            <button
                type="button"
                onClick={() => {
                    setDismissedCampaignKey(campaignKey);
                    setVisible(false);
                }}
                className="updream-announcement-close absolute right-4 text-[#3b4a5c] transition-colors hover:text-black sm:right-5"
                aria-label="关闭首页横幅"
            >
                <X className="updream-announcement-close-icon size-4" />
            </button>
        </div>
    );
}
