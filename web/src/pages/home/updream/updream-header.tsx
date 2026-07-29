import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { UpdreamAccountActions } from "@/pages/home/updream/updream-account-actions";
import "@/pages/home/updream/updream-sticky-header.css";

export function UpdreamHeader() {
    const { settings } = useSiteSettings();

    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <Link
                to="/"
                className="updream-header-logo-link"
                data-logo-source={settings.logoUrl ? "custom" : "default"}
                aria-label={`${settings.siteName} 首页`}
            >
                <span className="updream-header-logo-mark" aria-hidden="true">
                    <img className="updream-header-logo-image" src={siteLogoURL(settings)} alt="" />
                </span>
                <span className="updream-header-logo">{settings.siteName}</span>
            </Link>
            <UpdreamAccountActions />
        </header>
    );
}
