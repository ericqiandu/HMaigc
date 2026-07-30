import { App, Popover, Switch } from "antd";
import { BookOpenCheck, ChevronRight, Coins, Gem, LogIn, LogOut, Moon, Settings2, ShieldCheck, Sun, UserRound, UsersRound, Zap } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router";

import { IdentityProviderBadge } from "@/components/layout/identity-provider-badge";
import { SystemAnnouncementCenter } from "@/components/layout/system-announcement-center";
import { useMembershipAction } from "@/hooks/use-membership-action";
import { useWalletBalance } from "@/hooks/use-wallet-balance";
import { applyUserSession } from "@/lib/user-session";
import { cn } from "@/lib/utils";
import { logout } from "@/services/api/auth";
import { useThemeStore } from "@/stores/use-theme-store";
import { useUserStore, type LocalUser } from "@/stores/use-user-store";

type WorkspaceSidebarFooterProps = {
    expandedClassName: string;
    collapsedClassName: string;
    accountClassName: string;
    compact?: boolean;
};

export function WorkspaceSidebarFooter({ expandedClassName, collapsedClassName, accountClassName, compact = false }: WorkspaceSidebarFooterProps) {
    const theme = useThemeStore((state) => state.theme);
    const setTheme = useThemeStore((state) => state.setTheme);
    const user = useUserStore((state) => state.user);
    const hydrated = useUserStore((state) => state.hydrated);
    const navigate = useNavigate();
    const { message } = App.useApp();
    const [menuOpen, setMenuOpen] = useState(false);
    const { availableMicrocredits } = useWalletBalance(user?.id, true);
    const membershipAction = useMembershipAction(user?.id);
    const balance = availableMicrocredits === null ? "--" : (availableMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 2 });

    const handleLogout = async () => {
        try {
            await logout();
            await applyUserSession({ user: null, systemChannels: [] });
            setMenuOpen(false);
            message.success("已退出登录");
            navigate("/login", { replace: true });
        } catch (error) {
            message.error(error instanceof Error ? error.message : "退出失败");
        }
    };

    if (!hydrated) return <div className={cn("animate-pulse rounded-md bg-foreground/[.035]", compact ? "h-8 w-24" : "h-24")} />;

    return (
        <div className={cn("workspace-account-actions", compact && "flex items-center gap-1")}>
            {user ? (
                <SystemAnnouncementCenter
                    userId={user.id}
                    className={cn("relative flex h-9 items-center rounded-md text-xs text-foreground/55 transition-colors hover:bg-foreground/[.05] hover:text-foreground", compact ? "w-9 shrink-0" : "mb-1 w-full", collapsedClassName)}
                    showLabel={!compact}
                    labelClassName={expandedClassName}
                    staticMotion
                />
            ) : null}
            {user ? (
                <div className={compact ? "workspace-top-bar-account-pill" : "workspace-sidebar-account-menu"}>
                    {compact ? (
                        <>
                            <Link to="/wallet" className="workspace-top-bar-balance" title={`${balance} 积分`} aria-label={`可用积分 ${balance}`}>
                                <Zap className="workspace-top-bar-balance-icon" aria-hidden="true" />
                                <span className="workspace-top-bar-balance-value">{balance}</span>
                            </Link>
                            <Link to="/membership" className="workspace-top-bar-membership" aria-label={membershipAction.label} title={membershipAction.title}>
                                <Gem className="workspace-top-bar-membership-icon" aria-hidden="true" />
                                <span className="workspace-top-bar-membership-label">{membershipAction.label}</span>
                            </Link>
                        </>
                    ) : null}
                    <Popover
                        trigger="click"
                        placement={compact ? "bottomRight" : "rightBottom"}
                        open={menuOpen}
                        onOpenChange={setMenuOpen}
                        content={
                            <div className="workspace-account-popover-content w-[232px] py-0.5">
                                <div className="workspace-account-popover-summary flex items-center gap-3 border-b border-border/65 px-1 pb-3">
                                    <UserAvatar user={user} className="size-8" />
                                    <div className="workspace-account-popover-copy min-w-0 flex-1">
                                        <div className="workspace-account-popover-name-row flex min-w-0 items-center gap-1.5">
                                            <span className="workspace-account-popover-name truncate text-sm font-medium">{user.displayName || user.username}</span>
                                            <IdentityProviderBadge user={user} />
                                        </div>
                                        <div className="workspace-account-popover-balance mt-0.5 truncate text-[11px] tabular-nums text-foreground/45">可用 {balance} 积分</div>
                                    </div>
                                </div>

                                <nav className="workspace-account-popover-nav py-2" aria-label="账号与工具">
                                    <MenuLink to="/wallet" icon={<Coins />} label="积分与账单" onNavigate={() => setMenuOpen(false)} />
                                    <MenuLink to="/teams" icon={<UsersRound />} label="团队空间" onNavigate={() => setMenuOpen(false)} />
                                    <MenuLink to="/skills" icon={<BookOpenCheck />} label="技能库" onNavigate={() => setMenuOpen(false)} />
                                    <MenuLink to="/settings" icon={<Settings2 />} label="设置" onNavigate={() => setMenuOpen(false)} />
                                    {user.role === "admin" ? <MenuLink to="/admin" icon={<ShieldCheck />} label="管理员后台" onNavigate={() => setMenuOpen(false)} /> : null}
                                </nav>

                                <div className="workspace-account-popover-theme flex h-10 items-center px-2">
                                    {theme === "dark" ? <Moon className="workspace-account-popover-theme-icon size-3.5 text-foreground/45" /> : <Sun className="workspace-account-popover-theme-icon size-3.5 text-foreground/45" />}
                                    <span className="workspace-account-popover-theme-label ml-2 flex-1 text-xs text-foreground/65">深色模式</span>
                                    <Switch className="workspace-account-popover-theme-switch" size="small" checked={theme === "dark"} onChange={(checked) => setTheme(checked ? "dark" : "light")} aria-label="深色模式" />
                                </div>
                                <button type="button" className="workspace-account-popover-logout flex h-9 w-full items-center gap-2 rounded px-2 text-xs text-foreground/55 hover:bg-red-500/[.08] hover:text-red-600" onClick={() => void handleLogout()}>
                                    <LogOut className="workspace-account-popover-logout-icon size-3.5" />
                                    <span className="workspace-account-popover-logout-label">退出登录</span>
                                </button>
                            </div>
                        }
                    >
                        <button
                            type="button"
                            className={cn("workspace-account-trigger flex min-h-10 w-full min-w-0 items-center overflow-hidden rounded-md text-left transition-colors hover:bg-foreground/[.045]", accountClassName)}
                            title={`${user.displayName || user.username} · ${balance} 积分`}
                        >
                            <UserAvatar user={user} className="size-7" />
                            <span className={cn("workspace-account-trigger-copy min-w-0 flex-1 flex-col", expandedClassName)}>
                                <span className="workspace-account-trigger-name truncate text-xs font-medium">{user.displayName || user.username}</span>
                                <span className="workspace-account-trigger-balance mt-0.5 block truncate text-[9px] tabular-nums text-foreground/42">{balance} 积分</span>
                            </span>
                            <ChevronRight className={cn("workspace-account-trigger-chevron size-3.5 shrink-0 text-foreground/30", expandedClassName)} />
                        </button>
                    </Popover>
                </div>
            ) : (
                <Link to="/login" className={cn("workspace-account-login flex h-10 items-center rounded-md text-xs text-foreground/65 hover:bg-foreground/[.05] hover:text-foreground", collapsedClassName)} title="登录">
                    <LogIn className="workspace-account-login-icon size-4 shrink-0" />
                    <span className={cn("workspace-account-login-label", expandedClassName)}>登录</span>
                </Link>
            )}
        </div>
    );
}

function MenuLink({ to, icon, label, onNavigate }: { to: string; icon: ReactNode; label: string; onNavigate: () => void }) {
    return (
        <Link to={to} onClick={onNavigate} className="flex h-9 items-center gap-2.5 rounded px-2 text-xs text-foreground/62 hover:bg-foreground/[.055] hover:text-foreground [&_svg]:size-3.5 [&_svg]:shrink-0">
            {icon}
            <span className="flex-1">{label}</span>
            <ChevronRight className="!size-3 text-foreground/25" />
        </Link>
    );
}

function UserAvatar({ user, className }: { user: LocalUser; className: string }) {
    const [failed, setFailed] = useState(false);
    const avatarUrl = /^https?:\/\//i.test(user.avatarUrl || "") ? user.avatarUrl : "";

    useEffect(() => setFailed(false), [avatarUrl]);

    return (
        <span className={`relative grid shrink-0 place-items-center ${className}`}>
            <span className="grid size-full place-items-center overflow-hidden rounded-full bg-muted text-foreground/55">
                {avatarUrl && !failed ? <img src={avatarUrl} alt="" referrerPolicy="no-referrer" className="size-full object-cover" onError={() => setFailed(true)} /> : <UserRound className="size-[52%]" aria-hidden />}
            </span>
            <IdentityProviderBadge user={user} compact className="absolute -bottom-1 -right-1" />
        </span>
    );
}
