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

export type ReferralRelationshipStatus = "eligible" | "rewarded" | "disqualified";

export type ReferralRule = {
    id?: string;
    membershipPlanId: string;
    inviterRewardMicrocredits: number;
    inviteeRewardMicrocredits: number;
    enabled: boolean;
    planCode: string;
    planName: string;
    planTier: string;
    billingCycle: "month" | "year";
    priceCents: number;
    currency: string;
    createdAt?: string;
    updatedAt?: string;
};

export type ReferralInvitation = {
    id: string;
    inviterUserId: string;
    inviteeUserId: string;
    inviteeUsername: string;
    inviteeDisplayName: string;
    referralCode: string;
    bindingIp?: string;
    status: ReferralRelationshipStatus;
    planName?: string;
    rewardedMicrocredits: number;
    disqualificationReason?: string;
    rewardedAt?: string;
    boundAt: string;
};

export type ReferralCenterData = {
    program: { enabled: boolean };
    inviteCode: string;
    summary: {
        registeredCount: number;
        purchasedCount: number;
        earnedInviterMicrocredits: number;
    };
    rules: ReferralRule[];
    invitations: ReferralInvitation[];
    total: number;
};

export type AdminReferralProgramData = {
    program: { enabled: boolean };
    summary: {
        registeredCount: number;
        purchasedCount: number;
        grantedTotalMicrocredits: number;
    };
    rules: ReferralRule[];
    invites: ReferralInvitation[];
    total: number;
};

export function getReferralCenter(page = 1, limit = 20) {
    return request<ReferralCenterData>(api.get("/referrals/me", { params: { page, limit } }));
}

export function getAdminReferralProgram(page = 1, limit = 20) {
    return request<AdminReferralProgramData>(api.get("/admin/referral-program", { params: { page, limit } }));
}

export function updateAdminReferralProgram(enabled: boolean) {
    return request<{ program: { enabled: boolean } }>(api.patch("/admin/referral-program", { enabled }));
}

export function updateAdminReferralRule(
    planId: string,
    input: { inviterRewardMicrocredits: number; inviteeRewardMicrocredits: number; enabled: boolean },
) {
    return request<{ rule: ReferralRule }>(api.put(`/admin/referral-program/rules/${encodeURIComponent(planId)}`, input));
}

export function disqualifyAdminReferral(relationshipId: string, reason: string) {
    return request<{ ok: boolean }>(api.post(`/admin/referral-program/relationships/${encodeURIComponent(relationshipId)}/disqualify`, { reason }));
}
