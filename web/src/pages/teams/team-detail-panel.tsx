import { Button, Select, Tag, Tooltip } from "antd";
import { Coins, Crown, DoorOpen, MailPlus, Pencil, ShieldCheck, Trash2, UserRound, UsersRound } from "lucide-react";

import type { TeamDetail, TeamMember, TeamRole } from "@/services/api/teams";
import { TeamCommercialPanel } from "./team-commercial-panel";

type TeamDetailPanelProps = {
    detail: TeamDetail;
    busyKey: string;
    onInvite: () => void;
    onRename: () => void;
    onPurchase: () => void;
    onLeave: () => void;
    onRoleChange: (member: TeamMember, role: Exclude<TeamRole, "owner">) => void;
    onCreditLimitChange: (member: TeamMember) => void;
    onRemove: (member: TeamMember) => void;
    onRegenerateInvitation: (invitationId: string) => void;
    onRevokeInvitation: (invitationId: string) => void;
    onTeamChanged: () => Promise<void>;
};

const roleLabels: Record<TeamRole, string> = {
    owner: "所有者",
    admin: "管理员",
    member: "成员",
};

const auditLabels: Record<string, string> = {
    "team.created": "创建了团队",
    "team.renamed": "修改了团队名称",
    "invitation.created": "发出了成员邀请",
    "invitation.regenerated": "重新生成了邀请链接",
    "invitation.accepted": "接受邀请并加入团队",
    "invitation.revoked": "撤销了成员邀请",
    "member.role_updated": "调整了成员角色",
    "member.policy_updated": "调整了成员权限与积分额度",
    "member.removed": "移除了团队成员",
    "member.left": "退出了团队",
};

export function TeamDetailPanel({ detail, busyKey, onInvite, onRename, onPurchase, onLeave, onRoleChange, onCreditLimitChange, onRemove, onRegenerateInvitation, onRevokeInvitation, onTeamChanged }: TeamDetailPanelProps) {
    const { summary } = detail;
    const { capabilities } = summary;
    const occupiedSeats = summary.seatUsed + summary.invitationSeatReserved;

    return (
        <section className="team-detail-panel min-w-0 flex-1">
            <header className="team-detail-header flex flex-col gap-4 border-b border-border/65 pb-5 sm:flex-row sm:items-start sm:justify-between">
                <div className="team-detail-title-group min-w-0">
                    <div className="team-detail-title-row flex min-w-0 items-center gap-2">
                        <h2 className="team-detail-title truncate text-xl font-semibold tracking-[-0.025em]">{summary.team.name}</h2>
                        <Tag className="team-detail-role-tag m-0 border-0 bg-foreground/[.055] text-foreground/60">{roleLabels[summary.currentRole]}</Tag>
                        {capabilities.canRenameTeam ? (
                            <Tooltip className="team-rename-tooltip" title="修改团队名称">
                                <Button className="team-rename-button" type="text" size="small" icon={<Pencil className="team-rename-icon size-3.5" />} onClick={onRename} aria-label="修改团队名称" />
                            </Tooltip>
                        ) : null}
                    </div>
                    <p className="team-detail-description mt-1 text-xs leading-5 text-foreground/48">成员、邀请和权限变更均保留审计记录。</p>
                </div>
                <div className="team-detail-actions flex shrink-0 flex-wrap gap-2">
                    {capabilities.canInviteMembers ? (
                        <Button className="team-invite-button" type="primary" icon={<MailPlus className="team-invite-icon size-4" />} disabled={!summary.subscription} onClick={onInvite}>
                            邀请成员
                        </Button>
                    ) : null}
                    {capabilities.canManageSubscription ? (
                        <Button className="team-plan-button" onClick={onPurchase}>
                            {summary.subscription ? "管理套餐" : "开通团队会员"}
                        </Button>
                    ) : null}
                    {capabilities.canLeaveTeam ? (
                        <Button className="team-leave-button" danger icon={<DoorOpen className="team-leave-icon size-4" />} onClick={onLeave}>
                            退出团队
                        </Button>
                    ) : null}
                </div>
            </header>

            <div className="team-metric-grid grid grid-cols-2 gap-x-5 gap-y-4 border-b border-border/65 py-5 lg:grid-cols-4">
                <TeamMetric label="已使用席位" value={`${summary.seatUsed}`} detail={summary.subscription ? `共 ${summary.subscription.seatLimit} 席` : "仅所有者"} />
                <TeamMetric label="待接受邀请" value={`${summary.invitationSeatReserved}`} detail={summary.invitationSeatReserved ? "已预留席位" : "暂无占用"} />
                <TeamMetric label="当前套餐" value={summary.subscription?.planName || "未开通"} detail={summary.subscription ? `${occupiedSeats}/${summary.subscription.seatLimit} 席已占用` : "暂不可邀请成员"} />
                <TeamMetric label="有效期" value={summary.subscription?.endsAt ? formatDate(summary.subscription.endsAt) : "--"} detail={summary.subscription ? "到期后停止新增成员" : "开通后生效"} />
            </div>

            {!summary.subscription ? (
                <div className="team-subscription-notice mt-5 flex flex-col gap-3 bg-foreground/[.035] px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                    <div className="team-subscription-notice-copy min-w-0">
                        <div className="team-subscription-notice-title text-sm font-medium">团队尚未开通有效套餐</div>
                        <div className="team-subscription-notice-description mt-1 text-xs text-foreground/48">开通后才能邀请成员；邀请会立即预留一个席位。</div>
                    </div>
                    {capabilities.canManageSubscription ? (
                        <Button className="team-subscription-notice-button" type="primary" onClick={onPurchase}>
                            选择团队套餐
                        </Button>
                    ) : null}
                </div>
            ) : null}

            <section className="team-members-section mt-7">
                <div className="team-section-heading flex items-end justify-between gap-3">
                    <div className="team-section-heading-copy">
                        <h3 className="team-section-title text-sm font-semibold">团队成员</h3>
                        <p className="team-section-description mt-1 text-xs text-foreground/45">{detail.members.length} 位有效成员</p>
                    </div>
                </div>
                <div className="team-member-list mt-3 divide-y divide-border/55 bg-foreground/[.02]">
                    {detail.members.map((member) => {
                        const isOwner = member.role === "owner";
                        const canRemove = capabilities.canRemoveMembers && member.canRemove;
                        return (
                            <article className="team-member-row flex min-h-16 items-center gap-3 px-3 py-2.5 sm:px-4" key={member.id}>
                                <span className="team-member-avatar grid size-9 shrink-0 place-items-center rounded-full bg-foreground/[.065] text-xs font-semibold text-foreground/65">
                                    {(member.displayName || member.username).slice(0, 1).toUpperCase()}
                                </span>
                                <div className="team-member-copy min-w-0 flex-1">
                                    <div className="team-member-name-row flex min-w-0 items-center gap-1.5">
                                        <span className="team-member-name truncate text-sm font-medium">{member.displayName || member.username}</span>
                                        {isOwner ? <Crown className="team-member-owner-icon size-3.5 shrink-0 text-amber-500" /> : null}
                                    </div>
                                    <div className="team-member-meta mt-0.5 text-[11px] text-foreground/42">
                                        @{member.username} · 本月已用 {formatCredits(member.monthlyUsedMicrocredits)}
                                        {member.monthlyCreditLimitMicrocredits > 0 ? ` / ${formatCredits(member.monthlyCreditLimitMicrocredits)}` : " / 不限额"}
                                    </div>
                                </div>
                                <div className="team-member-actions flex shrink-0 items-center gap-1.5">
                                    {capabilities.canManageMemberRoles && !isOwner ? (
                                        <Select
                                            className="team-member-role-select w-[92px]"
                                            size="small"
                                            value={member.role === "admin" ? "admin" : "member"}
                                            options={capabilities.inviteRoles.map((role) => ({ label: roleLabels[role], value: role }))}
                                            loading={busyKey === `role:${member.id}`}
                                            disabled={busyKey !== ""}
                                            onChange={(role: Exclude<TeamRole, "owner">) => onRoleChange(member, role)}
                                            aria-label={`调整 ${member.displayName || member.username} 的角色`}
                                        />
                                    ) : (
                                        <span className="team-member-role-label text-xs text-foreground/48">{roleLabels[member.role]}</span>
                                    )}
                                    {capabilities.canManageMemberCreditLimits && !isOwner ? (
                                        <Tooltip className="team-member-credit-tooltip" title="设置成员月度积分额度">
                                            <Button
                                                className="team-member-credit-button"
                                                type="text"
                                                size="small"
                                                icon={<Coins className="team-member-credit-icon size-3.5" />}
                                                disabled={busyKey !== ""}
                                                onClick={() => onCreditLimitChange(member)}
                                                aria-label={`设置 ${member.displayName || member.username} 的月度积分额度`}
                                            />
                                        </Tooltip>
                                    ) : null}
                                    {canRemove ? (
                                        <Tooltip className="team-member-remove-tooltip" title="移出团队">
                                            <Button
                                                className="team-member-remove-button"
                                                type="text"
                                                danger
                                                size="small"
                                                icon={<Trash2 className="team-member-remove-icon size-3.5" />}
                                                loading={busyKey === `remove:${member.id}`}
                                                disabled={busyKey !== "" && busyKey !== `remove:${member.id}`}
                                                onClick={() => onRemove(member)}
                                                aria-label={`移除 ${member.displayName || member.username}`}
                                            />
                                        </Tooltip>
                                    ) : null}
                                </div>
                            </article>
                        );
                    })}
                </div>
            </section>

            {capabilities.canInviteMembers ? (
                <section className="team-invitations-section mt-7">
                    <div className="team-section-heading">
                        <h3 className="team-section-title text-sm font-semibold">待接受邀请</h3>
                        <p className="team-section-description mt-1 text-xs text-foreground/45">邀请在 7 天后失效，并在有效期内占用席位。</p>
                    </div>
                    <div className="team-invitation-list mt-3 divide-y divide-border/55 bg-foreground/[.02]">
                        {detail.invitations.length ? (
                            detail.invitations.map((invitation) => (
                                <article className="team-invitation-row flex min-h-14 items-center gap-3 px-3 py-2.5 sm:px-4" key={invitation.id}>
                                    <MailPlus className="team-invitation-icon size-4 shrink-0 text-foreground/35" />
                                    <div className="team-invitation-copy min-w-0 flex-1">
                                        <div className="team-invitation-email truncate text-sm font-medium">{invitation.email}</div>
                                        <div className="team-invitation-meta mt-0.5 text-[11px] text-foreground/42">
                                            {roleLabels[invitation.role]} · {formatDateTime(invitation.expiresAt)} 失效
                                        </div>
                                    </div>
                                    <div className="team-invitation-actions flex items-center gap-1">
                                        <Button
                                            className="team-invitation-regenerate-button"
                                            type="text"
                                            size="small"
                                            loading={busyKey === `regenerate:${invitation.id}`}
                                            disabled={busyKey !== "" && busyKey !== `regenerate:${invitation.id}`}
                                            onClick={() => onRegenerateInvitation(invitation.id)}
                                        >
                                            重新生成
                                        </Button>
                                        <Button
                                            className="team-invitation-revoke-button"
                                            type="text"
                                            danger
                                            size="small"
                                            loading={busyKey === `revoke:${invitation.id}`}
                                            disabled={busyKey !== "" && busyKey !== `revoke:${invitation.id}`}
                                            onClick={() => onRevokeInvitation(invitation.id)}
                                        >
                                            撤销
                                        </Button>
                                    </div>
                                </article>
                            ))
                        ) : (
                            <div className="team-invitation-empty px-4 py-6 text-center text-xs text-foreground/38">暂无待接受邀请</div>
                        )}
                    </div>
                </section>
            ) : null}

            <TeamCommercialPanel detail={detail} onTeamChanged={onTeamChanged} />

            {capabilities.canViewAudit ? (
                <section className="team-audit-section mt-7 pb-8">
                    <div className="team-section-heading">
                        <h3 className="team-section-title text-sm font-semibold">最近动态</h3>
                        <p className="team-section-description mt-1 text-xs text-foreground/45">团队关键写操作的事实记录。</p>
                    </div>
                    <div className="team-audit-list mt-3 divide-y divide-border/55">
                        {detail.auditEvents.length ? (
                            detail.auditEvents.map((event) => (
                                <div className="team-audit-row flex items-start gap-3 py-3" key={event.id}>
                                    <span className="team-audit-icon grid size-7 shrink-0 place-items-center rounded-full bg-foreground/[.05]">
                                        <ShieldCheck className="team-audit-shield size-3.5 text-foreground/45" />
                                    </span>
                                    <div className="team-audit-copy min-w-0 flex-1">
                                        <div className="team-audit-message text-xs text-foreground/72">
                                            <span className="team-audit-actor font-medium text-foreground">{event.actorName}</span>
                                            <span className="team-audit-action ml-1">{auditLabels[event.action] || event.action}</span>
                                            {event.targetName ? <span className="team-audit-target"> · {event.targetName}</span> : null}
                                        </div>
                                        <div className="team-audit-time mt-1 text-[10px] text-foreground/36">{formatDateTime(event.createdAt)}</div>
                                    </div>
                                </div>
                            ))
                        ) : (
                            <div className="team-audit-empty py-6 text-center text-xs text-foreground/38">暂无团队动态</div>
                        )}
                    </div>
                </section>
            ) : null}
        </section>
    );
}

function formatCredits(value: number) {
    return `${(value / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 2 })} 积分`;
}

function TeamMetric({ label, value, detail }: { label: string; value: string; detail: string }) {
    return (
        <div className="team-metric min-w-0">
            <div className="team-metric-label flex items-center gap-1.5 text-[11px] text-foreground/42">
                <UsersRound className="team-metric-icon size-3.5" />
                {label}
            </div>
            <div className="team-metric-value mt-2 truncate text-lg font-semibold tabular-nums">{value}</div>
            <div className="team-metric-detail mt-0.5 truncate text-[10px] text-foreground/36">{detail}</div>
        </div>
    );
}

export function TeamPlaceholder() {
    return (
        <div className="team-detail-placeholder grid min-h-[420px] place-items-center">
            <div className="team-detail-placeholder-copy text-center">
                <UserRound className="team-detail-placeholder-icon mx-auto size-8 text-foreground/22" />
                <div className="team-detail-placeholder-title mt-3 text-sm font-medium">选择一个团队</div>
                <div className="team-detail-placeholder-description mt-1 text-xs text-foreground/42">在左侧选择团队后查看成员与权限。</div>
            </div>
        </div>
    );
}

function formatDate(value: string) {
    return new Date(value).toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}

function formatDateTime(value: string) {
    return new Date(value).toLocaleString("zh-CN", { hour12: false, month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
