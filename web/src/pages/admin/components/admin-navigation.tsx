import { Input, Tooltip } from "antd";
import {
    BadgeDollarSign,
    AudioLines,
    BarChart3,
    BellRing,
    ChevronDown,
    Coins,
    CreditCard,
    Crown,
    FileClock,
    Gift,
    Globe2,
    HardDrive,
    Mail,
    MessageSquareText,
    RadioTower,
    ScrollText,
    Search,
    ServerCog,
    Settings2,
    ShieldCheck,
    Sparkles,
    TicketCheck,
    UsersRound,
    type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { NavLink, useLocation } from "react-router";

import { cn } from "@/lib/utils";

export type AdminNavigationItem = {
    path: string;
    label: string;
    description: string;
    icon: LucideIcon;
};

export type AdminNavigationGroup = {
    id: string;
    label: string;
    items: AdminNavigationItem[];
};

export const ADMIN_NAVIGATION_GROUPS: AdminNavigationGroup[] = [
    {
        id: "overview",
        label: "核心概览",
        items: [{ path: "/admin", label: "数据概览", description: "活跃用户、模型调用、成本与成功率", icon: BarChart3 }],
    },
    {
        id: "users-growth",
        label: "用户与增长",
        items: [
            { path: "/admin/users", label: "用户管理", description: "账号、角色、状态与用户明细", icon: UsersRound },
            { path: "/admin/membership", label: "会员管理", description: "套餐、权益、会员订单与发票", icon: Crown },
            { path: "/admin/referrals", label: "邀请奖励", description: "邀请码、首购奖励与资格审核", icon: Gift },
        ],
    },
    {
        id: "models-cost",
        label: "模型与计费",
        items: [
            { path: "/admin/models", label: "AI 模型", description: "渠道接入、模型目录、图标与启停", icon: RadioTower },
            { path: "/admin/voices", label: "音色管理", description: "系统音色、克隆音色、权限与模型兼容", icon: AudioLines },
            { path: "/admin/model-pricing", label: "商业定价", description: "供应商成本、积分售价与利润率", icon: BadgeDollarSign },
            { path: "/admin/super-resolution-pricing", label: "超分定价", description: "独立视频增强成本、帧率档与积分售价", icon: Sparkles },
            { path: "/admin/storyboard-prompts", label: "分镜提示词", description: "Agent 分镜提示词模板与版本", icon: MessageSquareText },
        ],
    },
    {
        id: "operations",
        label: "运营与内容",
        items: [
            { path: "/admin/credit-operations", label: "积分管理", description: "积分规则、人工调账与异常计费", icon: Coins },
            { path: "/admin/redemption-codes", label: "兑换码", description: "生成、导出与查看兑换码批次", icon: TicketCheck },
            { path: "/admin/announcements", label: "系统公告", description: "发布、关闭与查看历史公告", icon: BellRing },
            { path: "/admin/settings/legal", label: "法律与协议", description: "用户协议、隐私政策与公开内容", icon: ScrollText },
        ],
    },
    {
        id: "system",
        label: "系统设置",
        items: [
            { path: "/admin/settings/site", label: "站点与品牌", description: "站点名称、Logo、首页横幅、版权与备案", icon: Globe2 },
            { path: "/admin/settings/access", label: "登录与注册", description: "注册策略、管理员账号与 Linux.do", icon: ShieldCheck },
            { path: "/admin/settings/payment", label: "支付配置", description: "收银台、微信与支付宝商户参数", icon: CreditCard },
            { path: "/admin/settings/email", label: "邮件服务", description: "注册验证码与 SMTP 发信配置", icon: Mail },
            { path: "/admin/settings/storage", label: "存储服务", description: "OSS、公开资源与云存储参数", icon: HardDrive },
        ],
    },
    {
        id: "security-operations",
        label: "安全与运维",
        items: [
            { path: "/admin/settings/runtime-policy", label: "运行策略", description: "并发、频控、配额、队列与超时", icon: Settings2 },
            { path: "/admin/logs", label: "请求日志", description: "上游调用、耗时、状态与费用明细", icon: FileClock },
            { path: "/admin/operations", label: "运维升级", description: "版本检查、备份、升级、回滚与操作审计", icon: ServerCog },
        ],
    },
];

export function findAdminNavigationGroup(pathname: string) {
    return ADMIN_NAVIGATION_GROUPS.find((group) => group.items.some((item) => item.path === pathname));
}

export function findAdminNavigationItem(pathname: string) {
    return ADMIN_NAVIGATION_GROUPS.flatMap((group) => group.items).find((item) => item.path === pathname);
}

export function AdminNavigation({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
    const location = useLocation();
    const activeGroup = findAdminNavigationGroup(location.pathname);
    const [query, setQuery] = useState("");
    const [expandedGroupIds, setExpandedGroupIds] = useState<Set<string>>(() => new Set(activeGroup ? [activeGroup.id] : ["overview"]));
    const normalizedQuery = query.trim().toLocaleLowerCase();
    const visibleGroups = useMemo(() => {
        if (!normalizedQuery) return ADMIN_NAVIGATION_GROUPS;
        return ADMIN_NAVIGATION_GROUPS.map((group) => ({
            ...group,
            items: group.items.filter((item) =>
                `${group.label} ${item.label} ${item.description}`.toLocaleLowerCase().includes(normalizedQuery),
            ),
        })).filter((group) => group.items.length > 0);
    }, [normalizedQuery]);

    useEffect(() => {
        if (!activeGroup) return;
        setExpandedGroupIds((current) => {
            if (current.has(activeGroup.id)) return current;
            return new Set([...current, activeGroup.id]);
        });
    }, [activeGroup]);

    const toggleGroup = (groupId: string) => {
        setExpandedGroupIds((current) => {
            const next = new Set(current);
            if (next.has(groupId)) {
                next.delete(groupId);
            } else {
                next.add(groupId);
            }
            return next;
        });
    };

    return (
        <nav className="admin-navigation thin-scrollbar flex-1 overflow-y-auto" aria-label="管理后台菜单">
            {!collapsed ? (
                <div className="admin-navigation-search">
                    <Input
                        className="admin-navigation-search-input"
                        aria-label="查找后台功能"
                        allowClear
                        autoComplete="off"
                        prefix={<Search className="admin-navigation-search-icon size-3.5" />}
                        placeholder="查找后台功能"
                        value={query}
                        onChange={(event) => setQuery(event.target.value)}
                    />
                </div>
            ) : null}
            <div className="admin-navigation-groups">
                {visibleGroups.map((group) => {
                    const expanded = collapsed || Boolean(normalizedQuery) || expandedGroupIds.has(group.id);
                    return (
                        <section key={group.id} className="admin-navigation-group">
                            {!collapsed ? (
                                <button
                                    type="button"
                                    className="admin-navigation-group-trigger"
                                    aria-expanded={expanded}
                                    onClick={() => toggleGroup(group.id)}
                                >
                                    <span className="admin-navigation-label">{group.label}</span>
                                    <span className="admin-navigation-group-meta">
                                        <span className="admin-navigation-group-count">{group.items.length}</span>
                                        <ChevronDown className={cn("admin-navigation-group-chevron size-3.5", expanded && "is-expanded")} />
                                    </span>
                                </button>
                            ) : (
                                <div className="admin-navigation-divider" />
                            )}
                            {expanded ? (
                                <div className="admin-navigation-items">
                                    {group.items.map((item) => {
                                        const Icon = item.icon;
                                        return (
                                            <Tooltip key={item.path} title={collapsed ? `${item.label} · ${item.description}` : undefined} placement="right">
                                                <NavLink
                                                    to={item.path}
                                                    end={item.path === "/admin"}
                                                    onClick={() => {
                                                        setQuery("");
                                                        onNavigate?.();
                                                    }}
                                                    className={({ isActive }) =>
                                                        cn(
                                                            "app-workspace-nav-link admin-navigation-link relative flex items-center transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                                                            collapsed ? "justify-center" : "gap-2.5",
                                                            isActive ? "is-active font-medium" : "text-foreground/62 hover:bg-foreground/[.05] hover:text-foreground",
                                                        )
                                                    }
                                                >
                                                    <span className="admin-navigation-icon grid size-4 shrink-0 place-items-center">
                                                        <Icon className="admin-navigation-item-icon size-4" />
                                                    </span>
                                                    {!collapsed ? <span className="admin-navigation-text truncate">{item.label}</span> : null}
                                                </NavLink>
                                            </Tooltip>
                                        );
                                    })}
                                </div>
                            ) : null}
                        </section>
                    );
                })}
                {!visibleGroups.length ? (
                    <div className="admin-navigation-empty" role="status">
                        未找到“{query.trim()}”
                    </div>
                ) : null}
            </div>
        </nav>
    );
}
