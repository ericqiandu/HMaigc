import { X } from "lucide-react";
import { useState } from "react";

import { useSiteSettings } from "@/components/site/site-settings-provider";

export function UpdreamAnnouncementBar() {
    const [visible, setVisible] = useState(true);
    const { settings } = useSiteSettings();

    if (!visible || !settings.homeBannerEnabled) return null;

    return (
        <div className="updream-announcement relative flex min-h-10 items-center justify-center bg-[#d9edfc] px-12 py-2 text-[13px]">
            <div className="updream-announcement-content flex flex-wrap items-center justify-center gap-2.5 sm:gap-3">
                {settings.homeBannerLabel ? <span className="updream-announcement-badge rounded-full px-2.5 py-0.5 text-[12px] font-medium text-white">{settings.homeBannerLabel}</span> : null}
                <span className="updream-announcement-copy text-center text-[#1f2d3d]">
                    {settings.homeBannerText}
                </span>
                {settings.homeBannerPrimaryActionUrl ? <a className="updream-announcement-submit rounded-full px-4 py-1 text-[12px] font-medium text-white transition-opacity hover:opacity-90" href={settings.homeBannerPrimaryActionUrl}>{settings.homeBannerPrimaryActionLabel}</a> : null}
                {settings.homeBannerSecondaryActionUrl ? <a className="updream-announcement-share rounded-full border border-[#c4d7e8] bg-white px-4 py-1 text-[12px] font-medium text-[#1f2d3d] transition-colors hover:bg-[#f2f8fd]" href={settings.homeBannerSecondaryActionUrl}>{settings.homeBannerSecondaryActionLabel}</a> : null}
            </div>
            <button
                type="button"
                onClick={() => setVisible(false)}
                className="updream-announcement-close absolute right-4 text-[#3b4a5c] transition-colors hover:text-black sm:right-5"
                aria-label="关闭招募公告"
            >
                <X className="updream-announcement-close-icon size-4" />
            </button>
        </div>
    );
}
