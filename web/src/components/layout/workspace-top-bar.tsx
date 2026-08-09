import { SiteAccountActions } from "@/components/layout/site-account-actions";
import { SiteBrandLink } from "@/components/layout/site-brand-link";
import "@/components/layout/workspace-top-bar.css";

export function WorkspaceTopBar() {
    return (
        <header className="workspace-top-bar" aria-label="工作台顶部栏">
            <div className="workspace-top-bar-inner">
                <SiteBrandLink />

                <div className="workspace-top-bar-account">
                    <SiteAccountActions />
                </div>
            </div>
        </header>
    );
}
