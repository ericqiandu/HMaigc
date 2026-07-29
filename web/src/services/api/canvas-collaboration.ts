import axios from "axios";

import type { CanvasBackgroundMode } from "@/lib/canvas-theme";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { CanvasConnection, CanvasNodeData } from "@/types/canvas";
import type { DirectorScene } from "@/types/director";
import type { BackendEnvelope } from "@/services/api/task-center";
import type { TeamMember, TeamRole } from "@/services/api/teams";

const apiBaseURL = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api").replace(/\/+$/, "");
const api = axios.create({ baseURL: apiBaseURL, withCredentials: true });

export type CanvasAccessLevel = "viewer" | "editor" | "manager";

export type CanvasAccess = {
    level: CanvasAccessLevel;
    teamId?: string;
    teamName?: string;
    teamRole?: TeamRole;
    canEdit: boolean;
    canManage: boolean;
    teamSubscriptionActive: boolean;
};

export type CanvasCollaborator = {
    id: string;
    canvasId: string;
    teamId: string;
    userId: string;
    access: Exclude<CanvasAccessLevel, "manager">;
    createdBy: string;
    username: string;
    displayName: string;
    avatarUrl?: string;
    teamRole: TeamRole;
    teamStatus: "active" | "removed";
    createdAt: string;
    updatedAt: string;
};

export type CanvasCollaborationState = {
    project: CanvasProject;
    access: CanvasAccess;
    collaborators: CanvasCollaborator[];
    teamMembers: TeamMember[];
};

export type CanvasDocumentPatch = {
    title?: string;
    backgroundMode?: CanvasBackgroundMode;
    showImageInfo?: boolean;
    directorScenes?: DirectorScene[];
};

export type CanvasMutationPatch = {
    upsertNodes?: CanvasNodeData[];
    deleteNodeIds?: string[];
    upsertConnections?: CanvasConnection[];
    deleteConnectionIds?: string[];
    document?: CanvasDocumentPatch;
};

export type CanvasMutationRequest = {
    baseRevision: number;
    clientMutationId: string;
    patch: CanvasMutationPatch;
};

export type CanvasMutationResult = {
    canvasId: string;
    revision: number;
    actorUserId: string;
    clientMutationId: string;
    patch: CanvasMutationPatch;
    updatedAt: string;
};

export type CanvasPresence = {
    connectionId: string;
    userId: string;
    displayName: string;
    avatarUrl?: string;
    cursor?: { x: number; y: number };
    selectedNodeIds?: string[];
    active: boolean;
    updatedAt: string;
};

export type CanvasRealtimeEnvelope =
    | { type: "snapshot"; canvasId: string; connectionId: string; state: CanvasCollaborationState }
    | { type: "mutation"; canvasId: string; connectionId?: string; mutation: CanvasMutationResult }
    | { type: "presence"; canvasId: string; connectionId: string; presence: CanvasPresence }
    | { type: "error"; canvasId: string; connectionId?: string; error: { status: number; message: string } };

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>) {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) {
            const message = error.response?.data?.msg || error.message || "请求失败";
            const wrapped = new Error(message) as Error & { status?: number };
            wrapped.status = error.response?.status;
            throw wrapped;
        }
        throw error;
    }
}

export function getCanvasCollaboration(canvasId: string) {
    return request<CanvasCollaborationState>(api.get(`/canvas-projects/${encodeURIComponent(canvasId)}/collaboration`));
}

export function configureCanvasCollaboration(canvasId: string, input: { teamId: string; defaultAccess: "viewer" | "editor" }) {
    return request<CanvasCollaborationState>(api.patch(`/canvas-projects/${encodeURIComponent(canvasId)}/collaboration`, input));
}

export function updateCanvasCollaborator(canvasId: string, userId: string, access: "viewer" | "editor") {
    return request<CanvasCollaborationState>(api.put(`/canvas-projects/${encodeURIComponent(canvasId)}/collaborators/${encodeURIComponent(userId)}`, { access }));
}

export function deleteCanvasCollaborator(canvasId: string, userId: string) {
    return request<CanvasCollaborationState>(api.delete(`/canvas-projects/${encodeURIComponent(canvasId)}/collaborators/${encodeURIComponent(userId)}`));
}

export function commitCanvasMutation(canvasId: string, input: CanvasMutationRequest) {
    return request<CanvasMutationResult>(api.post(`/canvas-projects/${encodeURIComponent(canvasId)}/mutations`, input));
}

export function canvasCollaborationSocketURL(canvasId: string) {
    const base = apiBaseURL.startsWith("http://") || apiBaseURL.startsWith("https://")
        ? new URL(apiBaseURL)
        : new URL(apiBaseURL.startsWith("/") ? apiBaseURL : `/${apiBaseURL}`, window.location.origin);
    base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
    base.pathname = `${base.pathname.replace(/\/+$/, "")}/canvas-projects/${encodeURIComponent(canvasId)}/collaboration/socket`;
    base.search = "";
    base.hash = "";
    return base.toString();
}
