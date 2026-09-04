import { create } from "zustand";
import { persist, type PersistStorage, type StorageValue } from "zustand/middleware";

import { nanoid } from "nanoid";
import { localForageStorage } from "@/lib/localforage-storage";
import { createCanvasPersistenceScheduler } from "@/stores/canvas/canvas-persistence-scheduler";
import type { CanvasBackgroundMode } from "@/lib/canvas-theme";
import type { CanvasAgentLaunchRequest, CanvasAssistantSession, CanvasConnection, CanvasNodeData, ViewportTransform } from "@/types/canvas";
import type { DirectorScene } from "@/types/director";

export type CanvasProject = {
    id: string;
    ownerUserId?: string;
    teamId?: string;
    projectId?: string;
    title: string;
    createdAt: string;
    updatedAt: string;
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
    chatSessions: CanvasAssistantSession[];
    activeChatId: string | null;
    pendingAgentLaunch?: CanvasAgentLaunchRequest;
    backgroundMode: CanvasBackgroundMode;
    showImageInfo: boolean;
    viewport: ViewportTransform;
    directorScenes: DirectorScene[];
    revision?: number;
    defaultTeamAccess?: "viewer" | "editor";
    accessLevel?: "viewer" | "editor" | "manager";
    canEdit?: boolean;
    canManage?: boolean;
    teamSubscriptionActive?: boolean;
};

type CanvasStore = {
    hydrated: boolean;
    projects: CanvasProject[];
    pendingDeletionIds: string[];
    createProject: (title?: string, projectId?: string) => string;
    importProject: (project: Partial<CanvasProject>) => string;
    openProject: (id: string) => CanvasProject | null;
    renameProject: (id: string, title: string) => void;
    stageProjectDeletions: (ids: string[]) => void;
    cancelProjectDeletions: (ids: string[]) => void;
    finishProjectDeletions: (ids: string[]) => void;
    replaceProjects: (projects: CanvasProject[]) => void;
    updateProject: (id: string, patch: Partial<Pick<CanvasProject, "ownerUserId" | "teamId" | "projectId" | "title" | "nodes" | "connections" | "chatSessions" | "activeChatId" | "pendingAgentLaunch" | "backgroundMode" | "showImageInfo" | "viewport" | "directorScenes" | "revision" | "defaultTeamAccess" | "accessLevel" | "canEdit" | "canManage" | "teamSubscriptionActive">>) => void;
};

const initialViewport: ViewportTransform = { x: 0, y: 0, k: 1 };
export const CANVAS_STORE_KEY = "infinite-canvas:canvas_store";
type PersistedCanvasState = Pick<CanvasStore, "projects" | "pendingDeletionIds">;
let lastPersistedState: PersistedCanvasState | null = null;

const canvasStorePersistenceScheduler = createCanvasPersistenceScheduler<StorageValue<CanvasStore>>({
    delayMs: 400,
    serialize: (value) => {
        const payload = JSON.stringify(value);
        if (payload === undefined) throw new Error("画布本地状态无法序列化");
        return payload;
    },
    write: async (name, payload) => {
        await localForageStorage.setItem(name, payload);
    },
});

const canvasStorage: PersistStorage<CanvasStore> = {
    getItem: async (name) => {
        const value = await localForageStorage.getItem(name);
        if (!value) return null;
        const parsed = JSON.parse(value) as StorageValue<CanvasStore>;
        lastPersistedState = parsed.state as PersistedCanvasState;
        return parsed;
    },
    setItem: (name, value) => {
        const nextState = value.state as PersistedCanvasState;
        if (lastPersistedState && lastPersistedState.projects === nextState.projects && lastPersistedState.pendingDeletionIds === nextState.pendingDeletionIds) return;
        lastPersistedState = nextState;
        canvasStorePersistenceScheduler.schedule(name, value);
    },
    removeItem: async (name) => {
        await canvasStorePersistenceScheduler.flush();
        lastPersistedState = null;
        await localForageStorage.removeItem(name);
    },
};

export async function flushCanvasStorePersistence() {
    await canvasStorePersistenceScheduler.flush();
}

export function getCanvasStorePersistenceSnapshot() {
    return canvasStorePersistenceScheduler.getSnapshot();
}

export async function retryCanvasStorePersistence() {
    await canvasStorePersistenceScheduler.retry();
}

export const useCanvasStore = create<CanvasStore>()(
    persist(
        (set, get) => ({
            hydrated: false,
            projects: [],
            pendingDeletionIds: [],
            createProject: (title = "未命名画布", projectId) => {
                const now = new Date().toISOString();
                const id = nanoid();
                const project: CanvasProject = {
                    id,
                    projectId,
                    title,
                    createdAt: now,
                    updatedAt: now,
                    nodes: [],
                    connections: [],
                    chatSessions: [],
                    activeChatId: null,
                    backgroundMode: "lines",
                    showImageInfo: false,
                    viewport: initialViewport,
                    directorScenes: [],
                };
                set((state) => ({ projects: [project, ...state.projects] }));
                return id;
            },
            importProject: (source) => {
                const now = new Date().toISOString();
                const project: CanvasProject = {
                    id: nanoid(),
                    projectId: source.projectId,
                    title: source.title || "导入画布",
                    createdAt: source.createdAt || now,
                    updatedAt: now,
                    nodes: source.nodes || [],
                    connections: source.connections || [],
                    chatSessions: source.chatSessions || [],
                    activeChatId: source.activeChatId || null,
                    pendingAgentLaunch: source.pendingAgentLaunch,
                    backgroundMode: source.backgroundMode || "lines",
                    showImageInfo: source.showImageInfo || false,
                    viewport: source.viewport || initialViewport,
                    directorScenes: source.directorScenes || [],
                };
                set((state) => ({ projects: [project, ...state.projects] }));
                return project.id;
            },
            openProject: (id) => get().projects.find((item) => item.id === id) || null,
            renameProject: (id, title) =>
                set((state) => ({
                    projects: state.projects.map((project) => (project.id === id ? { ...project, title: title.trim() || project.title, updatedAt: new Date().toISOString() } : project)),
                })),
            stageProjectDeletions: (ids) => set((state) => ({ pendingDeletionIds: [...new Set([...state.pendingDeletionIds, ...ids])] })),
            cancelProjectDeletions: (ids) => {
                const cancelledIds = new Set(ids);
                set((state) => ({ pendingDeletionIds: state.pendingDeletionIds.filter((id) => !cancelledIds.has(id)) }));
            },
            finishProjectDeletions: (ids) => {
                const finishedIds = new Set(ids);
                set((state) => ({
                    projects: state.projects.filter((project) => !finishedIds.has(project.id)),
                    pendingDeletionIds: state.pendingDeletionIds.filter((id) => !finishedIds.has(id)),
                }));
            },
            replaceProjects: (projects) => set({ projects }),
            updateProject: (id, patch) =>
                set((state) => ({
                    projects: state.projects.map((project) => (project.id === id ? { ...project, ...patch, updatedAt: new Date().toISOString() } : project)),
                })),
        }),
        {
            name: CANVAS_STORE_KEY,
            storage: canvasStorage,
            partialize: (state) =>
                ({
                    projects: state.projects,
                    pendingDeletionIds: state.pendingDeletionIds,
                }) as StorageValue<CanvasStore>["state"],
            onRehydrateStorage: () => () => {
                const projects = useCanvasStore.getState().projects;
                const pendingDeletionIds = useCanvasStore.getState().pendingDeletionIds || [];
                useCanvasStore.setState({ projects, pendingDeletionIds, hydrated: true });
            },
        },
    ),
);
