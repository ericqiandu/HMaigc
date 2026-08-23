import { lazy, Suspense } from "react";

import { SiteBrandLink } from "@/components/layout/site-brand-link";
import "@/components/layout/workspace-top-bar.css";

const SiteAccountActions = lazy(() => import("@/components/layout/site-account-actions").then((module) => ({ default: module.SiteAccountActions })));

export function WorkspaceTopBar() {
    return (
        <header className="workspace-top-bar" aria-label="工作台顶部栏">
            <div className="workspace-top-bar-inner">
                <SiteBrandLink />

                <div className="workspace-top-bar-account">
                    <Suspense fallback={<div className="site-account-loading w-[236px] animate-pulse rounded-full" aria-label="正在读取账户信息" />}>
                        <SiteAccountActions />
                    </Suspense>
                </div>
            </div>
        </header>
    );
}
