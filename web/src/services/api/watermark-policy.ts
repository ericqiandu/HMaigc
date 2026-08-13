import axios from "axios";

const api = axios.create({ baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api", withCredentials: true });

type BackendEnvelope = { code: number; data: unknown; msg: string };
export type WatermarkPreferenceStatus = "disabled" | "active" | "policy_updated" | "policy_unavailable";
export type WatermarkPolicyPublication = {
    id: string;
    version: number;
    managementRuleRichText: string;
    watermarkPolicyUrl: string;
    contentHash: string;
    publishedBy: string;
    publishedAt: string;
};
export type WatermarkPreferenceView = {
    removeWatermark: boolean;
    status: WatermarkPreferenceStatus;
    canEnable: boolean;
    acceptedAt: string | null;
    currentPolicy: WatermarkPolicyPublication | null;
};
export type UpdateWatermarkPreferenceInput = { removeWatermark: boolean; publicationId: string };
export type PublishWatermarkPolicyInput = { managementRuleRichText: string; watermarkPolicyUrl: string };
export type WatermarkPolicyApi = {
    getPreference: () => Promise<WatermarkPreferenceView>;
    updatePreference: (input: UpdateWatermarkPreferenceInput) => Promise<WatermarkPreferenceView>;
    getAdminPolicy: () => Promise<WatermarkPolicyPublication | null>;
    publishPolicy: (input: PublishWatermarkPolicyInput) => Promise<WatermarkPolicyPublication>;
};

export class WatermarkPolicyConflictError extends Error {}
export const watermarkPreferenceQueryKey = ["me", "watermark-preference"] as const;
export const adminWatermarkPolicyQueryKey = ["admin", "legal", "ai-watermark-policy"] as const;

async function request(promise: Promise<{ data: BackendEnvelope }>, parse: (value: unknown) => unknown) {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return parse(response.data.data);
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope>(error)) {
            const message = error.response?.data?.msg || error.message || "请求失败";
            if (error.response?.status === 409) throw new WatermarkPolicyConflictError(message);
            throw new Error(message);
        }
        throw error;
    }
}

function record(value: unknown, label: string): Record<string, unknown> {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label}响应格式无效`);
    return value as Record<string, unknown>;
}
function stringField(value: Record<string, unknown>, key: string) {
    if (typeof value[key] !== "string") throw new Error(`水印规范响应缺少 ${key}`);
    return value[key] as string;
}
function publication(value: unknown): WatermarkPolicyPublication {
    const item = record(value, "水印规范");
    if (!Number.isSafeInteger(item.version) || Number(item.version) <= 0) throw new Error("水印规范版本无效");
    return {
        id: stringField(item, "id"),
        version: Number(item.version),
        managementRuleRichText: stringField(item, "managementRuleRichText"),
        watermarkPolicyUrl: stringField(item, "watermarkPolicyUrl"),
        contentHash: stringField(item, "contentHash"),
        publishedBy: stringField(item, "publishedBy"),
        publishedAt: stringField(item, "publishedAt"),
    };
}
function preference(value: unknown): WatermarkPreferenceView {
    const item = record(value, "水印设置");
    const statuses: WatermarkPreferenceStatus[] = ["disabled", "active", "policy_updated", "policy_unavailable"];
    if (
        typeof item.status !== "string" ||
        !statuses.includes(item.status as WatermarkPreferenceStatus) ||
        typeof item.removeWatermark !== "boolean" ||
        typeof item.canEnable !== "boolean" ||
        !(item.acceptedAt === null || typeof item.acceptedAt === "string")
    )
        throw new Error("水印设置响应格式无效");
    return { removeWatermark: item.removeWatermark, status: item.status as WatermarkPreferenceStatus, canEnable: item.canEnable, acceptedAt: item.acceptedAt, currentPolicy: item.currentPolicy === null ? null : publication(item.currentPolicy) };
}

export const watermarkPolicyApi: WatermarkPolicyApi = {
    getPreference: async () => preference(await request(api.get("/me/watermark-preference"), (value) => value)),
    updatePreference: async (input) => preference(await request(api.put("/me/watermark-preference", input), (value) => value)),
    getAdminPolicy: async () => request(api.get("/admin/legal/ai-watermark-policy"), (value) => (value === null ? null : publication(value))) as Promise<WatermarkPolicyPublication | null>,
    publishPolicy: async (input) => publication(await request(api.post("/admin/legal/ai-watermark-policy/publications", input), (value) => value)),
};
