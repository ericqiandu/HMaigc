import type { ReactNode } from "react";

import { AppWorkspaceShell } from "@/components/layout/app-top-nav";
import { MarketingPopup } from "@/components/site/marketing-popup";

export default function UserLayout({ children }: { children: ReactNode }) {
    return (
        <div className="app-user-workspace h-dvh overflow-hidden text-foreground">
            <AppWorkspaceShell>{children}</AppWorkspaceShell>
            <MarketingPopup />
        </div>
    );
}
