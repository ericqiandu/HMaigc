import { useCallback, useMemo, useRef, useState } from "react";

import {
    acceptTeamInvitationById,
    acceptTeamInvitationByToken,
    createTeam,
    createTeamInvitation,
    leaveTeam,
    removeTeamMember,
    regenerateTeamInvitation,
    renameTeam,
    revokeTeamInvitation,
    updateTeamMemberPolicy,
    type TeamMember,
    type TeamRole,
} from "@/services/api/teams";

export type TeamCommandsApi = {
    createTeam?: typeof createTeam;
    renameTeam?: typeof renameTeam;
    createInvitation?: typeof createTeamInvitation;
    regenerateInvitation?: typeof regenerateTeamInvitation;
    acceptInvitationById?: typeof acceptTeamInvitationById;
    acceptInvitationByToken?: typeof acceptTeamInvitationByToken;
    updateMemberPolicy?: typeof updateTeamMemberPolicy;
    removeMember?: typeof removeTeamMember;
    revokeInvitation?: typeof revokeTeamInvitation;
    leaveTeam?: typeof leaveTeam;
};

type TeamCommandsOptions = {
    activeTeamId: string;
    api?: TeamCommandsApi;
    reloadActiveTeam: () => Promise<void>;
    reloadWorkspace: (preferredTeamId?: string) => Promise<void>;
};

export type TeamCommandsController = {
    busyKey: string;
    commandError: string;
    clearCommandError: () => void;
    create: (name: string, idempotencyKey: string) => ReturnType<typeof createTeam>;
    rename: (name: string) => ReturnType<typeof renameTeam>;
    invite: (email: string, role: Exclude<TeamRole, "owner">) => ReturnType<typeof createTeamInvitation>;
    regenerateInvitation: (invitationId: string) => ReturnType<typeof regenerateTeamInvitation>;
    acceptInvitation: (invitationId: string) => ReturnType<typeof acceptTeamInvitationById>;
    acceptInvitationToken: (token: string) => ReturnType<typeof acceptTeamInvitationByToken>;
    updateMember: (member: TeamMember, role: Exclude<TeamRole, "owner">, monthlyCreditLimitMicrocredits?: number) => ReturnType<typeof updateTeamMemberPolicy>;
    removeMember: (memberId: string) => ReturnType<typeof removeTeamMember>;
    revokeInvitation: (invitationId: string) => ReturnType<typeof revokeTeamInvitation>;
    leave: () => ReturnType<typeof leaveTeam>;
};

const defaultApi = {
    createTeam,
    renameTeam,
    createInvitation: createTeamInvitation,
    regenerateInvitation: regenerateTeamInvitation,
    acceptInvitationById: acceptTeamInvitationById,
    acceptInvitationByToken: acceptTeamInvitationByToken,
    updateMemberPolicy: updateTeamMemberPolicy,
    removeMember: removeTeamMember,
    revokeInvitation: revokeTeamInvitation,
    leaveTeam,
};

export function useTeamCommands({ activeTeamId, api: overrides, reloadActiveTeam, reloadWorkspace }: TeamCommandsOptions): TeamCommandsController {
    const api = useMemo(() => ({ ...defaultApi, ...overrides }), [overrides]);
    const inFlightKey = useRef("");
    const [busyKey, setBusyKey] = useState("");
    const [commandError, setCommandError] = useState("");

    const execute = useCallback(async <T>(key: string, operation: () => Promise<T>): Promise<T> => {
        if (inFlightKey.current) {
            const error = new Error("已有团队操作正在执行，请等待完成后重试");
            setCommandError(error.message);
            throw error;
        }
        inFlightKey.current = key;
        setBusyKey(key);
        setCommandError("");
        try {
            return await operation();
        } catch (error) {
            setCommandError(error instanceof Error ? error.message : "团队操作失败");
            throw error;
        } finally {
            inFlightKey.current = "";
            setBusyKey("");
        }
    }, []);

    const requireTeamId = useCallback(() => {
        if (!activeTeamId) throw new Error("尚未选择团队");
        return activeTeamId;
    }, [activeTeamId]);

    const refreshSnapshot = useCallback(async (reload: () => Promise<void>) => {
        try {
            await reload();
        } catch (error) {
            const detail = error instanceof Error ? error.message : "未知错误";
            setCommandError(`操作已成功，但刷新团队数据失败：${detail}`);
        }
    }, []);

    return {
        busyKey,
        commandError,
        clearCommandError: () => setCommandError(""),
        create: (name, idempotencyKey) =>
            execute("create", async () => {
                const team = await api.createTeam(name, idempotencyKey);
                await refreshSnapshot(() => reloadWorkspace(team.id));
                return team;
            }),
        rename: (name) =>
            execute("rename", async () => {
                const team = await api.renameTeam(requireTeamId(), name);
                await refreshSnapshot(reloadActiveTeam);
                return team;
            }),
        invite: (email, role) =>
            execute("invite", async () => {
                const invitation = await api.createInvitation(requireTeamId(), { email, role });
                await refreshSnapshot(reloadActiveTeam);
                return invitation;
            }),
        regenerateInvitation: (invitationId) =>
            execute(`regenerate:${invitationId}`, async () => {
                const invitation = await api.regenerateInvitation(requireTeamId(), invitationId);
                await refreshSnapshot(reloadActiveTeam);
                return invitation;
            }),
        acceptInvitation: (invitationId) =>
            execute(`accept:${invitationId}`, async () => {
                const member = await api.acceptInvitationById(invitationId);
                await refreshSnapshot(() => reloadWorkspace());
                return member;
            }),
        acceptInvitationToken: (token) =>
            execute("accept:token", async () => {
                const member = await api.acceptInvitationByToken(token);
                await refreshSnapshot(() => reloadWorkspace());
                return member;
            }),
        updateMember: (member, role, monthlyCreditLimitMicrocredits) =>
            execute(monthlyCreditLimitMicrocredits === undefined ? `role:${member.id}` : `credit-limit:${member.id}`, async () => {
                const result = await api.updateMemberPolicy(requireTeamId(), member.id, { role, monthlyCreditLimitMicrocredits, expectedUpdatedAt: member.updatedAt });
                await refreshSnapshot(reloadActiveTeam);
                return result;
            }),
        removeMember: (memberId) =>
            execute(`remove:${memberId}`, async () => {
                const result = await api.removeMember(requireTeamId(), memberId);
                await refreshSnapshot(reloadActiveTeam);
                return result;
            }),
        revokeInvitation: (invitationId) =>
            execute(`revoke:${invitationId}`, async () => {
                const result = await api.revokeInvitation(requireTeamId(), invitationId);
                await refreshSnapshot(reloadActiveTeam);
                return result;
            }),
        leave: () =>
            execute("leave", async () => {
                const result = await api.leaveTeam(requireTeamId());
                await refreshSnapshot(() => reloadWorkspace());
                return result;
            }),
    };
}
