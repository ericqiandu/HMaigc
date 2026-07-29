import { Drawer, Tooltip } from "antd";
import {
    BarChart3,
    BadgeDollarSign,
    BellRing,
    ChevronRight,
    Coins,
    CreditCard,
    Crown,
    FileClock,
    Gift,
    HardDrive,
    Home,
    Globe2,
    Mail,
    Menu,
    MessageSquareText,
    PanelLeftClose,
    PanelLeftOpen,
    RadioTower,
    Settings2,
    ShieldCheck,
    TicketCheck,
    UsersRound,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";

import { AppChangelogButton } from "@/components/layout/app-changelog-modal";
import { WORKSPACE_SIDEBAR_STORAGE_KEY } from "@/components/layout/workspace-sidebar-state";
import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { cn } from "@/lib/utils";
import "@/styles/workspace-ui.css";
import "@/styles/workspace-shell.css";
import "../admin-workspace.css";

type AdminNavigationItem = {
    path: string;
    label: string;
    description: string;
    icon: ReactNode;
};

const adminNavigation: Array<{ label: string; items: AdminNavigationItem[] }> = [
    {
        label: "概览",
        items: [{ path: "/admin", label: "数据概览", description: "活跃、调用与成本趋势", icon: <BarChart3 className="size-4" /> }],
    },
    {
        label: "平台资源",
        items: [
            { path: "/admin/membership", label: "会员商业化", description: "套餐、权益与会员订单", icon: <Crown className="size-4" /> },
            { path: "/admin/users", label: "用户管理", description: "账号、角色与状态", icon: <UsersRound className="size-4" /> },
            { path: "/admin/models", label: "AI 模型配置", description: "渠道接入、模型目录与启停", icon: <RadioTower className="size-4" /> },
            { path: "/admin/storyboard-prompts", label: "分镜提示词", description: "Agent 提示词版本", icon: <MessageSquareText className="size-4" /> },
        ],
    },
    {
        label: "运营",
        items: [
            { path: "/admin/referrals", label: "邀请有礼", description: "邀请码、首购奖励与资格", icon: <Gift className="size-4" /> },
            { path: "/admin/model-pricing", label: "模型商业定价", description: "成本、积分售价与利润率", icon: <BadgeDollarSign className="size-4" /> },
            { path: "/admin/announcements", label: "系统公告", description: "发布、关闭与历史公告", icon: <BellRing className="size-4" /> },
            { path: "/admin/credit-operations", label: "积分运营", description: "人工调账与异常计费", icon: <Coins className="size-4" /> },
            { path: "/admin/redemption-codes", label: "兑换码", description: "生成与查看兑换码批次", icon: <TicketCheck className="size-4" /> },
            { path: "/admin/logs", label: "请求明细", description: "上游调用与费用", icon: <FileClock className="size-4" /> },
        ],
    },
    {
        label: "系统配置",
        items: [
            { path: "/admin/settings/runtime-policy", label: "资源与策略", description: "配额、并发、频控与超时", icon: <Settings2 className="size-4" /> },
            { path: "/admin/settings/access", label: "登录与注册", description: "注册策略与 Linux.do", icon: <ShieldCheck className="size-4" /> },
            { path: "/admin/settings/email", label: "邮件服务", description: "注册验证码 SMTP", icon: <Mail className="size-4" /> },
            { path: "/admin/settings/storage", label: "存储服务", description: "OSS 与资源存储", icon: <HardDrive className="size-4" /> },
            { path: "/admin/settings/payment", label: "支付配置", description: "收银台与商户参数", icon: <CreditCard className="size-4" /> },
            { path: "/admin/settings/site", label: "站点设置", description: "品牌、版权与法律内容", icon: <Globe2 className="size-4" /> },
        ],
    },
];

export function AdminShell() {
    const [collapsed, setCollapsed] = useState(() => window.localStorage.getItem(WORKSPACE_SIDEBAR_STORAGE_KEY) === "1");
    const { settings } = useSiteSettings();
    const toggleCollapsed = () => {
        setCollapsed((current) => {
            const next = !current;
            window.localStorage.setItem(WORKSPACE_SIDEBAR_STORAGE_KEY, next ? "1" : "0");
            return next;
        });
    };

    return (
        <main className="app-user-workspace admin-workspace workspace-ui-scope flex h-full min-h-0 overflow-hidden text-foreground">
            <aside className={cn("app-workspace-sidebar admin-sidebar hidden shrink-0 flex-col overflow-hidden lg:flex", collapsed ? "w-16" : "w-[236px]")}>
                <div className={cn("admin-sidebar-brand flex h-16 shrink-0 items-center", collapsed ? "justify-center" : "gap-2.5 px-4")}>
                    {!collapsed ? (
                        <Link to="/" className="flex min-w-0 flex-1 items-center gap-2" title={settings.siteName}>
                            <span className="admin-brand-mark grid size-8 shrink-0 place-items-center overflow-hidden rounded-md bg-foreground/[.06]">
                                <img className="admin-brand-image size-5 object-contain" src={siteLogoURL(settings)} alt="" />
                            </span>
                            <span className="admin-brand-copy min-w-0">
                                <span className="admin-brand-name block truncate text-sm font-semibold">{settings.siteName}</span>
                                <span className="admin-brand-caption block truncate text-[9px] font-medium tracking-[0.16em] text-foreground/38">ADMIN CONSOLE</span>
                            </span>
                        </Link>
                    ) : null}
                    <Tooltip title={collapsed ? "展开侧栏" : "折叠侧栏"} placement="right">
                        <button type="button" className="app-workspace-icon-button shrink-0" onClick={toggleCollapsed} aria-label={collapsed ? "展开侧栏" : "折叠侧栏"}>
                            {collapsed ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
                        </button>
                    </Tooltip>
                </div>
                <AdminNavigation collapsed={collapsed} />
                <AdminNavigationFooter collapsed={collapsed} />
            </aside>
            <section className="admin-workspace-main flex min-w-0 flex-1 flex-col overflow-hidden">
                <MobileAdminNavigation />
                <Outlet />
            </section>
        </main>
    );
}

export function AdminPageFrame({ title, description, actions, children }: { title: string; description: string; actions?: ReactNode; children: ReactNode }) {
    const location = useLocation();
    const currentGroup = adminNavigation.find((group) => group.items.some((item) => item.path === location.pathname))?.label ?? "管理";

    return (
        <main className="admin-page thin-scrollbar h-full overflow-y-auto">
            <div className="admin-page-frame mx-auto w-full">
                <header className="admin-page-header flex flex-wrap justify-between">
                    <div className="admin-page-heading min-w-0">
                        <div className="admin-page-breadcrumb flex items-center">
                            <span className="admin-page-breadcrumb-root">管理后台</span>
                            <ChevronRight className="admin-page-breadcrumb-separator size-3" />
                            <span className="admin-page-breadcrumb-current">{currentGroup}</span>
                        </div>
                        <h1 className="admin-page-title">{title}</h1>
                        <p className="admin-page-description max-w-2xl">{description}</p>
                    </div>
                    {actions ? <div className="admin-page-actions flex shrink-0 items-center">{actions}</div> : null}
                </header>
                <div className="admin-page-content">{children}</div>
            </div>
        </main>
    );
}

function MobileAdminNavigation() {
    const [open, setOpen] = useState(false);
    const location = useLocation();
    const { settings } = useSiteSettings();
    const currentItem = adminNavigation.flatMap((group) => group.items).find((item) => item.path === location.pathname);

    return (
        <>
            <header className="admin-mobile-header flex shrink-0 items-center justify-between lg:hidden">
                <Link to="/" className="admin-mobile-brand flex min-w-0 items-center" title={settings.siteName}>
                    <span className="admin-mobile-brand-mark grid shrink-0 place-items-center overflow-hidden">
                        <img className="admin-mobile-brand-image object-contain" src={siteLogoURL(settings)} alt="" />
                    </span>
                    <span className="admin-mobile-brand-copy min-w-0">
                        <span className="admin-mobile-brand-name block truncate">{settings.siteName}</span>
                        <span className="admin-mobile-page-name block truncate">{currentItem?.label ?? "管理后台"}</span>
                    </span>
                </Link>
                <button type="button" className="admin-mobile-menu-button app-workspace-icon-button" onClick={() => setOpen(true)} aria-label="打开管理后台导航" aria-expanded={open}>
                    <Menu className="admin-mobile-menu-icon size-4" />
                </button>
            </header>
            <Drawer
                rootClassName="admin-mobile-navigation-drawer workspace-ui-scope"
                title={
                    <div className="admin-mobile-drawer-title">
                        <span className="admin-mobile-drawer-title-primary">管理后台</span>
                        <span className="admin-mobile-drawer-title-secondary">{settings.siteName}</span>
                    </div>
                }
                placement="left"
                width={320}
                open={open}
                onClose={() => setOpen(false)}
            >
                <div className="admin-mobile-drawer-body flex h-full min-h-0 flex-col">
                    <AdminNavigation collapsed={false} onNavigate={() => setOpen(false)} />
                    <AdminNavigationFooter collapsed={false} onNavigate={() => setOpen(false)} />
                </div>
            </Drawer>
        </>
    );
}

function AdminNavigation({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
    return (
        <nav className="admin-navigation thin-scrollbar flex-1 overflow-y-auto px-2.5 py-3" aria-label="管理后台菜单">
            {adminNavigation.map((group) => (
                <div key={group.label} className="admin-navigation-group mb-4">
                    {!collapsed ? <div className="admin-navigation-label mb-1.5 px-2.5 text-[10px] font-semibold tracking-[0.08em] text-foreground/32">{group.label}</div> : <div className="admin-navigation-divider mx-auto mb-2 h-px w-7 bg-border/70" />}
                    <div className="admin-navigation-items space-y-0.5">
                        {group.items.map((item) => (
                            <Tooltip key={item.path} title={collapsed ? item.label : undefined} placement="right">
                                <NavLink
                                    to={item.path}
                                    end={item.path === "/admin"}
                                    onClick={onNavigate}
                                    className={({ isActive }) =>
                                        cn(
                                            "app-workspace-nav-link relative flex h-9 items-center rounded-md text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                                            collapsed ? "justify-center px-0" : "gap-2.5 px-2.5",
                                            isActive ? "is-active font-medium" : "text-foreground/62 hover:bg-foreground/[.05] hover:text-foreground",
                                        )
                                    }
                                >
                                    <span className="admin-navigation-icon grid size-4 shrink-0 place-items-center">{item.icon}</span>
                                    {!collapsed ? <span className="admin-navigation-text truncate">{item.label}</span> : null}
                                </NavLink>
                            </Tooltip>
                        ))}
                    </div>
                </div>
            ))}
        </nav>
    );
}

function AdminNavigationFooter({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
    return (
        <div className="admin-sidebar-footer shrink-0">
            <Tooltip title={collapsed ? "更新日志" : undefined} placement="right">
                <AppChangelogButton
                    className={cn("admin-sidebar-footer-action flex w-full items-center transition-colors", collapsed ? "justify-center px-0" : "gap-2 px-2.5")}
                    showVersion={!collapsed}
                />
            </Tooltip>
            <Tooltip title={collapsed ? "返回创作台" : undefined} placement="right">
                <NavLink to="/canvas" onClick={onNavigate} className={cn("admin-sidebar-footer-action flex items-center transition-colors", collapsed ? "justify-center px-0" : "gap-2 px-2.5")}>
                    <Home className="admin-sidebar-footer-icon size-3.5" />
                    {!collapsed ? <span className="admin-sidebar-footer-label">返回创作台</span> : null}
                </NavLink>
            </Tooltip>
        </div>
    );
}
