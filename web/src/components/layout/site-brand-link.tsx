import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import "@/components/layout/site-brand-link.css";

export function SiteBrandLink() {
    const { settings } = useSiteSettings();

    return (
        <Link className="site-brand-link" data-logo-source={settings.logoUrl ? "custom" : "default"} to="/" aria-label={`${settings.siteName} 首页`}>
            <span className="site-brand-mark" aria-hidden="true">
                <img className="site-brand-image" src={siteLogoURL(settings)} alt="" />
            </span>
            <span className="site-brand-name">{settings.siteName}</span>
        </Link>
    );
}
