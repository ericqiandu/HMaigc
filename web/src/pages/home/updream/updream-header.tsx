import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { UpdreamAccountActions } from "@/pages/home/updream/updream-account-actions";
import "@/pages/home/updream/updream-sticky-header.css";

export function UpdreamHeader() {
    const { settings } = useSiteSettings();

    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <Link to="/" className="updream-header-logo-link flex min-w-0 items-center gap-2.5" aria-label={`${settings.siteName} 首页`}>
                <img className="updream-header-logo-image size-8 shrink-0 object-contain" src={siteLogoURL(settings)} alt="" />
                <span className="updream-header-logo truncate text-[22px] font-bold tracking-[-0.04em] text-white">
                    {settings.siteName}
                </span>
            </Link>
            <UpdreamAccountActions />
        </header>
    );
}
