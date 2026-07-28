import axios from "axios";

const api = axios.create({ baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api", withCredentials: true });

type BackendEnvelope<T> = { code: number; data: T; msg: string };

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) throw new Error(error.response?.data?.msg || error.message || "请求失败");
        throw error;
    }
}

export type TeamRole = "owner" | "admin" | "member";

export type Team = {
    id: string;
    ownerUserId: string;
    name: string;
    status: "active" | "disabled";
    createdAt: string;
    updatedAt: string;
};

export type TeamSubscription = {
    planId: string;
    planName: string;
    planTier: string;
    seatLimit: number;
    endsAt?: string;
};

export type TeamSummary = {
    team: Team;
    currentRole: TeamRole;
    seatUsed: number;
    invitationSeatReserved: number;
    subscription?: TeamSubscription;
};

export type TeamMember = {
    id: string;
    teamId: string;
    userId: string;
    role: TeamRole;
    status: "active" | "removed";
    username: string;
    displayName: string;
    createdAt: string;
    updatedAt: string;
};

export type TeamInvitation = {
    id: string;
    teamId: string;
    inviterUserId: string;
    email: string;
    role: Exclude<TeamRole, "owner">;
    status: "pending" | "accepted" | "revoked" | "expired";
    expiresAt: string;
    createdAt: string;
    updatedAt: string;
};

export type IncomingTeamInvitation = TeamInvitation & {
    teamName: string;
    inviterName: string;
};

export type TeamAuditEvent = {
    id: string;
    teamId: string;
    actorUserId: string;
    action: string;
    targetUserId?: string;
    targetInvitationId?: string;
    metadataJson: string;
    actorName: string;
    targetName?: string;
    createdAt: string;
};

export type TeamDetail = {
    summary: TeamSummary;
    members: TeamMember[];
    invitations: TeamInvitation[];
    auditEvents: TeamAuditEvent[];
};

export type TeamWorkspace = {
    teams: TeamSummary[];
    incomingInvitations: IncomingTeamInvitation[];
};

export function getTeamWorkspace() {
    return request<TeamWorkspace>(api.get("/teams"));
}

export function getTeamDetail(teamId: string) {
    return request<TeamDetail>(api.get(`/teams/${encodeURIComponent(teamId)}`));
}

export function createTeam(name: string) {
    return request<Team>(api.post("/teams", { name }));
}

export function renameTeam(teamId: string, name: string) {
    return request<Team>(api.patch(`/teams/${encodeURIComponent(teamId)}`, { name }));
}

export function createTeamInvitation(teamId: string, input: { email: string; role: Exclude<TeamRole, "owner"> }) {
    return request<{ invitation: TeamInvitation; acceptToken: string }>(api.post(`/teams/${encodeURIComponent(teamId)}/invitations`, input));
}

export function acceptTeamInvitationById(invitationId: string) {
    return request<TeamMember>(api.post(`/team-invitations/${encodeURIComponent(invitationId)}/accept`, {}));
}

export function acceptTeamInvitationByToken(token: string) {
    return request<TeamMember>(api.post("/team-invitations/accept", { token }));
}

export function revokeTeamInvitation(teamId: string, invitationId: string) {
    return request<{ revoked: boolean }>(api.delete(`/teams/${encodeURIComponent(teamId)}/invitations/${encodeURIComponent(invitationId)}`));
}

export function updateTeamMemberRole(teamId: string, memberId: string, role: Exclude<TeamRole, "owner">) {
    return request<{ updated: boolean }>(api.patch(`/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(memberId)}`, { role }));
}

export function removeTeamMember(teamId: string, memberId: string) {
    return request<{ removed: boolean }>(api.delete(`/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(memberId)}`));
}

export function leaveTeam(teamId: string) {
    return request<{ left: boolean }>(api.post(`/teams/${encodeURIComponent(teamId)}/leave`, {}));
}
