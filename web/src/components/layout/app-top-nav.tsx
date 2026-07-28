import { type ReactNode, useEffect } from "react";
import { useLocation, useNavigate } from "react-router";

import { ModelSetupGuide } from "@/components/layout/model-setup-guide";
import { WorkspaceFloatingNavigation } from "@/components/layout/workspace-floating-navigation";
import { WorkspaceSidebarFooter } from "@/components/layout/workspace-sidebar-footer";

export function AppWorkspaceShell({ children }: { children: ReactNode }) {
    const { pathname } = useLocation();
    const navigate = useNavigate();
    const hideChrome = pathname === "/"
        || pathname === "/membership"
        || pathname.startsWith("/admin")
        || /^\/canvas\/[^/]+/.test(pathname);

    useEffect(() => {
        const handleWorkspaceNavigation = (rawEvent: Event) => {
            const event = rawEvent as CustomEvent<{ to?: string }>;
            if (!event.detail?.to) return;
            event.preventDefault();
            navigate(event.detail.to);
        };
        window.addEventListener("workspace:navigate", handleWorkspaceNavigation);
        return () => window.removeEventListener("workspace:navigate", handleWorkspaceNavigation);
    }, [navigate]);

    return (
        <>
            <div className="app-product-shell flex h-dvh min-h-0 w-full flex-col overflow-hidden">
                {!hideChrome ? (
                    <>
                        <WorkspaceFloatingNavigation />
                        <div className="app-product-floating-account">
                            <WorkspaceSidebarFooter
                                collapsedClassName="justify-center px-0"
                                expandedClassName="hidden"
                                accountClassName="size-9 justify-center p-0"
                                compact
                            />
                        </div>
                    </>
                ) : null}
                <div className="app-product-content relative min-h-0 min-w-0 flex-1 overflow-hidden">{children}</div>
            </div>
            <ModelSetupGuide
                hidden={pathname === "/"
                    || pathname === "/login"
                    || pathname === "/register"
                    || pathname === "/membership"
                    || pathname.startsWith("/admin")}
            />
        </>
    );
}
