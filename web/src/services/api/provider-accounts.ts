import axios from "axios";

export type ProviderHealthStatus = "unverified" | "healthy" | "insufficient_balance" | "invalid" | "blocked" | "unavailable" | "rejected" | "unknown";

export type ProviderModelSpec = {
    modelKey: string;
    displayName: string;
    upstreamMode: string;
    capability: string;
    resolutions: string[];
    ratios: string[];
    durationMin: number;
    durationMax: number;
    supportsSmartDuration: boolean;
    supportsGeneratedAudio: boolean;
    supportsWatermark: boolean;
    maxImages: number;
    maxVideos: number;
    maxAudios: number;
};

export type ProviderAdapterDescriptor = {
    providerKind: string;
    family: string;
    models: ProviderModelSpec[];
};

export type AdminProviderEndpoint = { baseUrl: string; version: number; active: boolean };

export type AdminProviderCredentialCandidate = {
    hasKey: boolean;
    keyFingerprint: string;
    version: number;
    healthStatus: ProviderHealthStatus;
    walletBalanceSubunits: string;
    verifiedAt?: string;
};

export type AdminProviderCredential = AdminProviderCredentialCandidate & {
    family: string;
    candidate?: AdminProviderCredentialCandidate;
};

export type AdminProviderAccount = {
    providerKind: string;
    name: string;
    enabled: boolean;
    endpoint?: AdminProviderEndpoint;
    endpointCandidate?: AdminProviderEndpoint;
    credentials: AdminProviderCredential[];
    adapters: ProviderAdapterDescriptor[];
};

export type ProviderAccountRequest = { method: "GET" | "PUT" | "POST"; path: string; data?: unknown };
export type ProviderAccountTransport = (request: ProviderAccountRequest) => Promise<unknown>;

type BackendEnvelope = { code: number; data: unknown; msg: string };

const providerApi = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

const healthStatuses = new Set<ProviderHealthStatus>(["unverified", "healthy", "insufficient_balance", "invalid", "blocked", "unavailable", "rejected", "unknown"]);

function record(value: unknown, label: string): Record<string, unknown> {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} 必须是对象`);
    return value as Record<string, unknown>;
}

function stringField(source: Record<string, unknown>, key: string, label: string): string {
    const value = source[key];
    if (typeof value !== "string") throw new Error(`${label}.${key} 必须是字符串`);
    return value;
}

function booleanField(source: Record<string, unknown>, key: string, label: string): boolean {
    const value = source[key];
    if (typeof value !== "boolean") throw new Error(`${label}.${key} 必须是布尔值`);
    return value;
}

function integerField(source: Record<string, unknown>, key: string, label: string): number {
    const value = source[key];
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error(`${label}.${key} 必须是非负安全整数`);
    return value;
}

function stringArrayField(source: Record<string, unknown>, key: string, label: string): string[] {
    const value = source[key];
    if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) throw new Error(`${label}.${key} 必须是字符串数组`);
    return [...value];
}

function optionalTimestamp(source: Record<string, unknown>, key: string, label: string): string | undefined {
    const value = source[key];
    if (value === undefined) return undefined;
    if (typeof value !== "string") throw new Error(`${label}.${key} 必须是时间字符串`);
    return value;
}

function balanceSubunitsField(source: Record<string, unknown>, key: string, label: string): string {
    const value = stringField(source, key, label);
    if (value !== "" && !/^(?:0|[1-9]\d*)$/.test(value)) {
        throw new Error(`${label}.${key} 必须是空字符串或规范化非负十进制整数`);
    }
    return value;
}

function parseHealthStatus(value: unknown): ProviderHealthStatus {
    if (typeof value !== "string" || !healthStatuses.has(value as ProviderHealthStatus)) {
        throw new Error(`不受支持的 healthStatus: ${String(value)}`);
    }
    return value as ProviderHealthStatus;
}

function parseEndpoint(value: unknown, label: string): AdminProviderEndpoint {
    const source = record(value, label);
    return {
        baseUrl: stringField(source, "baseUrl", label),
        version: integerField(source, "version", label),
        active: booleanField(source, "active", label),
    };
}

function parseCredentialCandidate(value: unknown, label: string): AdminProviderCredentialCandidate {
    const source = record(value, label);
    return {
        hasKey: booleanField(source, "hasKey", label),
        keyFingerprint: stringField(source, "keyFingerprint", label),
        version: integerField(source, "version", label),
        healthStatus: parseHealthStatus(source.healthStatus),
        walletBalanceSubunits: balanceSubunitsField(source, "walletBalanceSubunits", label),
        verifiedAt: optionalTimestamp(source, "verifiedAt", label),
    };
}

function parseCredential(value: unknown, index: number): AdminProviderCredential {
    const label = `credentials[${index}]`;
    const source = record(value, label);
    const candidate = source.candidate === undefined ? undefined : parseCredentialCandidate(source.candidate, `${label}.candidate`);
    return {
        ...parseCredentialCandidate(source, label),
        family: stringField(source, "family", label),
        candidate,
    };
}

function parseModel(value: unknown, label: string): ProviderModelSpec {
    const source = record(value, label);
    return {
        modelKey: stringField(source, "modelKey", label),
        displayName: stringField(source, "displayName", label),
        upstreamMode: stringField(source, "upstreamMode", label),
        capability: stringField(source, "capability", label),
        resolutions: stringArrayField(source, "resolutions", label),
        ratios: stringArrayField(source, "ratios", label),
        durationMin: integerField(source, "durationMin", label),
        durationMax: integerField(source, "durationMax", label),
        supportsSmartDuration: booleanField(source, "supportsSmartDuration", label),
        supportsGeneratedAudio: booleanField(source, "supportsGeneratedAudio", label),
        supportsWatermark: booleanField(source, "supportsWatermark", label),
        maxImages: integerField(source, "maxImages", label),
        maxVideos: integerField(source, "maxVideos", label),
        maxAudios: integerField(source, "maxAudios", label),
    };
}

function parseAdapter(value: unknown, index: number): ProviderAdapterDescriptor {
    const label = `adapters[${index}]`;
    const source = record(value, label);
    if (!Array.isArray(source.models)) throw new Error(`${label}.models 必须是数组`);
    return {
        providerKind: stringField(source, "providerKind", label),
        family: stringField(source, "family", label),
        models: source.models.map((model, modelIndex) => parseModel(model, `${label}.models[${modelIndex}]`)),
    };
}

export function parseAdminProviderAccount(value: unknown): AdminProviderAccount {
    const source = record(value, "providerAccount");
    if (!Array.isArray(source.credentials)) throw new Error("providerAccount.credentials 必须是数组");
    if (!Array.isArray(source.adapters)) throw new Error("providerAccount.adapters 必须是数组");
    return {
        providerKind: stringField(source, "providerKind", "providerAccount"),
        name: stringField(source, "name", "providerAccount"),
        enabled: booleanField(source, "enabled", "providerAccount"),
        endpoint: source.endpoint === undefined ? undefined : parseEndpoint(source.endpoint, "providerAccount.endpoint"),
        endpointCandidate: source.endpointCandidate === undefined ? undefined : parseEndpoint(source.endpointCandidate, "providerAccount.endpointCandidate"),
        credentials: source.credentials.map(parseCredential),
        adapters: source.adapters.map(parseAdapter),
    };
}

export function providerCredentialSecretRequest(value: string): { key: string } | null {
    const key = value.trim();
    return key ? { key } : null;
}

async function axiosTransport(request: ProviderAccountRequest): Promise<unknown> {
    try {
        const response = await providerApi.request<BackendEnvelope>({ method: request.method, url: request.path, data: request.data });
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope>(error)) {
            throw new Error(error.response?.data?.msg || error.message || "请求失败");
        }
        throw error;
    }
}

export function createProviderAccountsApi(transport: ProviderAccountTransport) {
    const accountRequest = async (request: ProviderAccountRequest) => parseAdminProviderAccount(await transport(request));
    return {
        get: () => accountRequest({ method: "GET", path: "/admin/providers/kuaizi" }),
        saveEndpoint: (baseUrl: string) => accountRequest({ method: "PUT", path: "/admin/providers/kuaizi", data: { baseUrl: baseUrl.trim() } }),
        saveCredential: (family: string, key: string): Promise<AdminProviderAccount | null> => {
            const data = providerCredentialSecretRequest(key);
            if (!data) return Promise.resolve(null);
            return accountRequest({ method: "PUT", path: `/admin/providers/kuaizi/credentials/${encodeURIComponent(family)}`, data });
        },
        verifyCredential: (family: string) => accountRequest({ method: "POST", path: `/admin/providers/kuaizi/credentials/${encodeURIComponent(family)}/verify` }),
    };
}

export type ProviderAccountsApi = ReturnType<typeof createProviderAccountsApi>;

export const providerAccountsApi = createProviderAccountsApi(axiosTransport);
