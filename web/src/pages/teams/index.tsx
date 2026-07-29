import { App, Button, Input, InputNumber, Modal, Select, Skeleton } from "antd";
import { Check, ChevronRight, Copy, Mail, Plus, RefreshCw, UsersRound } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { PageHeader, WorkspacePage } from "@/components/layout/workspace-page";
import {
    acceptTeamInvitationById,
    acceptTeamInvitationByToken,
    createTeam,
    createTeamInvitation,
    getTeamDetail,
    getTeamWorkspace,
    leaveTeam,
    removeTeamMember,
    renameTeam,
    revokeTeamInvitation,
    updateTeamMemberPolicy,
    type TeamDetail,
    type TeamMember,
    type TeamRole,
    type TeamWorkspace,
} from "@/services/api/teams";
import { TeamDetailPanel, TeamPlaceholder } from "./team-detail-panel";

export default function TeamsPage() {
    const { message, modal } = App.useApp();
    const navigate = useNavigate();
    const [searchParams, setSearchParams] = useSearchParams();
    const [workspace, setWorkspace] = useState<TeamWorkspace | null>(null);
    const [activeTeamId, setActiveTeamId] = useState("");
    const [detail, setDetail] = useState<TeamDetail | null>(null);
    const [loading, setLoading] = useState(true);
    const [detailLoading, setDetailLoading] = useState(false);
    const [busyKey, setBusyKey] = useState("");
    const [createOpen, setCreateOpen] = useState(false);
    const [createName, setCreateName] = useState("");
    const [renameOpen, setRenameOpen] = useState(false);
    const [renameName, setRenameName] = useState("");
    const [inviteOpen, setInviteOpen] = useState(false);
    const [inviteEmail, setInviteEmail] = useState("");
    const [inviteRole, setInviteRole] = useState<Exclude<TeamRole, "owner">>("member");
    const [inviteLink, setInviteLink] = useState("");
    const [copied, setCopied] = useState(false);
    const [creditLimitMember, setCreditLimitMember] = useState<TeamMember | null>(null);
    const [creditLimit, setCreditLimit] = useState<number>(0);
    const workspaceRequest = useRef(0);
    const detailRequest = useRef(0);
    const inviteTokenHandled = useRef(false);

    const loadWorkspace = useCallback(
        async (preferredTeamId?: string) => {
            const sequence = ++workspaceRequest.current;
            setLoading(true);
            try {
                const next = await getTeamWorkspace();
                if (sequence !== workspaceRequest.current) return;
                setWorkspace(next);
                setActiveTeamId((current) => {
                    const candidate = preferredTeamId || current;
                    if (candidate && next.teams.some((item) => item.team.id === candidate)) return candidate;
                    return next.teams[0]?.team.id || "";
                });
            } catch (error) {
                if (sequence === workspaceRequest.current) message.error(error instanceof Error ? error.message : "读取团队空间失败");
            } finally {
                if (sequence === workspaceRequest.current) setLoading(false);
            }
        },
        [message],
    );

    const loadDetail = useCallback(
        async (teamId: string) => {
            if (!teamId) {
                setDetail(null);
                return;
            }
            const sequence = ++detailRequest.current;
            setDetailLoading(true);
            try {
                const next = await getTeamDetail(teamId);
                if (sequence === detailRequest.current) setDetail(next);
            } catch (error) {
                if (sequence === detailRequest.current) {
                    setDetail(null);
                    message.error(error instanceof Error ? error.message : "读取团队详情失败");
                }
            } finally {
                if (sequence === detailRequest.current) setDetailLoading(false);
            }
        },
        [message],
    );

    const reloadActiveTeam = useCallback(async () => {
        await Promise.all([loadWorkspace(activeTeamId), loadDetail(activeTeamId)]);
    }, [activeTeamId, loadDetail, loadWorkspace]);

    useEffect(() => {
        void loadWorkspace();
    }, [loadWorkspace]);

    useEffect(() => {
        void loadDetail(activeTeamId);
    }, [activeTeamId, loadDetail]);

    useEffect(() => {
        const token = searchParams.get("invite");
        if (!token || inviteTokenHandled.current) return;
        inviteTokenHandled.current = true;
        setBusyKey("accept:token");
        void acceptTeamInvitationByToken(token)
            .then(async () => {
                const next = new URLSearchParams(searchParams);
                next.delete("invite");
                setSearchParams(next, { replace: true });
                message.success("已加入团队");
                await loadWorkspace();
            })
            .catch((error: unknown) => {
                message.error(error instanceof Error ? error.message : "接受团队邀请失败");
            })
            .finally(() => setBusyKey(""));
    }, [loadWorkspace, message, searchParams, setSearchParams]);

    const submitCreate = async () => {
        const name = createName.trim();
        if (!name) {
            message.error("请输入团队名称");
            return;
        }
        setBusyKey("create");
        try {
            const team = await createTeam(name);
            setCreateOpen(false);
            setCreateName("");
            await loadWorkspace(team.id);
            message.success("团队已创建");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建团队失败");
        } finally {
            setBusyKey("");
        }
    };

    const submitRename = async () => {
        const name = renameName.trim();
        if (!activeTeamId || !name) {
            message.error("请输入团队名称");
            return;
        }
        setBusyKey("rename");
        try {
            await renameTeam(activeTeamId, name);
            setRenameOpen(false);
            await reloadActiveTeam();
            message.success("团队名称已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "修改团队名称失败");
        } finally {
            setBusyKey("");
        }
    };

    const submitInvitation = async () => {
        const email = inviteEmail.trim();
        if (!activeTeamId || !email) {
            message.error("请输入成员邮箱");
            return;
        }
        setBusyKey("invite");
        try {
            const result = await createTeamInvitation(activeTeamId, { email, role: inviteRole });
            const link = `${window.location.origin}/teams?invite=${encodeURIComponent(result.acceptToken)}`;
            setInviteLink(link);
            setCopied(false);
            await reloadActiveTeam();
            message.success("邀请已创建并预留席位");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建邀请失败");
        } finally {
            setBusyKey("");
        }
    };

    const copyInviteLink = async () => {
        if (!inviteLink || !navigator.clipboard) {
            message.error("当前浏览器不支持复制，请手动复制邀请链接");
            return;
        }
        try {
            await navigator.clipboard.writeText(inviteLink);
            setCopied(true);
            message.success("邀请链接已复制");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "复制邀请链接失败");
        }
    };

    const acceptInvitation = async (invitationId: string) => {
        setBusyKey(`accept:${invitationId}`);
        try {
            await acceptTeamInvitationById(invitationId);
            await loadWorkspace();
            message.success("已加入团队");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "接受邀请失败");
        } finally {
            setBusyKey("");
        }
    };

    const changeRole = async (member: TeamMember, role: Exclude<TeamRole, "owner">) => {
        if (!activeTeamId) return;
        setBusyKey(`role:${member.id}`);
        try {
            await updateTeamMemberPolicy(activeTeamId, member.id, { role });
            await reloadActiveTeam();
            message.success("成员角色已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新成员角色失败");
        } finally {
            setBusyKey("");
        }
    };

    const saveCreditLimit = async () => {
        if (!activeTeamId || !creditLimitMember) return;
        if (!Number.isFinite(creditLimit) || creditLimit < 0) {
            message.error("成员月度积分额度不能小于 0");
            return;
        }
        setBusyKey(`credit-limit:${creditLimitMember.id}`);
        try {
            await updateTeamMemberPolicy(activeTeamId, creditLimitMember.id, {
                role: creditLimitMember.role === "admin" ? "admin" : "member",
                monthlyCreditLimitMicrocredits: Math.round(creditLimit * 1_000_000),
            });
            setCreditLimitMember(null);
            await reloadActiveTeam();
            message.success("成员月度积分额度已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新成员积分额度失败");
        } finally {
            setBusyKey("");
        }
    };

    const confirmRemove = (member: TeamMember) => {
        modal.confirm({
            title: `移除 ${member.displayName || member.username}？`,
            content: "移除后该成员将立即失去团队会员权益，历史审计记录仍会保留。",
            okText: "确认移除",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                setBusyKey(`remove:${member.id}`);
                try {
                    await removeTeamMember(activeTeamId, member.id);
                    await reloadActiveTeam();
                    message.success("成员已移出团队");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "移除成员失败");
                    throw error;
                } finally {
                    setBusyKey("");
                }
            },
        });
    };

    const confirmRevoke = (invitationId: string) => {
        modal.confirm({
            title: "撤销这个邀请？",
            content: "邀请链接将立即失效，已预留的席位会被释放。",
            okText: "撤销邀请",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                setBusyKey(`revoke:${invitationId}`);
                try {
                    await revokeTeamInvitation(activeTeamId, invitationId);
                    await reloadActiveTeam();
                    message.success("邀请已撤销");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "撤销邀请失败");
                    throw error;
                } finally {
                    setBusyKey("");
                }
            },
        });
    };

    const confirmLeave = () => {
        modal.confirm({
            title: "退出当前团队？",
            content: "退出后将立即失去团队会员权益，需要新的邀请才能再次加入。",
            okText: "确认退出",
            okButtonProps: { danger: true },
            cancelText: "取消",
            onOk: async () => {
                setBusyKey("leave");
                try {
                    await leaveTeam(activeTeamId);
                    setDetail(null);
                    await loadWorkspace();
                    message.success("已退出团队");
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "退出团队失败");
                    throw error;
                } finally {
                    setBusyKey("");
                }
            },
        });
    };

    const activeSummary = workspace?.teams.find((item) => item.team.id === activeTeamId);

    return (
        <WorkspacePage className="teams-workspace-page">
            <PageHeader
                title="团队空间"
                description="管理团队成员、邀请和角色权限"
                actions={
                    <div className="teams-header-actions flex items-center gap-2">
                        <Button className="teams-refresh-button" icon={<RefreshCw className="teams-refresh-icon size-4" />} loading={loading} onClick={() => void reloadActiveTeam()}>
                            刷新
                        </Button>
                        <Button className="teams-create-button" icon={<Plus className="teams-create-icon size-4" />} onClick={() => setCreateOpen(true)}>
                            新建团队
                        </Button>
                    </div>
                }
            />

            {workspace?.incomingInvitations.length ? (
                <section className="incoming-team-invitations mt-5 bg-[var(--workspace-accent)]/[.07] px-4 py-3">
                    <div className="incoming-team-invitations-heading flex items-center gap-2">
                        <Mail className="incoming-team-invitations-icon size-4 text-[var(--workspace-accent)]" />
                        <h2 className="incoming-team-invitations-title text-sm font-semibold">待处理的团队邀请</h2>
                    </div>
                    <div className="incoming-team-invitation-list mt-2 divide-y divide-border/55">
                        {workspace.incomingInvitations.map((invitation) => (
                            <article className="incoming-team-invitation-row flex flex-col gap-2 py-2.5 sm:flex-row sm:items-center" key={invitation.id}>
                                <div className="incoming-team-invitation-copy min-w-0 flex-1">
                                    <div className="incoming-team-invitation-name truncate text-sm font-medium">{invitation.teamName}</div>
                                    <div className="incoming-team-invitation-meta mt-0.5 text-[11px] text-foreground/48">
                                        {invitation.inviterName} 邀请你成为{invitation.role === "admin" ? "管理员" : "成员"} · {formatDateTime(invitation.expiresAt)} 失效
                                    </div>
                                </div>
                                <Button
                                    className="incoming-team-invitation-accept"
                                    type="primary"
                                    size="small"
                                    loading={busyKey === `accept:${invitation.id}`}
                                    disabled={busyKey !== "" && busyKey !== `accept:${invitation.id}`}
                                    onClick={() => void acceptInvitation(invitation.id)}
                                >
                                    接受邀请
                                </Button>
                            </article>
                        ))}
                    </div>
                </section>
            ) : null}

            <div className="teams-page-body mt-5 flex min-h-[560px] flex-col bg-foreground/[.018] md:flex-row">
                <aside className="teams-navigation shrink-0 border-b border-border/65 p-3 md:w-[228px] md:border-b-0 md:border-r" aria-label="团队列表">
                    <div className="teams-navigation-heading flex items-center justify-between px-2 py-1.5">
                        <span className="teams-navigation-label text-[11px] font-medium text-foreground/42">我的团队</span>
                        <span className="teams-navigation-count text-[10px] tabular-nums text-foreground/32">{workspace?.teams.length || 0}</span>
                    </div>
                    <nav className="teams-navigation-list mt-1 flex gap-1 overflow-x-auto md:block md:space-y-1">
                        {workspace?.teams.map((item) => {
                            const selected = item.team.id === activeTeamId;
                            return (
                                <button
                                    className={`team-navigation-item flex min-w-[190px] items-center gap-2.5 px-2.5 py-2.5 text-left transition-colors md:w-full md:min-w-0 ${selected ? "bg-foreground text-background" : "text-foreground/68 hover:bg-foreground/[.05] hover:text-foreground"}`}
                                    type="button"
                                    key={item.team.id}
                                    aria-current={selected ? "page" : undefined}
                                    onClick={() => setActiveTeamId(item.team.id)}
                                >
                                    <span className={`team-navigation-icon grid size-8 shrink-0 place-items-center rounded-md ${selected ? "bg-background/12" : "bg-foreground/[.055]"}`}>
                                        <UsersRound className="team-navigation-users-icon size-4" />
                                    </span>
                                    <span className="team-navigation-copy min-w-0 flex-1">
                                        <span className="team-navigation-name block truncate text-xs font-medium">{item.team.name}</span>
                                        <span className={`team-navigation-meta mt-0.5 block truncate text-[10px] ${selected ? "text-background/58" : "text-foreground/38"}`}>
                                            {item.seatUsed} 位成员 · {roleLabel(item.currentRole)}
                                        </span>
                                    </span>
                                    <ChevronRight className={`team-navigation-chevron size-3.5 shrink-0 ${selected ? "text-background/45" : "text-foreground/24"}`} />
                                </button>
                            );
                        })}
                    </nav>
                    {!loading && !workspace?.teams.length ? (
                        <div className="teams-navigation-empty px-2 py-8 text-center">
                            <UsersRound className="teams-navigation-empty-icon mx-auto size-7 text-foreground/20" />
                            <div className="teams-navigation-empty-title mt-2 text-xs font-medium">还没有团队</div>
                            <button className="teams-navigation-empty-action mt-2 text-xs text-[var(--workspace-accent)]" type="button" onClick={() => setCreateOpen(true)}>
                                创建第一个团队
                            </button>
                        </div>
                    ) : null}
                </aside>

                <main className="teams-detail-content min-w-0 flex-1 px-4 py-5 sm:px-6">
                    {detailLoading && !detail ? (
                        <Skeleton className="teams-detail-skeleton" active paragraph={{ rows: 8 }} />
                    ) : detail ? (
                        <TeamDetailPanel
                            detail={detail}
                            busyKey={busyKey}
                            onInvite={() => {
                                setInviteEmail("");
                                setInviteRole("member");
                                setInviteLink("");
                                setCopied(false);
                                setInviteOpen(true);
                            }}
                            onRename={() => {
                                setRenameName(detail.summary.team.name);
                                setRenameOpen(true);
                            }}
                            onPurchase={() => navigate(`/membership?audience=team&teamId=${encodeURIComponent(activeTeamId)}`)}
                            onLeave={confirmLeave}
                            onRoleChange={(member, role) => void changeRole(member, role)}
                            onCreditLimitChange={(member) => {
                                setCreditLimitMember(member);
                                setCreditLimit(member.monthlyCreditLimitMicrocredits / 1_000_000);
                            }}
                            onRemove={confirmRemove}
                            onRevokeInvitation={confirmRevoke}
                            onTeamChanged={reloadActiveTeam}
                        />
                    ) : (
                        <TeamPlaceholder />
                    )}
                </main>
            </div>

            <Modal className="team-create-modal" title="新建团队" open={createOpen} okText="创建团队" cancelText="取消" confirmLoading={busyKey === "create"} onOk={() => void submitCreate()} onCancel={() => setCreateOpen(false)} destroyOnHidden>
                <label className="team-create-field block pt-2">
                    <span className="team-create-label text-xs font-medium text-foreground/68">团队名称</span>
                    <Input className="team-create-input mt-2" value={createName} maxLength={80} placeholder="例如：弘梦创作团队" autoFocus onChange={(event) => setCreateName(event.target.value)} onPressEnter={() => void submitCreate()} />
                </label>
            </Modal>

            <Modal className="team-rename-modal" title="修改团队名称" open={renameOpen} okText="保存" cancelText="取消" confirmLoading={busyKey === "rename"} onOk={() => void submitRename()} onCancel={() => setRenameOpen(false)} destroyOnHidden>
                <label className="team-rename-field block pt-2">
                    <span className="team-rename-label text-xs font-medium text-foreground/68">团队名称</span>
                    <Input className="team-rename-input mt-2" value={renameName} maxLength={80} autoFocus onChange={(event) => setRenameName(event.target.value)} onPressEnter={() => void submitRename()} />
                </label>
            </Modal>

            <Modal
                className="team-credit-limit-modal"
                title={`设置 ${creditLimitMember?.displayName || creditLimitMember?.username || "成员"} 的月度积分额度`}
                open={Boolean(creditLimitMember)}
                okText="保存额度"
                cancelText="取消"
                confirmLoading={busyKey === `credit-limit:${creditLimitMember?.id || ""}`}
                onOk={() => void saveCreditLimit()}
                onCancel={() => setCreditLimitMember(null)}
                destroyOnHidden
            >
                <div className="team-credit-limit-content pt-2">
                    <label className="team-credit-limit-field block">
                        <span className="team-credit-limit-label text-xs font-medium text-foreground/68">每自然月最多可消耗积分</span>
                        <InputNumber className="team-credit-limit-input mt-2 w-full" min={0} precision={2} value={creditLimit} addonAfter="积分" onChange={(value) => setCreditLimit(value ?? 0)} />
                    </label>
                    <p className="team-credit-limit-help mt-2 text-xs leading-5 text-foreground/42">填写 0 表示不设置成员级上限；实际调用仍受团队积分余额与渠道安全并发限制。</p>
                </div>
            </Modal>

            <Modal
                className="team-invite-modal"
                title="邀请团队成员"
                open={inviteOpen}
                okText={inviteLink ? "完成" : "创建邀请"}
                cancelText="取消"
                confirmLoading={busyKey === "invite"}
                onOk={() => (inviteLink ? setInviteOpen(false) : void submitInvitation())}
                onCancel={() => setInviteOpen(false)}
                destroyOnHidden
            >
                {inviteLink ? (
                    <div className="team-invite-result pt-2">
                        <div className="team-invite-result-heading flex items-center gap-2 text-sm font-medium">
                            <Check className="team-invite-result-icon size-4 text-emerald-500" />
                            邀请已创建
                        </div>
                        <p className="team-invite-result-description mt-2 text-xs leading-5 text-foreground/48">邀请链接只在这里展示一次。对方需要使用受邀邮箱登录，并在 7 天内接受。</p>
                        <div className="team-invite-link-row mt-4 flex items-center gap-2">
                            <Input className="team-invite-link-input min-w-0 flex-1" value={inviteLink} readOnly />
                            <Button className="team-invite-copy-button shrink-0" icon={copied ? <Check className="team-invite-copy-icon size-4" /> : <Copy className="team-invite-copy-icon size-4" />} onClick={() => void copyInviteLink()}>
                                {copied ? "已复制" : "复制"}
                            </Button>
                        </div>
                    </div>
                ) : (
                    <div className="team-invite-fields space-y-4 pt-2">
                        <label className="team-invite-email-field block">
                            <span className="team-invite-email-label text-xs font-medium text-foreground/68">成员邮箱</span>
                            <Input className="team-invite-email-input mt-2" type="email" value={inviteEmail} placeholder="member@example.com" autoFocus onChange={(event) => setInviteEmail(event.target.value)} />
                        </label>
                        <label className="team-invite-role-field block">
                            <span className="team-invite-role-label text-xs font-medium text-foreground/68">团队角色</span>
                            <Select
                                className="team-invite-role-select mt-2 w-full"
                                value={inviteRole}
                                options={[{ label: "成员 · 使用团队会员权益", value: "member" }, ...(activeSummary?.currentRole === "owner" ? [{ label: "管理员 · 可邀请和移除普通成员", value: "admin" as const }] : [])]}
                                onChange={(role: Exclude<TeamRole, "owner">) => setInviteRole(role)}
                            />
                        </label>
                        <p className="team-invite-seat-note text-xs leading-5 text-foreground/42">创建邀请后会立即预留 1 个席位；撤销或过期后自动释放。</p>
                    </div>
                )}
            </Modal>
        </WorkspacePage>
    );
}

function roleLabel(role: TeamRole) {
    if (role === "owner") return "所有者";
    if (role === "admin") return "管理员";
    return "成员";
}

function formatDateTime(value: string) {
    return new Date(value).toLocaleString("zh-CN", { hour12: false, month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
