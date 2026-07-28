import { App, Popover, Switch } from "antd";
import { ChevronDown, Coins, Gem, LogOut, Settings2, ShieldCheck, UserRound, Zap } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useNavigate } from "react-router";

import { SystemAnnouncementCenter } from "@/components/layout/system-announcement-center";
import { useWalletBalance } from "@/hooks/use-wallet-balance";
import { applyUserSession } from "@/lib/user-session";
import { logout } from "@/services/api/auth";
import { useThemeStore } from "@/stores/use-theme-store";
import { useUserStore, type LocalUser } from "@/stores/use-user-store";

export function UpdreamAccountActions() {
    const { message } = App.useApp();
    const navigate = useNavigate();
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);
    const theme = useThemeStore((state) => state.theme);
    const setTheme = useThemeStore((state) => state.setTheme);
    const [menuOpen, setMenuOpen] = useState(false);
    const { availableMicrocredits } = useWalletBalance(user?.id, Boolean(user));
    const balance = availableMicrocredits === null ? "--" : (availableMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 1 });

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

    if (!hydrated) return <div className="updream-account-loading h-10 w-[236px] animate-pulse rounded-full bg-foreground/[.06]" aria-label="正在读取账户信息" />;

    if (!user) {
        return (
            <div className="updream-account-guest flex items-center gap-2">
                <Link to="/membership" className="updream-account-upgrade flex h-10 items-center gap-1.5 rounded-full bg-foreground/[.07] px-4 text-[13px] font-medium text-foreground transition-colors hover:bg-foreground/[.12]">
                    <Gem className="updream-account-upgrade-icon size-4 text-violet-500" aria-hidden />
                    <span className="updream-account-upgrade-label">升级会员</span>
                </Link>
                <Link to="/login" className="updream-header-auth flex h-10 items-center rounded-full px-5 text-[13px] font-medium transition-colors">注册 / 登录</Link>
            </div>
        );
    }

    return (
        <div className="updream-account-actions flex items-center gap-2">
            <SystemAnnouncementCenter userId={user.id} className="updream-account-notifications grid size-9 shrink-0 place-items-center rounded-full bg-foreground/[.07] text-foreground/65 transition-colors hover:bg-foreground/[.12] hover:text-foreground" staticMotion />
            <div className="updream-account-pill flex h-10 items-center rounded-full bg-[#eef0f4]/90 px-1.5 text-[#172033] shadow-[inset_0_0_0_1px_rgba(23,32,51,0.06)] backdrop-blur-xl dark:bg-[#202124]/90 dark:text-white dark:shadow-[inset_0_0_0_1px_rgba(255,255,255,0.08)]">
                <Link to="/wallet" className="updream-account-balance flex h-full items-center gap-1.5 px-2.5 text-[13px] font-medium tabular-nums transition-opacity hover:opacity-70" title={`${balance} 积分`}>
                    <Zap className="updream-account-balance-icon size-4 fill-violet-500 text-violet-500" aria-hidden />
                    <span className="updream-account-balance-value">{balance}</span>
                </Link>
                <Link to="/membership" className="updream-account-member flex h-full items-center gap-1.5 px-2.5 text-[13px] font-medium transition-opacity hover:opacity-70">
                    <Gem className="updream-account-member-icon size-4 text-violet-500" aria-hidden />
                    <span className="updream-account-member-label hidden sm:inline">升级会员</span>
                </Link>
                <Popover className="updream-account-popover" trigger="click" placement="bottomRight" open={menuOpen} onOpenChange={setMenuOpen} content={<AccountMenu user={user} balance={balance} theme={theme} setTheme={setTheme} close={() => setMenuOpen(false)} logout={() => void handleLogout()} />}>
                    <button type="button" className="updream-account-trigger flex h-8 items-center gap-1 pl-1 pr-1.5 text-left transition-opacity hover:opacity-75" aria-label={`打开 ${user.displayName || user.username} 的账户菜单`}>
                        <UpdreamUserAvatar user={user} className="size-7" />
                        <ChevronDown className="updream-account-chevron size-3.5 opacity-50" aria-hidden />
                    </button>
                </Popover>
            </div>
        </div>
    );
}

function AccountMenu({ user, balance, theme, setTheme, close, logout: logOut }: { user: LocalUser; balance: string; theme: "light" | "dark"; setTheme: (theme: "light" | "dark") => void; close: () => void; logout: () => void }) {
    return (
        <div className="updream-account-menu w-[244px] py-1">
            <div className="updream-account-summary flex items-center gap-3 px-1 pb-3">
                <UpdreamUserAvatar user={user} className="size-9" />
                <div className="updream-account-summary-copy min-w-0 flex-1">
                    <div className="updream-account-display-name truncate text-sm font-semibold">{user.displayName || user.username}</div>
                    <div className="updream-account-username mt-0.5 truncate text-[11px] text-foreground/45">@{user.username}</div>
                </div>
            </div>
            <div className="updream-account-balance-row mb-2 flex items-center justify-between bg-foreground/[.045] px-3 py-2.5">
                <span className="updream-account-balance-label text-xs text-foreground/55">可用创作积分</span>
                <span className="updream-account-balance-number text-xs font-semibold tabular-nums">{balance}</span>
            </div>
            <nav className="updream-account-menu-nav py-1" aria-label="账户菜单">
                <AccountMenuLink to="/wallet" icon={<Coins className="updream-account-menu-icon size-4" />} label="积分中心" onNavigate={close} />
                <AccountMenuLink to="/membership" icon={<Gem className="updream-account-menu-icon size-4" />} label="升级会员" onNavigate={close} />
                <AccountMenuLink to="/settings" icon={<Settings2 className="updream-account-menu-icon size-4" />} label="账户设置" onNavigate={close} />
                {user.role === "admin" ? <AccountMenuLink to="/admin" icon={<ShieldCheck className="updream-account-menu-icon size-4" />} label="管理后台" onNavigate={close} /> : null}
            </nav>
            <div className="updream-account-theme flex h-10 items-center px-2">
                <span className="updream-account-theme-label flex-1 text-xs text-foreground/65">深色模式</span>
                <Switch className="updream-account-theme-switch" size="small" checked={theme === "dark"} onChange={(checked) => setTheme(checked ? "dark" : "light")} aria-label="深色模式" />
            </div>
            <button type="button" className="updream-account-logout flex h-9 w-full items-center gap-2 px-2 text-xs text-foreground/55 transition-colors hover:bg-red-500/[.08] hover:text-red-600" onClick={logOut}>
                <LogOut className="updream-account-logout-icon size-4" aria-hidden />
                <span className="updream-account-logout-label">退出登录</span>
            </button>
        </div>
    );
}

function AccountMenuLink({ to, icon, label, onNavigate }: { to: string; icon: ReactNode; label: string; onNavigate: () => void }) {
    return <Link to={to} onClick={onNavigate} className="updream-account-menu-link flex h-9 items-center gap-2.5 px-2 text-xs text-foreground/62 transition-colors hover:bg-foreground/[.055] hover:text-foreground">{icon}<span className="updream-account-menu-label flex-1">{label}</span></Link>;
}

function UpdreamUserAvatar({ user, className }: { user: LocalUser; className: string }) {
    const [failed, setFailed] = useState(false);
    const avatarUrl = /^https?:\/\//i.test(user.avatarUrl || "") ? user.avatarUrl : "";
    useEffect(() => setFailed(false), [avatarUrl]);
    return (
        <span className={`updream-account-avatar grid shrink-0 place-items-center overflow-hidden rounded-full bg-foreground/[.08] text-foreground/55 ${className}`}>
            {avatarUrl && !failed ? <img src={avatarUrl} alt="" referrerPolicy="no-referrer" className="updream-account-avatar-image size-full object-cover" onError={() => setFailed(true)} /> : <UserRound className="updream-account-avatar-fallback size-[52%]" aria-hidden />}
        </span>
    );
}
