import axios from "axios";

import { compactApiParams, serializeApiParams, type ApiParams } from "@/services/api/request";
import type { BackendEnvelope } from "@/services/api/task-center";

const apiBaseURL = import.meta.env.VITE_CANVAS_BACKEND_URL || "/api";
const api = axios.create({ baseURL: apiBaseURL, withCredentials: true });

export type PlatformSkill = {
    dir: string;
    name: string;
    description: string;
    icon: string;
    cover_url: string;
    detail_text: string;
    categories: string[];
    version: number;
    checksum: string;
    status: "published";
    source_kind: "original" | "adapted";
    source_license: string;
    published_at: string;
    uploader_name: string;
    liked: boolean;
    activated: boolean;
};

export type SkillCatalog = {
    skills: PlatformSkill[];
    total: number;
    page: number;
    page_size: number;
    categories: string[];
};

export type SkillIntegrationCapabilities = {
    provider: "first_party";
    publicCatalog: boolean;
    categoryFilter: boolean;
    versioned: boolean;
    adminPublishing: boolean;
    upload: boolean;
};

export type ListSkillsInput = {
    page?: number;
    page_size?: number;
    search?: string;
    categories?: string[];
};

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>) {
    const response = await promise;
    if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
    return response.data.data;
}

export function listSkillsCatalog(input: ListSkillsInput = {}) {
    const params = serializeApiParams(compactApiParams(input as ApiParams));
    return request<SkillCatalog>(api.get(`/skills/catalog?${params.toString()}`));
}

export function getSkillIntegrationCapabilities() {
    return request<{ capabilities: SkillIntegrationCapabilities }>(api.get("/skills/capabilities"));
}

export function getSkill(dir: string) {
    return request<{ skill: PlatformSkill }>(api.get(`/skills/catalog/${encodeURIComponent(dir)}`));
}

export function listActivatedSkills() {
    return request<{ skills: PlatformSkill[] }>(api.get("/skills/activated"));
}

export function listFavoriteSkills() {
    return request<{ skills: PlatformSkill[] }>(api.get("/skills/favorites"));
}

export function activateSkill(dir: string) {
    return request<{ skill: PlatformSkill }>(api.post(`/skills/${encodeURIComponent(dir)}/activate`));
}

export function deactivateSkill(dir: string) {
    return request<{ skill: PlatformSkill }>(api.delete(`/skills/${encodeURIComponent(dir)}/activate`));
}

export function favoriteSkill(dir: string) {
    return request<{ skill: PlatformSkill }>(api.post(`/skills/${encodeURIComponent(dir)}/favorite`));
}

export function unfavoriteSkill(dir: string) {
    return request<{ skill: PlatformSkill }>(api.delete(`/skills/${encodeURIComponent(dir)}/favorite`));
}

export function skillImageUrl(value?: string) {
    return value?.trim() || "";
}
