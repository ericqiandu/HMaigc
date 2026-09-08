import axios from "axios";

import { exactObject, flag } from "./strict-contract";

const api = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

const productRolloutFlagKeys = ["canvasToolRegistryV2", "canvasMediaToolsV2", "canvasProductionNodesV2", "creativeWorkspaceV2", "skillLibraryV2"] as const;

export type ProductRolloutFlags = Record<(typeof productRolloutFlagKeys)[number], boolean>;

type BackendEnvelope<T> = { code: number; data: T; msg: string };

export const productRolloutsQueryKey = ["product-rollouts"] as const;

export function parseProductRolloutFlags(value: unknown): ProductRolloutFlags {
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("产品能力配置必须是对象");
    const missing = productRolloutFlagKeys.find((key) => !Object.prototype.hasOwnProperty.call(value, key));
    if (missing) throw new Error(`产品能力配置缺少字段: ${missing}`);
    const source = exactObject(value, "产品能力配置", productRolloutFlagKeys);
    return {
        canvasToolRegistryV2: flag(source.canvasToolRegistryV2, "canvasToolRegistryV2"),
        canvasMediaToolsV2: flag(source.canvasMediaToolsV2, "canvasMediaToolsV2"),
        canvasProductionNodesV2: flag(source.canvasProductionNodesV2, "canvasProductionNodesV2"),
        creativeWorkspaceV2: flag(source.creativeWorkspaceV2, "creativeWorkspaceV2"),
        skillLibraryV2: flag(source.skillLibraryV2, "skillLibraryV2"),
    };
}

export async function getProductRollouts(signal?: AbortSignal): Promise<ProductRolloutFlags> {
    try {
        const response = await api.get<BackendEnvelope<unknown>>("/product-rollouts", { signal });
        if (response.data.code !== 0) throw new Error(response.data.msg || "产品能力配置不可用");
        return parseProductRolloutFlags(response.data.data);
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) throw new Error(error.response?.data?.msg || error.message || "产品能力配置不可用");
        throw error;
    }
}
