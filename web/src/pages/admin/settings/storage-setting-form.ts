import type { AdminOSSSetting } from "@/services/api/auth";

export type OSSFormValues = {
    enabled?: boolean;
    provider: "aliyun";
    region?: string;
    endpoint?: string;
    bucket?: string;
    accessKeyId?: string;
    accessKeySecret?: string;
    pathPrefix?: string;
};

export function storageFormValues(setting?: AdminOSSSetting | null): OSSFormValues {
    return {
        enabled: setting?.enabled ?? false,
        provider: setting?.provider ?? "aliyun",
        region: setting?.region ?? "",
        endpoint: setting?.endpoint ?? "",
        bucket: setting?.bucket ?? "",
        accessKeyId: setting?.accessKeyId ?? "",
        accessKeySecret: "",
        pathPrefix: setting?.pathPrefix ?? "",
    };
}

export function normalizeStorageFormValues(values: OSSFormValues): OSSFormValues {
    return {
        enabled: values.enabled === true,
        provider: values.provider || "aliyun",
        region: clean(values.region),
        endpoint: clean(values.endpoint).replace(/\/+$/, ""),
        bucket: clean(values.bucket),
        accessKeyId: clean(values.accessKeyId),
        accessKeySecret: clean(values.accessKeySecret),
        pathPrefix: clean(values.pathPrefix).replace(/^\/+|\/+$/g, ""),
    };
}

export function storageValuesEqual(values: OSSFormValues, setting: AdminOSSSetting | null): boolean {
    const current = normalizeStorageFormValues(storageFormValues(setting));
    const candidate = normalizeStorageFormValues(values);
    return (
        candidate.enabled === current.enabled &&
        candidate.provider === current.provider &&
        candidate.region === current.region &&
        candidate.endpoint === current.endpoint &&
        candidate.bucket === current.bucket &&
        candidate.pathPrefix === current.pathPrefix &&
        candidate.accessKeyId === current.accessKeyId &&
        candidate.accessKeySecret === ""
    );
}

export function storageFieldErrors(values: OSSFormValues, hasSavedSecret: boolean): Partial<Record<keyof OSSFormValues, string>> {
    const normalized = normalizeStorageFormValues(values);
    if (!normalized.enabled) return {};

    const errors: Partial<Record<keyof OSSFormValues, string>> = {};
    if (!normalized.endpoint) errors.endpoint = "启用 OSS 时必须填写 Endpoint";
    else if (!isHTTPURL(normalized.endpoint)) errors.endpoint = "Endpoint 必须是有效的 HTTP(S) 地址";
    if (!normalized.bucket) errors.bucket = "启用 OSS 时必须填写 Bucket";
    if (!normalized.accessKeyId) errors.accessKeyId = "启用 OSS 时必须填写 AccessKey ID";
    if (!normalized.accessKeySecret && !hasSavedSecret) errors.accessKeySecret = "首次启用 OSS 时必须填写 AccessKey Secret";
    return errors;
}

function clean(value?: string): string {
    return value?.trim() ?? "";
}

function isHTTPURL(value: string): boolean {
    try {
        const url = new URL(value);
        return url.protocol === "http:" || url.protocol === "https:";
    } catch {
        return false;
    }
}
