import { Popover } from "antd";
import { ArrowLeftRight, Check, RefreshCw, UserRound, UsersRound } from "lucide-react";
import { useState, type JSX } from "react";
import { useNavigate } from "react-router";

import type { TeamWorkspace } from "@/services/api/teams";
import { personalWorkspaceScope, type WorkspaceScope } from "@/lib/workspace-scope";
import type { LocalUser } from "@/stores/use-user-store";

type TeamWorkspaceStatus = "idle" | "loading" | "ready" | "error";

type SiteAccountTeamSwitcherProps = {
    user: LocalUser;
    scope: WorkspaceScope;
    workspace?: TeamWorkspace;
    status: TeamWorkspaceStatus;
    error: string;
    reload: () => void;
    selectScope: (scope: WorkspaceScope) => void;
    closeAccountMenu: () => void;
};

export function SiteAccountTeamSwitcher({ user, scope, workspace, status, error, reload, selectScope, closeAccountMenu }: SiteAccountTeamSwitcherProps): JSX.Element {
    const navigate = useNavigate();
    const [open, setOpen] = useState(false);

    const switchTo = (nextScope: WorkspaceScope) => {
        selectScope(nextScope);
        setOpen(false);
        closeAccountMenu();
        navigate("/projects");
    };

    const content = (
        <div className="site-account-team-panel" aria-label="切换团队空间">
            <div className="site-account-team-section-label">个人空间</div>
            <button type="button" className={`site-account-team-option ${scope.kind === "personal" ? "site-account-team-option--active" : ""}`.trim()} aria-current={scope.kind === "personal" ? "page" : undefined} onClick={() => switchTo(personalWorkspaceScope)}>
                <span className="site-account-team-option-avatar site-account-team-option-avatar--personal">
                    <UserRound className="site-account-team-option-avatar-icon" aria-hidden />
                </span>
                <span className="site-account-team-option-name">{user.displayName || user.username}</span>
                {scope.kind === "personal" ? <Check className="site-account-team-option-check" aria-hidden /> : null}
            </button>

            <div className="site-account-team-section-label site-account-team-section-label--teams">团队</div>
            {status === "loading" ? <div className="site-account-team-state">正在读取团队…</div> : null}
            {status === "error" ? (
                <div className="site-account-team-state site-account-team-state--error" role="alert">
                    <span className="site-account-team-state-message">{error}</span>
                    <button type="button" className="site-account-team-retry" onClick={reload} aria-label="重新读取团队列表">
                        <RefreshCw className="site-account-team-retry-icon" aria-hidden />
                        <span className="site-account-team-retry-label">重试</span>
                    </button>
                </div>
            ) : null}
            {status === "ready" && !workspace?.teams.length ? <div className="site-account-team-state">暂无团队</div> : null}
            {status === "ready" && workspace?.teams.length ? (
                <div className="site-account-team-list">
                    {workspace.teams.map((summary) => {
                        const selected = scope.kind === "team" && summary.team.id === scope.teamId;
                        return (
                            <button
                                key={summary.team.id}
                                type="button"
                                className={`site-account-team-option ${selected ? "site-account-team-option--active" : ""}`.trim()}
                                aria-current={selected ? "page" : undefined}
                                onClick={() => switchTo({ kind: "team", teamId: summary.team.id })}
                            >
                                <span className="site-account-team-option-avatar">
                                    <UsersRound className="site-account-team-option-avatar-icon" aria-hidden />
                                </span>
                                <span className="site-account-team-option-name">{summary.team.name}</span>
                                {selected ? <Check className="site-account-team-option-check" aria-hidden /> : null}
                            </button>
                        );
                    })}
                </div>
            ) : null}
        </div>
    );

    return (
        <Popover rootClassName="site-account-team-popover" trigger="click" placement="leftTop" align={{ offset: [-245, -31] }} open={open} onOpenChange={setOpen} content={content}>
            <button type="button" className="site-account-switch-team" aria-label="切换团队">
                <ArrowLeftRight className="site-account-switch-team-icon" aria-hidden />
                <span className="site-account-switch-team-label">切换团队</span>
            </button>
        </Popover>
    );
}
