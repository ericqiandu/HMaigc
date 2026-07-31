import axios from "axios";

export type StorageMigrationStatus = "preparing" | "queued" | "running" | "succeeded" | "partial_failed" | "failed";
export type StorageMigrationItemStatus = "pending" | "running" | "committed" | "failed";

export type StorageMigrationJob = {
    id: string;
    status: StorageMigrationStatus;
    requestedBy: string;
    targetProvider: string;
    targetEndpoint: string;
    targetBucket: string;
    targetPrefix: string;
    totalItems: number;
    committedItems: number;
    failedItems: number;
    totalBytes: number;
    committedBytes: number;
    error?: string;
    startedAt?: string;
    completedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type StorageMigrationItem = {
    id: string;
    jobId: string;
    resourceId: string;
    status: StorageMigrationItemStatus;
    sourceObjectKey: string;
    targetObjectKey: string;
    size: number;
    sourceSha256?: string;
    targetEtag?: string;
    attemptCount: number;
    error?: string;
    startedAt?: string;
    completedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type StorageMigrationOverview = {
    eligible: { items: number; bytes: number };
    active?: StorageMigrationJob;
    latest?: StorageMigrationJob;
    items: StorageMigrationItem[];
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

export function getStorageMigrationOverview() {
    return request<StorageMigrationOverview>(api.get("/admin/storage-migrations/overview"));
}

export function startStorageMigration(confirmation: string) {
    return request<StorageMigrationJob>(api.post("/admin/storage-migrations", { confirmation }));
}

export function retryStorageMigration(jobId: string, confirmation: string) {
    return request<StorageMigrationJob>(api.post(`/admin/storage-migrations/${encodeURIComponent(jobId)}/retry`, { confirmation }));
}
