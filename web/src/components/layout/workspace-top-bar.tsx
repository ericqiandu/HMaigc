import { SiteBrandLink } from "@/components/layout/site-brand-link";
import { WorkspaceSidebarFooter } from "@/components/layout/workspace-sidebar-footer";
import "@/components/layout/workspace-top-bar.css";

export function WorkspaceTopBar() {
    return (
        <header className="workspace-top-bar" aria-label="工作台顶部栏">
            <div className="workspace-top-bar-inner">
                <SiteBrandLink />

                <div className="workspace-top-bar-account">
                    <WorkspaceSidebarFooter collapsedClassName="workspace-top-bar-account-collapsed" expandedClassName="workspace-top-bar-account-expanded" accountClassName="workspace-top-bar-account-trigger" compact />
                </div>
            </div>
        </header>
    );
}
