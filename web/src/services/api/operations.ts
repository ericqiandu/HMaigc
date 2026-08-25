import axios from "axios";

export type OperationsAction = "upgrade" | "rollback" | "backup" | "verify";
export type OperationsStatus = "queued" | "running" | "cancelling" | "recovering" | "succeeded" | "failed" | "cancelled" | "recovery_required";
export type OperationsStage =
    | "accepted"
    | "runner_preparing"
    | "online_preflight"
    | "public_verifying"
    | "quiescing"
    | "quiesced_audit"
    | "backing_up"
    | "starting_target"
    | "verifying_target"
    | "restoring_current"
    | "restoring_backup"
    | "restoring_rollback_backup"
    | "committing_release"
    | "controller_handoff"
    | "completed";
export type OperationsServiceState = "current_online" | "maintenance" | "target_online" | "current_restored" | "unknown";
export type OperationsRecoveryAction = "none" | "retry_preflight" | "restore_current" | "restore_backup" | "commit_target" | "continue_controller_handoff" | "require_operator";
export type OperationsControllerHandoff = "unchanged" | "updated" | "restored_previous";

export type OperationsWarning = {
    code: string;
    message: string;
    facts?: unknown;
};

export type OperationsRecord = {
    id: string;
    action: OperationsAction | "install";
    targetVersion?: string;
    currentVersionAtStart?: string;
    resultVersion?: string;
    status: OperationsStatus;
    stage: OperationsStage;
    phase: string;
    runnerVersion?: string;
    runnerDigest?: string;
    runnerGeneration?: number;
    heartbeatAt?: string;
    serviceState: OperationsServiceState;
    checkpointSequence?: number;
    cancelRequestedAt?: string;
    recoveryAction?: OperationsRecoveryAction;
    controllerVersionAtStart?: string;
    resultControllerVersion?: string;
    controllerHandoff?: OperationsControllerHandoff;
    warnings?: OperationsWarning[];
    errorCode?: string;
    error?: string;
    exitCode?: number;
    actorUserId: string;
    actorDisplayName: string;
    idempotencyKey: string;
    createdAt: string;
    startedAt?: string;
    completedAt?: string;
    updatedAt: string;
};

export type OperationsLog = {
    sequence: number;
    operationId: string;
    stream: "stdout" | "stderr" | "system";
    message: string;
    createdAt: string;
};

export type OperationsBackup = {
    name: string;
    path: string;
    version: string;
    createdAt: string;
    sizeBytes: number;
    checksumStatus: "verified" | "invalid";
    validationError?: string;
};

export type PublicVerification = {
    status: "not_run" | "succeeded" | "failed";
    operationId: string;
    checkedAt: string | null;
    errorCode: string;
    error: string;
};

export type OperationsOverview = {
    controller: {
        status: string;
        version: string;
        commit: string;
    };
    release: {
        status: "ok" | "failed" | "unconfigured";
        currentVersion?: string;
        latestVersion?: string;
        updateAvailable: boolean;
        checkedAt: string;
        message?: string;
    };
    activeOperation?: OperationsRecord;
    latestOperation?: OperationsRecord;
    latestBackup?: OperationsBackup;
    rollbackReady: boolean;
    rollbackStatus: string;
    previousVersion?: string;
    publicVerification: PublicVerification;
    updatedAt: string;
};

type BackendEnvelope<T> = {
    code: number;
    data: T;
    msg: string;
};

const api = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) {
            throw new Error(error.response?.data?.msg || error.message || "请求失败");
        }
        throw error;
    }
}

export function getOperationsOverview() {
    return request<OperationsOverview>(api.get("/admin/operations/overview"));
}

export function listOperations(limit = 50) {
    return request<{ items: OperationsRecord[]; total: number }>(api.get("/admin/operations", { params: { limit } }));
}

export function getOperation(id: string) {
    return request<OperationsRecord>(api.get(`/admin/operations/${encodeURIComponent(id)}`));
}

export function listOperationLogs(id: string, after = 0, limit = 500) {
    return request<{ items: OperationsLog[]; nextCursor: number }>(api.get(`/admin/operations/${encodeURIComponent(id)}/logs`, { params: { after, limit } }));
}

export function listOperationBackups(limit = 50) {
    return request<OperationsBackup[]>(api.get("/admin/operations/backups", { params: { limit } }));
}

export function startOperation(input: { action: OperationsAction; targetVersion?: string; confirmation: string; idempotencyKey: string }) {
    return request<OperationsRecord>(api.post("/admin/operations", input));
}

export function cancelOperation(id: string, input: { confirmation: string; idempotencyKey: string }) {
    return request<OperationsRecord>(api.post(`/admin/operations/${encodeURIComponent(id)}/cancel`, input));
}

export function recoverOperation(id: string, input: { confirmation: string; idempotencyKey: string }) {
    return request<OperationsRecord>(api.post(`/admin/operations/${encodeURIComponent(id)}/recover`, input));
}
