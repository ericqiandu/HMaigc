import axios from "axios";

export type AdminAgentRunStatus =
    | "queued"
    | "running"
    | "waiting_input"
    | "waiting_approval"
    | "waiting_tool"
    | "succeeded"
    | "failed"
    | "cancelled";

export type AdminAgentRunActivity = "active" | "awaiting_user" | "possibly_stalled";

export type AdminAgentRunControlDisposition =
    | "interruptible_now"
    | "cancel_request_required"
    | "blocked_by_unresolved_billing"
    | "already_terminal";

export type AdminAgentRun = {
    runId: string;
    threadId: string;
    actorUserId: string;
    actorDisplayName: string;
    domainProjectId: string;
    canvasId: string;
    status: AdminAgentRunStatus;
    stateVersion: number;
    stepNumber: number;
    maxSteps: number;
    toolSchemaVersion: number;
    runtimeVersion: number;
    policyVersion: number;
    pendingKind: string;
    pendingToolName: string;
    updatedAt: string;
    inactiveSeconds: number;
    activityClassification: AdminAgentRunActivity;
    linkedModelTaskStatus: string;
    linkedMediaTaskStatus: string;
    billingState: string;
    providerRequestState: string;
    controlDisposition: AdminAgentRunControlDisposition;
    controlBlockedReason: string;
    confirmationPhrase?: string;
};

export type AdminAgentRunPage = {
    items: AdminAgentRun[];
    total: number;
    page: number;
    pageSize: number;
};

export type AdminAgentRunQuery = {
    status?: AdminAgentRunStatus;
    activity?: AdminAgentRunActivity;
    user?: string;
    scope?: string;
    updatedBefore?: string;
    page?: number;
    pageSize?: 20 | 50 | 100;
};

export type AdminAgentRunInterruptInput = {
    expectedStateVersion: number;
    reason: string;
    confirmation: string;
};

export type AdminAgentRunInterruptResult = {
    run: AdminAgentRun;
    disposition: AdminAgentRunControlDisposition;
    affectedTaskIds: string[];
    reconciliationPending: boolean;
};

type BackendEnvelope<T> = {
    code: number;
    data: T;
    msg: string;
};

type AdminAgentRunErrorData = {
    errorCode?: string;
    latestRun?: AdminAgentRun;
};

export class AdminAgentRunApiError extends Error {
    readonly status: number | undefined;
    readonly errorCode: string | undefined;
    readonly latestRun: AdminAgentRun | undefined;

    constructor(message: string, options: { status?: number; errorCode?: string; latestRun?: AdminAgentRun } = {}) {
        super(message);
        this.name = "AdminAgentRunApiError";
        this.status = options.status;
        this.errorCode = options.errorCode;
        this.latestRun = options.latestRun;
    }
}

const api = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) {
            throw new AdminAgentRunApiError(response.data.msg || "请求失败", { status: response.data.code });
        }
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<AdminAgentRunErrorData>>(error)) {
            const data = error.response?.data.data;
            throw new AdminAgentRunApiError(error.response?.data.msg || error.message || "请求失败", {
                status: error.response?.status,
                errorCode: data?.errorCode,
                latestRun: data?.latestRun,
            });
        }
        throw error;
    }
}

export function getAdminAgentRuns(query: AdminAgentRunQuery) {
    return request<AdminAgentRunPage>(api.get("/admin/agent-runs", { params: query }));
}

export function getAdminAgentRun(runId: string) {
    return request<AdminAgentRun>(api.get(`/admin/agent-runs/${encodeURIComponent(runId)}`));
}

export function interruptAdminAgentRun(runId: string, input: AdminAgentRunInterruptInput) {
    return request<AdminAgentRunInterruptResult>(api.post(`/admin/agent-runs/${encodeURIComponent(runId)}/interrupt`, input));
}
