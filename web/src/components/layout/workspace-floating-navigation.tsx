import { Home } from "lucide-react";
import { NavLink } from "react-router";

import { navigationTools } from "@/constant/navigation-tools";
import { cn } from "@/lib/utils";
import { useUserStore } from "@/stores/use-user-store";

export function WorkspaceFloatingNavigation() {
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);

    if (!hydrated || !user) return null;

    return (
        <nav className="workspace-floating-navigation" aria-label="工作台导航">
            <NavLink
                to="/"
                end
                className={({ isActive }) => cn("workspace-floating-navigation-link", isActive && "is-active")}
                aria-label="首页"
                title="首页"
            >
                <span className="workspace-floating-navigation-icon-frame">
                    <Home className="workspace-floating-navigation-icon" aria-hidden="true" />
                </span>
                <span className="workspace-floating-navigation-label">首页</span>
            </NavLink>
            {navigationTools.map((tool) => {
                const Icon = tool.icon;
                return (
                    <NavLink
                        key={tool.slug}
                        to={`/${tool.slug}`}
                        className={({ isActive }) => cn("workspace-floating-navigation-link", isActive && "is-active")}
                        aria-label={tool.label}
                        title={tool.label}
                    >
                        <span className="workspace-floating-navigation-icon-frame">
                            <Icon className="workspace-floating-navigation-icon" aria-hidden="true" />
                        </span>
                        <span className="workspace-floating-navigation-label">{tool.label}</span>
                    </NavLink>
                );
            })}
        </nav>
    );
}
