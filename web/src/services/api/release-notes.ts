import axios from "axios";

import type { BackendEnvelope } from "@/services/api/task-center";

export type AdminReleaseNotes = {
    version: string;
    changelog: string;
};

const api = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

export async function getAdminReleaseNotes(signal?: AbortSignal): Promise<AdminReleaseNotes> {
    try {
        const response = await api.get<BackendEnvelope<AdminReleaseNotes>>("/admin/releases/changelog", { signal });
        if (response.data.code !== 0) throw new Error(response.data.msg || "读取更新日志失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) {
            throw new Error(error.response?.data?.msg || error.message || "读取更新日志失败");
        }
        throw error;
    }
}
