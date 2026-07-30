import axios from "axios";

import type { ChannelVoice } from "@/stores/use-config-store";

const api = axios.create({ baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api", withCredentials: true });

type BackendEnvelope<T> = { code: number; data: T; msg: string };

export type ChannelVoiceInput = {
    voiceKey: string;
    displayName: string;
    description: string;
    language: string;
    kind: ChannelVoice["kind"];
    accessPolicy: ChannelVoice["accessPolicy"];
    compatibleModels: string[];
    enabled: boolean;
};

export type ChannelVoicePreview = {
    audioDataUrl: string;
    mimeType: string;
    sha256: string;
    traceId?: string;
    cached: boolean;
};

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) {
            throw new Error(response.data.msg || "音色管理请求失败");
        }
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) {
            throw new Error(error.response?.data?.msg || error.message || "音色管理请求失败");
        }
        throw error;
    }
}

export function listAdminChannelVoices(channelId: string) {
    return request<{ voices: ChannelVoice[] }>(api.get(`/admin/channels/${encodeURIComponent(channelId)}/voices`));
}

export function syncAdminChannelVoices(channelId: string) {
    return request<{ voices: ChannelVoice[] }>(api.post(`/admin/channels/${encodeURIComponent(channelId)}/voices/sync`));
}

export function createAdminChannelVoice(channelId: string, input: ChannelVoiceInput) {
    return request<{ voice: ChannelVoice }>(api.post(`/admin/channels/${encodeURIComponent(channelId)}/voices`, input));
}

export function updateAdminChannelVoice(channelId: string, voiceId: string, input: ChannelVoiceInput) {
    return request<{ voice: ChannelVoice }>(api.patch(`/admin/channels/${encodeURIComponent(channelId)}/voices/${encodeURIComponent(voiceId)}`, input));
}

export function cloneAdminChannelVoice(
    channelId: string,
    input: ChannelVoiceInput & {
        file: File;
        consentConfirmed: boolean;
        idempotencyKey: string;
    },
) {
    const form = new FormData();
    form.append("file", input.file);
    form.append("voiceKey", input.voiceKey);
    form.append("displayName", input.displayName);
    form.append("description", input.description);
    form.append("language", input.language);
    form.append("accessPolicy", input.accessPolicy);
    form.append("compatibleModels", JSON.stringify(input.compatibleModels));
    form.append("consentConfirmed", String(input.consentConfirmed));
    form.append("idempotencyKey", input.idempotencyKey);
    return request<{ voice: ChannelVoice }>(api.post(`/admin/channels/${encodeURIComponent(channelId)}/voices/clone`, form));
}

export function deleteAdminChannelVoice(channelId: string, voiceId: string) {
    return request<{ ok: boolean }>(api.delete(`/admin/channels/${encodeURIComponent(channelId)}/voices/${encodeURIComponent(voiceId)}`));
}

export function previewChannelVoice(channelId: string, voiceId: string, model: string, signal?: AbortSignal) {
    return request<{ preview: ChannelVoicePreview }>(api.post(`/channels/${encodeURIComponent(channelId)}/voices/${encodeURIComponent(voiceId)}/preview`, { model }, { signal }));
}

export function listUserChannelVoices(channelId: string) {
    return request<{ voices: ChannelVoice[] }>(api.get(`/channels/${encodeURIComponent(channelId)}/voices`));
}

export function setChannelVoiceFavorite(channelId: string, voiceId: string, favorite: boolean) {
    return request<{ voice: ChannelVoice }>(api.put(`/channels/${encodeURIComponent(channelId)}/voices/${encodeURIComponent(voiceId)}/favorite`, { favorite }));
}

export function cloneUserChannelVoice(
    channelId: string,
    input: {
        file: File;
        displayName?: string;
        language?: string;
        consentConfirmed: boolean;
        idempotencyKey: string;
    },
) {
    const form = new FormData();
    form.append("file", input.file);
    if (input.displayName) form.append("displayName", input.displayName);
    if (input.language) form.append("language", input.language);
    form.append("consentConfirmed", String(input.consentConfirmed));
    form.append("idempotencyKey", input.idempotencyKey);
    return request<{ voice: ChannelVoice }>(api.post(`/channels/${encodeURIComponent(channelId)}/voices/clone`, form));
}
