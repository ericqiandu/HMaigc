import { Popover } from "antd";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, Coins, LogOut, Moon, Settings, ShieldCheck, Stamp, Sun, UserPlus, UserRound, UsersRound, Zap } from "lucide-react";
import { lazy, Suspense, useEffect, useState, type JSX, type ReactNode } from "react";
import { Link } from "react-router";

import { openReferralCenter, ReferralRewardCenter } from "@/components/account/referral-reward-center";
import { useConfirmLogout } from "@/components/auth/use-confirm-logout";
import { SystemAnnouncementCenter } from "@/components/layout/system-announcement-center";
import { useMembershipAction } from "@/hooks/use-membership-action";
import { useWalletBalance } from "@/hooks/use-wallet-balance";
import { getTeamWorkspace, type TeamWorkspace } from "@/services/api/teams";
import type { WorkspaceScope } from "@/lib/workspace-scope";
import { useThemeStore } from "@/stores/use-theme-store";
import { useUserStore, type LocalUser } from "@/stores/use-user-store";

import "./site-account-actions.css";

const AIWatermarkSettingsModal = lazy(() => import("@/components/account/ai-watermark-settings-modal").then((module) => ({ default: module.AIWatermarkSettingsModal })));
const SiteAccountTeamSwitcher = lazy(() => import("@/components/layout/site-account-team-switcher").then((module) => ({ default: module.SiteAccountTeamSwitcher })));

export function SiteAccountActions(): JSX.Element {
    const confirmLogout = useConfirmLogout();
    const hydrated = useUserStore((state) => state.hydrated);
    const user = useUserStore((state) => state.user);
    const workspaceScope = useUserStore((state) => state.workspaceScope);
    const selectWorkspaceScope = useUserStore((state) => state.selectWorkspaceScope);
    const theme = useThemeStore((state) => state.theme);
    const setTheme = useThemeStore((state) => state.setTheme);
    const [menuOpen, setMenuOpen] = useState(false);
    const [watermarkOpen, setWatermarkOpen] = useState(false);
    const teamWorkspaceQuery = useQuery({
        queryKey: ["team-workspace", user?.id],
        queryFn: getTeamWorkspace,
        enabled: Boolean(user) && (menuOpen || workspaceScope.kind === "team"),
        staleTime: 30_000,
    });
    const { availableMicrocredits } = useWalletBalance(user?.id, Boolean(user) && workspaceScope.kind === "personal");
    const membershipAction = useMembershipAction(user?.id);
    const activeTeam = workspaceScope.kind === "team" ? teamWorkspaceQuery.data?.teams.find((summary) => summary.team.id === workspaceScope.teamId) : undefined;
    const activeMicrocredits = workspaceScope.kind === "team" ? activeTeam?.availableMicrocredits ?? null : availableMicrocredits;
    const balance = activeMicrocredits === null ? "--" : (activeMicrocredits / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 1 });

    const handleInvite = () => {
        setMenuOpen(false);
        openReferralCenter();
    };

    if (!hydrated) return <div className="site-account-loading w-[236px] animate-pulse rounded-full" aria-label="正在读取账户信息" />;

    if (!user) {
        return (
            <div className="site-account-guest flex items-center gap-2">
                <Link to="/membership" className="site-account-upgrade flex items-center gap-1.5 rounded-full px-4 text-[13px] font-medium transition-colors" aria-label="升级会员">
                    <MembershipIcon className="site-account-upgrade-icon size-4" />
                    <span className="site-account-upgrade-label">升级会员</span>
                </Link>
                <Link to="/login" className="site-account-auth flex items-center rounded-full px-5 text-[13px] font-medium transition-colors">
                    注册 / 登录
                </Link>
            </div>
        );
    }

    return (
        <div className="site-account-actions flex items-center gap-2">
            <ReferralRewardCenter />
            {watermarkOpen ? (
                <Suspense fallback={null}>
                    <AIWatermarkSettingsModal open onClose={() => setWatermarkOpen(false)} />
                </Suspense>
            ) : null}
            <SystemAnnouncementCenter userId={user.id} className="site-account-notifications grid shrink-0 place-items-center rounded-full transition-colors" staticMotion />
            <div className="site-account-pill flex items-center rounded-full px-1.5 backdrop-blur-xl">
                <Link to={workspaceScope.kind === "team" ? `/teams?teamId=${encodeURIComponent(workspaceScope.teamId)}` : "/wallet"} className="site-account-balance flex h-full items-center gap-1.5 px-2.5 text-[13px] font-medium tabular-nums transition-opacity hover:opacity-70" title={`${workspaceScope.kind === "team" ? activeTeam?.team.name || "团队" : "个人"}可用积分：${balance}`}>
                    <Zap className="site-account-balance-icon size-4" aria-hidden />
                    <span className="site-account-balance-value">{balance}</span>
                </Link>
                <Link to="/membership" className="site-account-member flex h-full items-center gap-1.5 px-2.5 text-[13px] font-medium transition-opacity hover:opacity-70" aria-label={membershipAction.label} title={membershipAction.title}>
                    <MembershipIcon className="site-account-member-icon size-4" />
                    <span className="site-account-member-label hidden sm:inline">{membershipAction.label}</span>
                </Link>
                <Popover
                    rootClassName="site-account-popover"
                    trigger="click"
                    placement="bottomRight"
                    open={menuOpen}
                    onOpenChange={setMenuOpen}
                    content={
                        <AccountMenu
                            user={user}
                            workspaceScope={workspaceScope}
                            teamWorkspace={teamWorkspaceQuery.data}
                            teamWorkspaceStatus={teamWorkspaceQuery.isError ? "error" : teamWorkspaceQuery.isLoading || teamWorkspaceQuery.isFetching ? "loading" : teamWorkspaceQuery.data ? "ready" : "idle"}
                            teamWorkspaceError={teamWorkspaceQuery.error instanceof Error ? teamWorkspaceQuery.error.message : "读取团队列表失败"}
                            reloadTeamWorkspace={() => void teamWorkspaceQuery.refetch()}
                            selectWorkspaceScope={selectWorkspaceScope}
                            theme={theme}
                            setTheme={setTheme}
                            close={() => setMenuOpen(false)}
                            invite={() => void handleInvite()}
                            openWatermark={() => {
                                setMenuOpen(false);
                                setWatermarkOpen(true);
                            }}
                            logout={() => {
                                setMenuOpen(false);
                                confirmLogout();
                            }}
                        />
                    }
                >
                    <button type="button" className="site-account-trigger flex items-center gap-1 pl-1 pr-1.5 text-left transition-opacity hover:opacity-75" aria-label={`打开 ${user.displayName || user.username} 的账户菜单`}>
                        <SiteUserAvatar user={user} className="size-7" />
                        <ChevronDown className="site-account-chevron size-3.5 opacity-50" aria-hidden />
                    </button>
                </Popover>
            </div>
        </div>
    );
}

function AccountMenu({
    user,
    workspaceScope,
    teamWorkspace,
    teamWorkspaceStatus,
    teamWorkspaceError,
    reloadTeamWorkspace,
    selectWorkspaceScope,
    theme,
    setTheme,
    close,
    invite,
    openWatermark,
    logout: logOut,
}: {
    user: LocalUser;
    workspaceScope: WorkspaceScope;
    teamWorkspace?: TeamWorkspace;
    teamWorkspaceStatus: "idle" | "loading" | "ready" | "error";
    teamWorkspaceError: string;
    reloadTeamWorkspace: () => void;
    selectWorkspaceScope: (scope: WorkspaceScope) => void;
    theme: "light" | "dark";
    setTheme: (theme: "light" | "dark") => void;
    close: () => void;
    invite: () => void;
    openWatermark: () => void;
    logout: () => void;
}) {
    return (
        <div className="site-account-menu">
            <div className="site-account-summary flex items-center">
                <SiteUserAvatar user={user} className="size-9" />
                <div className="site-account-summary-copy min-w-0 flex-1">
                    <div className="site-account-display-name truncate">{user.displayName || user.username}</div>
                    <div className="site-account-username truncate" title={`用户 ID：${user.publicId}`}>
                        ID:{user.publicId}
                    </div>
                </div>
                <Suspense
                    fallback={
                        <span className="site-account-switch-team-loading" role="status">
                            正在读取团队…
                        </span>
                    }
                >
                    <SiteAccountTeamSwitcher
                        user={user}
                        scope={workspaceScope}
                        workspace={teamWorkspace}
                        status={teamWorkspaceStatus}
                        error={teamWorkspaceError}
                        reload={reloadTeamWorkspace}
                        selectScope={selectWorkspaceScope}
                        closeAccountMenu={close}
                    />
                </Suspense>
            </div>
            <div className="site-account-divider" aria-hidden="true" />
            <nav className="site-account-menu-nav py-1" aria-label="账户菜单">
                <AccountMenuLink className="site-account-menu-link--mobile-wallet" to="/wallet" icon={<Coins className="site-account-menu-icon" />} label="积分中心" onNavigate={close} />
                <AccountMenuLink to="/settings" icon={<Settings className="site-account-menu-icon" />} label="账户设置" onNavigate={close} />
                <AccountMenuLink to="/teams" icon={<UsersRound className="site-account-menu-icon" />} label="团队管理" onNavigate={close} />
                <AccountMenuButton icon={<UserPlus className="site-account-menu-icon" />} label="邀请好友" onClick={invite} />
                <AccountMenuButton icon={<Stamp className="site-account-menu-icon" />} label="AI 水印设置" onClick={openWatermark} />
                {user.role === "admin" ? <AccountMenuLink to="/admin" icon={<ShieldCheck className="site-account-menu-icon" />} label="管理后台" onNavigate={close} /> : null}
            </nav>
            <div className="site-account-theme flex items-center px-2">
                <span className="site-account-theme-label flex-1">深色模式</span>
                <div className="site-account-theme-options" role="group" aria-label="界面主题">
                    <button
                        type="button"
                        className={`site-account-theme-option ${theme === "light" ? "site-account-theme-option--active" : ""}`.trim()}
                        onClick={() => setTheme("light")}
                        aria-label="浅色模式"
                        aria-pressed={theme === "light"}
                        title="浅色模式"
                    >
                        <Sun className="site-account-theme-option-icon" aria-hidden />
                    </button>
                    <button
                        type="button"
                        className={`site-account-theme-option ${theme === "dark" ? "site-account-theme-option--active" : ""}`.trim()}
                        onClick={() => setTheme("dark")}
                        aria-label="深色模式"
                        aria-pressed={theme === "dark"}
                        title="深色模式"
                    >
                        <Moon className="site-account-theme-option-icon" aria-hidden />
                    </button>
                </div>
            </div>
            <button type="button" className="site-account-logout flex w-full items-center gap-2 px-2 transition-colors" onClick={logOut}>
                <LogOut className="site-account-logout-icon" aria-hidden />
                <span className="site-account-logout-label">退出登录</span>
            </button>
        </div>
    );
}

function AccountMenuLink({ to, icon, label, onNavigate, className = "" }: { to: string; icon: ReactNode; label: string; onNavigate: () => void; className?: string }) {
    return (
        <Link to={to} onClick={onNavigate} className={`site-account-menu-link flex items-center gap-2.5 px-2 transition-colors ${className}`.trim()}>
            {icon}
            <span className="site-account-menu-label flex-1">{label}</span>
        </Link>
    );
}

function AccountMenuButton({ icon, label, onClick }: { icon: ReactNode; label: string; onClick: () => void }) {
    return (
        <button type="button" onClick={onClick} className="site-account-menu-link flex w-full items-center gap-2.5 px-2 text-left transition-colors">
            {icon}
            <span className="site-account-menu-label flex-1">{label}</span>
        </button>
    );
}

function MembershipIcon({ className }: { className: string }) {
    return (
        <svg className={`site-membership-icon ${className}`} viewBox="0 0 1024 1024" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false">
            <path
                className="site-membership-icon-layer site-membership-icon-layer-1"
                d="M663.864 147.333H362.136c-34.421 0-67.432 13.998-91.773 38.916l-207.86 212.78c-19.338 19.798-19.338 51.893 0 71.691l404.102 413.673c25.623 26.229 67.165 26.229 92.788 0L963.495 470.72c19.338-19.798 19.338-51.893 0-71.691l-207.862-212.78c-24.337-24.918-57.348-38.916-91.769-38.916z"
                fill="#FFA820"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-2"
                d="M62.504 399.028c-19.338 19.798-19.338 51.893 0 71.691l8.667 8.873c2.085-38.628 9.355-75.876 21.127-111.063l-29.794 30.499zM963.496 399.028L861.313 294.427c34.885 61.489 54.811 132.578 54.811 208.323 0 5.627-0.113 11.228-0.33 16.802l47.704-48.833c19.337-19.798 19.337-51.893-0.002-71.691z"
                fill="#FEAC33"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-3"
                d="M160.006 299.218l-67.708 69.311c-11.772 35.187-19.041 72.435-21.127 111.063l41.377 42.357a368.9 368.9 0 0 1-2.075-39.073c0-66.966 18.046-129.715 49.533-183.658zM861.313 294.427L755.635 186.248c-24.339-24.917-57.35-38.916-91.771-38.916h-45.019C749 203.074 840.189 332.323 840.189 482.875c0 51.345-10.611 100.209-29.752 144.528l105.356-107.851c0.218-5.574 0.33-11.175 0.33-16.802 0-75.746-19.925-146.835-54.81-208.323zM427.792 844.659l38.814 39.733c25.623 26.229 67.165 26.229 92.788 0l68.278-69.895c-46.362 21.333-97.961 33.236-152.341 33.236a368.23 368.23 0 0 1-47.539-3.074z"
                fill="#FEB133"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-4"
                d="M618.845 147.333H362.136c-34.421 0-67.432 13.998-91.773 38.916l-110.358 112.97c-31.487 53.943-49.533 116.692-49.533 183.657 0 13.2 0.708 26.235 2.075 39.073l64.293 65.816c-16.982-38.122-26.436-80.336-26.436-124.763 0-169.51 137.415-306.925 306.925-306.925s306.925 137.415 306.925 306.925S626.839 769.926 457.33 769.926c-46.985 0-91.495-10.573-131.306-29.445L427.792 844.66a368.23 368.23 0 0 0 47.539 3.074c54.38 0 105.979-11.903 152.341-33.236l182.765-187.094c19.14-44.319 29.752-93.184 29.752-144.528 0-150.553-91.189-279.802-221.344-335.543z"
                fill="#FEB633"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-5"
                d="M764.254 463.001c0-169.51-137.415-306.925-306.925-306.925S150.405 293.491 150.405 463.001c0 44.427 9.453 86.642 26.436 124.763L326.024 740.48c39.81 18.872 84.321 29.445 131.306 29.445 169.509 0.001 306.924-137.414 306.924-306.924z m-573.917-19.875c0-137.514 111.477-248.991 248.991-248.991S688.32 305.612 688.32 443.126 576.842 692.118 439.328 692.118 190.337 580.641 190.337 443.126z"
                fill="#FFBC34"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-6"
                d="M688.32 443.126c0-137.514-111.477-248.991-248.991-248.991S190.337 305.612 190.337 443.126s111.477 248.991 248.991 248.991S688.32 580.641 688.32 443.126zM421.327 614.31c-105.518 0-191.058-85.54-191.058-191.058s85.54-191.058 191.058-191.058 191.058 85.54 191.058 191.058S526.846 614.31 421.327 614.31z"
                fill="#FFC134"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-7"
                d="M421.327 232.194c-105.518 0-191.058 85.54-191.058 191.058s85.54 191.058 191.058 191.058 191.058-85.54 191.058-191.058-85.539-191.058-191.058-191.058z m-18.001 304.308c-73.523 0-133.125-59.602-133.125-133.125s59.602-133.125 133.125-133.125 133.125 59.602 133.125 133.125-59.602 133.125-133.125 133.125z"
                fill="#FFC634"
            />
            <path className="site-membership-icon-layer site-membership-icon-layer-8" d="M403.326 403.378m-133.125 0a133.125 133.125 0 1 0 266.25 0 133.125 133.125 0 1 0-266.25 0Z" fill="#FFCB34" />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-9"
                d="M663.864 165.333c14.702 0 29.048 2.922 42.639 8.686 13.62 5.775 25.818 14.122 36.256 24.808L950.62 411.606c12.532 12.83 12.532 33.706 0.001 46.535L546.518 871.814c-8.977 9.189-20.881 14.25-33.518 14.25-12.638 0-24.542-5.061-33.518-14.25L75.38 458.141c-12.532-12.83-12.532-33.706-0.001-46.535l207.86-212.78c10.439-10.686 22.637-19.033 36.257-24.808 13.591-5.763 27.937-8.686 42.639-8.686h301.729m0-17.999H362.136c-34.421 0-67.432 13.998-91.772 38.915l-207.86 212.78c-19.338 19.798-19.338 51.893 0 71.691l404.102 413.673c12.811 13.115 29.603 19.672 46.394 19.672s33.583-6.557 46.394-19.672l404.102-413.673c19.338-19.798 19.338-51.893 0-71.691l-207.862-212.78c-24.338-24.917-57.349-38.915-91.77-38.915z"
                fill="#FFA820"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-10"
                d="M585.506 299.37H440.494c-16.543 0-32.407 6.686-44.106 18.584L296.49 419.583c-9.294 9.454-9.294 24.783 0 34.237l194.213 197.576a31.154 31.154 0 0 0 44.593 0L729.509 453.82c9.294-9.454 9.294-24.783 0-34.237l-99.896-101.629c-11.698-11.898-27.564-18.584-44.107-18.584z"
                fill="#FFE3B4"
            />
            <path
                className="site-membership-icon-layer site-membership-icon-layer-11"
                d="M222.012 346.805a17.94 17.94 0 0 1-12.677-5.222c-7.057-7.001-7.102-18.398-0.101-25.456l87.419-88.112c7.002-7.057 18.398-7.102 25.456-0.1 7.057 7.001 7.102 18.398 0.101 25.456l-87.419 88.112a17.945 17.945 0 0 1-12.779 5.322zM172.371 396.84a17.94 17.94 0 0 1-12.677-5.222c-7.058-7.001-7.103-18.398-0.101-25.456l7.428-7.487c7.002-7.058 18.399-7.103 25.456-0.101 7.058 7.001 7.103 18.398 0.101 25.456l-7.428 7.487a17.946 17.946 0 0 1-12.779 5.323z"
                fill="#FFFFFF"
            />
        </svg>
    );
}

function SiteUserAvatar({ user, className }: { user: LocalUser; className: string }) {
    const [failed, setFailed] = useState(false);
    const avatarUrl = /^https?:\/\//i.test(user.avatarUrl || "") ? user.avatarUrl : "";
    useEffect(() => setFailed(false), [avatarUrl]);
    return (
        <span className={`site-account-avatar grid shrink-0 place-items-center overflow-hidden rounded-full bg-foreground/[.08] text-foreground/55 ${className}`}>
            {avatarUrl && !failed ? (
                <img src={avatarUrl} alt="" referrerPolicy="no-referrer" className="site-account-avatar-image size-full object-cover" onError={() => setFailed(true)} />
            ) : (
                <UserRound className="site-account-avatar-fallback size-[52%]" aria-hidden />
            )}
        </span>
    );
}
