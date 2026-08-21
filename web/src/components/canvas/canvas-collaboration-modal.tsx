import { useCallback, useEffect, useMemo, useState } from "react";
import { App, Avatar, Button, Empty, Modal, Select, Spin, Tag } from "antd";
import { Crown, ShieldCheck, UserRound, UsersRound, X } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import {
    configureCanvasCollaboration,
    deleteCanvasCollaborator,
    getCanvasCollaboration,
    updateCanvasCollaborator,
    type CanvasCollaborationState,
} from "@/services/api/canvas-collaboration";
import { getTeamWorkspace, type TeamMember, type TeamSummary } from "@/services/api/teams";
import { useThemeStore } from "@/stores/use-theme-store";
import { useUserStore } from "@/stores/use-user-store";

type CanvasCollaborationModalProps = {
    projectId: string;
    open: boolean;
    onClose: () => void;
    onStateChange: (state: CanvasCollaborationState) => void;
};

type MemberAccess = "default" | "viewer" | "editor";

export function CanvasCollaborationModal({
    projectId,
    open,
    onClose,
    onStateChange,
}: CanvasCollaborationModalProps) {
    const { message, modal } = App.useApp();
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const currentUserId = useUserStore((store) => store.user?.id);
    const [state, setState] = useState<CanvasCollaborationState | null>(null);
    const [teams, setTeams] = useState<TeamSummary[]>([]);
    const [selectedTeamId, setSelectedTeamId] = useState("");
    const [defaultAccess, setDefaultAccess] = useState<"viewer" | "editor">("editor");
    const [loading, setLoading] = useState(false);
    const [submittingKey, setSubmittingKey] = useState("");

    const applyLocalState = useCallback((next: CanvasCollaborationState) => {
        setState(next);
        setDefaultAccess(next.project.defaultTeamAccess === "viewer" ? "viewer" : "editor");
    }, []);

    const applyState = useCallback((next: CanvasCollaborationState) => {
        applyLocalState(next);
        onStateChange(next);
    }, [applyLocalState, onStateChange]);

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const [collaboration, workspace] = await Promise.all([
                getCanvasCollaboration(projectId),
                getTeamWorkspace(),
            ]);
            applyLocalState(collaboration);
            const manageableTeams = workspace.teams.filter((item) =>
                (item.currentRole === "owner" || item.currentRole === "admin") &&
                Boolean(item.subscription),
            );
            setTeams(manageableTeams);
            setSelectedTeamId(collaboration.access.teamId || manageableTeams[0]?.team.id || "");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取协作设置失败");
        } finally {
            setLoading(false);
        }
    }, [applyLocalState, message, projectId]);

    useEffect(() => {
        if (open) void load();
    }, [load, open]);

    const teamName = useMemo(
        () => state?.access.teamName || teams.find((item) => item.team.id === state?.access.teamId)?.team.name || "团队空间",
        [state?.access.teamId, state?.access.teamName, teams],
    );

    const attach = async () => {
        if (!selectedTeamId) {
            message.warning("请选择已开通团队会员的团队");
            return;
        }
        setSubmittingKey("attach");
        try {
            const next = await configureCanvasCollaboration(projectId, { teamId: selectedTeamId, defaultAccess });
            applyState(next);
            message.success("团队协作已启用");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "启用团队协作失败");
        } finally {
            setSubmittingKey("");
        }
    };

    const updateDefaultAccess = async (access: "viewer" | "editor") => {
        if (!state?.access.teamId) return;
        setDefaultAccess(access);
        setSubmittingKey("default");
        try {
            const next = await configureCanvasCollaboration(projectId, { teamId: state.access.teamId, defaultAccess: access });
            applyState(next);
            message.success("团队默认权限已更新");
        } catch (error) {
            setDefaultAccess(state.project.defaultTeamAccess === "viewer" ? "viewer" : "editor");
            message.error(error instanceof Error ? error.message : "更新默认权限失败");
        } finally {
            setSubmittingKey("");
        }
    };

    const updateMemberAccess = async (member: TeamMember, access: MemberAccess) => {
        setSubmittingKey(member.userId);
        try {
            const next = access === "default"
                ? await deleteCanvasCollaborator(projectId, member.userId)
                : await updateCanvasCollaborator(projectId, member.userId, access);
            applyState(next);
            message.success(access === "default" ? "已恢复团队默认权限" : "成员权限已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新成员权限失败");
        } finally {
            setSubmittingKey("");
        }
    };

    const detach = () => {
        modal.confirm({
            title: "移回个人空间？",
            content: "团队成员将立即失去访问权限，画布内容与版本记录不会被删除。",
            okText: "移回个人空间",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: async () => {
                setSubmittingKey("detach");
                try {
                    const next = await configureCanvasCollaboration(projectId, { teamId: "", defaultAccess: "viewer" });
                    applyState(next);
                    message.success("画布已移回个人空间");
                } finally {
                    setSubmittingKey("");
                }
            },
        });
    };

    const isTeamCanvas = Boolean(state?.access.teamId);
    const collaboratorByUserId = useMemo(
        () => new Map((state?.collaborators || []).map((item) => [item.userId, item])),
        [state?.collaborators],
    );

    return (
        <Modal
            rootClassName="canvas-overlay-modal canvas-overlay-modal--collaboration"
            title={<span className="canvas-collaboration-modal-title inline-flex items-center gap-2"><UsersRound className="size-4" />多人协作</span>}
            open={open}
            onCancel={onClose}
            footer={null}
            centered
            width={620}
            destroyOnHidden
        >
            <Spin spinning={loading}>
                <div className="canvas-overlay-body canvas-collaboration-modal-body">
                    {!isTeamCanvas ? (
                        <div className="canvas-collaboration-setup space-y-5">
                            <div className="canvas-collaboration-intro">
                                <div className="canvas-collaboration-intro-title text-sm font-semibold">将画布加入团队</div>
                                <p className="canvas-collaboration-intro-copy mt-1 text-xs leading-5" style={{ color: theme.node.muted }}>
                                    团队成员可实时看到节点、连线和内容变化。画布版本由服务端统一排序，发生并发冲突时会显式重新同步。
                                </p>
                            </div>
                            {teams.length ? (
                                <div className="canvas-collaboration-setup-controls grid gap-3 sm:grid-cols-[1fr_180px]">
                                    <Select
                                        className="canvas-collaboration-team-select w-full"
                                        value={selectedTeamId || undefined}
                                        placeholder="选择团队"
                                        onChange={setSelectedTeamId}
                                        options={teams.map((item) => ({
                                            value: item.team.id,
                                            label: `${item.team.name} · ${item.seatUsed}/${item.subscription?.seatLimit || item.seatUsed} 人`,
                                        }))}
                                    />
                                    <Select
                                        className="canvas-collaboration-default-select w-full"
                                        value={defaultAccess}
                                        onChange={setDefaultAccess}
                                        options={[
                                            { value: "editor", label: "成员默认可编辑" },
                                            { value: "viewer", label: "成员默认仅查看" },
                                        ]}
                                    />
                                </div>
                            ) : (
                                <Empty
                                    className="canvas-collaboration-empty py-3"
                                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                                    description="暂无可管理且已开通团队会员的团队"
                                />
                            )}
                            <div className="canvas-collaboration-setup-footer flex justify-end">
                                <Button
                                    type="primary"
                                    icon={<UsersRound className="size-4" />}
                                    disabled={!teams.length}
                                    loading={submittingKey === "attach"}
                                    onClick={() => void attach()}
                                >
                                    启用团队协作
                                </Button>
                            </div>
                        </div>
                    ) : (
                        <div className="canvas-collaboration-management space-y-5">
                            <div className="canvas-collaboration-summary flex items-start justify-between gap-4">
                                <div className="canvas-collaboration-summary-copy min-w-0">
                                    <div className="canvas-collaboration-team-name flex items-center gap-2 text-sm font-semibold">
                                        <ShieldCheck className="size-4" style={{ color: theme.accent.primary }} />
                                        <span className="canvas-collaboration-team-name-text truncate">{teamName}</span>
                                        <Tag className="canvas-collaboration-live-tag !m-0" color={state?.access.teamSubscriptionActive ? "blue" : "default"}>
                                            {state?.access.teamSubscriptionActive ? "协作有效" : "只读"}
                                        </Tag>
                                    </div>
                                    <p className="canvas-collaboration-summary-description mt-1 text-xs" style={{ color: theme.node.muted }}>
                                        当前权限：{accessLabel(state?.access.level)} · 版本 {state?.project.revision || 0}
                                    </p>
                                </div>
                                {state?.access.canManage ? (
                                    <Select
                                        className="canvas-collaboration-manage-default w-40 shrink-0"
                                        value={defaultAccess}
                                        loading={submittingKey === "default"}
                                        onChange={(value) => void updateDefaultAccess(value)}
                                        options={[
                                            { value: "editor", label: "默认可编辑" },
                                            { value: "viewer", label: "默认仅查看" },
                                        ]}
                                    />
                                ) : null}
                            </div>

                            <div className="canvas-collaboration-members">
                                <div className="canvas-collaboration-members-heading mb-2 text-xs font-semibold" style={{ color: theme.node.muted }}>
                                    团队成员 · {state?.teamMembers.length || 0}
                                </div>
                                <div className="canvas-collaboration-member-list max-h-72 overflow-y-auto">
                                    {(state?.teamMembers || []).map((member) => {
                                        const override = collaboratorByUserId.get(member.userId);
                                        const fixedManager = member.role === "owner" || member.role === "admin";
                                        return (
                                            <div key={member.userId} className="canvas-collaboration-member-row flex min-h-14 items-center gap-3 py-2">
                                                <Avatar
                                                    className="canvas-collaboration-member-avatar shrink-0"
                                                    src={override?.avatarUrl}
                                                    icon={<UserRound className="size-4" />}
                                                />
                                                <div className="canvas-collaboration-member-copy min-w-0 flex-1">
                                                    <div className="canvas-collaboration-member-name flex items-center gap-1.5 text-sm font-medium">
                                                        <span className="canvas-collaboration-member-name-text truncate">{member.displayName || member.username}</span>
                                                        {member.role === "owner" ? <Crown className="size-3.5 text-amber-500" /> : null}
                                                    </div>
                                                    <div className="canvas-collaboration-member-role mt-0.5 text-[11px]" style={{ color: theme.node.muted }}>
                                                        {teamRoleLabel(member.role)}
                                                        {override ? ` · 单独设为${accessLabel(override.access)}` : ` · 继承${accessLabel(defaultAccess)}`}
                                                    </div>
                                                </div>
                                                {state?.access.canManage && !fixedManager ? (
                                                    <Select
                                                        className="canvas-collaboration-member-access w-32 shrink-0"
                                                        value={override?.access || "default"}
                                                        loading={submittingKey === member.userId}
                                                        onChange={(value: MemberAccess) => void updateMemberAccess(member, value)}
                                                        options={[
                                                            { value: "default", label: "继承默认" },
                                                            { value: "editor", label: "可编辑" },
                                                            { value: "viewer", label: "仅查看" },
                                                        ]}
                                                    />
                                                ) : (
                                                    <span className="canvas-collaboration-member-fixed shrink-0 text-xs font-medium" style={{ color: theme.node.muted }}>
                                                        {fixedManager ? "管理者" : accessLabel(override?.access || defaultAccess)}
                                                    </span>
                                                )}
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>

                            {state?.access.canManage && state.project.ownerUserId === currentUserId ? (
                                <div className="canvas-collaboration-management-footer flex justify-end">
                                    <Button danger type="text" icon={<X className="size-4" />} loading={submittingKey === "detach"} onClick={detach}>
                                        移回个人空间
                                    </Button>
                                </div>
                            ) : null}
                        </div>
                    )}
                </div>
            </Spin>
        </Modal>
    );
}

function accessLabel(access?: "viewer" | "editor" | "manager") {
    if (access === "manager") return "管理";
    if (access === "editor") return "编辑";
    return "查看";
}

function teamRoleLabel(role: TeamMember["role"]) {
    if (role === "owner") return "团队所有者";
    if (role === "admin") return "团队管理员";
    return "团队成员";
}
