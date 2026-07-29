import { Link } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { WorkspaceSidebarFooter } from "@/components/layout/workspace-sidebar-footer";
import "@/components/layout/workspace-top-bar.css";

export function WorkspaceTopBar() {
    const { settings } = useSiteSettings();

    return (
        <header className="workspace-top-bar" aria-label="工作台顶部栏">
            <div className="workspace-top-bar-inner">
                <Link
                    to="/"
                    className="workspace-top-bar-brand"
                    data-logo-source={settings.logoUrl ? "custom" : "default"}
                    aria-label={`${settings.siteName} 首页`}
                >
                    <span className="workspace-top-bar-brand-mark" aria-hidden="true">
                        <img className="workspace-top-bar-brand-image" src={siteLogoURL(settings)} alt="" />
                    </span>
                    <span className="workspace-top-bar-brand-name">{settings.siteName}</span>
                </Link>

                <div className="workspace-top-bar-account">
                    <WorkspaceSidebarFooter
                        collapsedClassName="workspace-top-bar-account-collapsed"
                        expandedClassName="workspace-top-bar-account-expanded"
                        accountClassName="workspace-top-bar-account-trigger"
                        compact
                    />
                </div>
            </div>
        </header>
    );
}
