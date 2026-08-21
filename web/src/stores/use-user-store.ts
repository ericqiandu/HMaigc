import { create } from "zustand";

import { personalWorkspaceScope, readWorkspaceScope, writeWorkspaceScope, type WorkspaceScope } from "@/lib/workspace-scope";

export type LocalUser = {
    id: string;
    publicId: number;
    username: string;
    email?: string;
    displayName: string;
    avatarUrl?: string;
    identityProvider?: string;
    identityId?: string;
    identityUsername?: string;
    role: "admin" | "user";
    status: "active" | "disabled";
    lastLoginAt?: string;
    createdAt?: string;
    updatedAt?: string;
};

export type RuntimeLimits = {
    activeTaskLimit: number;
    resourceUploadMB: number;
    sessionUploadMB: number;
};

type UserStore = {
    hydrated: boolean;
    user: LocalUser | null;
    workspaceScope: WorkspaceScope;
    runtimeLimits: RuntimeLimits;
    setUser: (user: LocalUser | null) => void;
    selectWorkspaceScope: (scope: WorkspaceScope) => void;
    setRuntimeLimits: (limits?: RuntimeLimits) => void;
    setHydrated: (hydrated: boolean) => void;
    clearSession: () => void;
};

export const useUserStore = create<UserStore>()((set) => ({
    hydrated: false,
    user: null,
    workspaceScope: personalWorkspaceScope,
    runtimeLimits: { activeTaskLimit: 5, resourceUploadMB: 50, sessionUploadMB: 32 },
    setUser: (user) => set({ user, workspaceScope: user ? readWorkspaceScope(user.id) : personalWorkspaceScope }),
    selectWorkspaceScope: (workspaceScope) =>
        set((state) => {
            if (state.user) writeWorkspaceScope(state.user.id, workspaceScope);
            return { workspaceScope };
        }),
    setRuntimeLimits: (runtimeLimits) => set({ runtimeLimits: runtimeLimits || { activeTaskLimit: 5, resourceUploadMB: 50, sessionUploadMB: 32 } }),
    setHydrated: (hydrated) => set({ hydrated }),
    clearSession: () => set({ user: null, workspaceScope: personalWorkspaceScope, runtimeLimits: { activeTaskLimit: 5, resourceUploadMB: 50, sessionUploadMB: 32 } }),
}));
