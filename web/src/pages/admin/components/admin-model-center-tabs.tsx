import { KeyRound, RadioTower, WalletCards } from "lucide-react";
import { NavLink, useInRouterContext } from "react-router";

import { cn } from "@/lib/utils";

export const MODEL_CENTER_SECTIONS = [
    { path: "/admin/models", label: "渠道与模型", description: "接入渠道并维护用户可用模型", icon: RadioTower },
    { path: "/admin/models/kuaizi", label: "筷子账号", description: "维护统一服务地址、Key 与验证状态", icon: KeyRound },
    { path: "/admin/models/pricing", label: "价格与 Agent", description: "配置成本、积分售价与全站 Agent 模型", icon: WalletCards },
] as const;

export function AdminModelCenterTabs() {
    const inRouter = useInRouterContext();

    return (
        <nav className="admin-model-center-tabs" aria-label="模型中心配置流程">
            {MODEL_CENTER_SECTIONS.map((section) => {
                const Icon = section.icon;
                const content = (
                    <>
                        <Icon className="admin-model-center-tab-icon" aria-hidden="true" />
                        <span className="admin-model-center-tab-copy">
                            <span className="admin-model-center-tab-label">{section.label}</span>
                            <span className="admin-model-center-tab-description">{section.description}</span>
                        </span>
                    </>
                );
                return inRouter ? (
                    <NavLink key={section.path} to={section.path} end className={({ isActive }) => cn("admin-model-center-tab", isActive && "is-active")}>
                        {({ isActive }) => (
                            <>
                                {content}
                                {isActive ? <span className="admin-model-center-tab-status">当前</span> : null}
                            </>
                        )}
                    </NavLink>
                ) : (
                    <a key={section.path} href={section.path} className="admin-model-center-tab">
                        {content}
                    </a>
                );
            })}
        </nav>
    );
}
