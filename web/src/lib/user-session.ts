import type { AuthSessionPayload } from "@/services/api/auth";
import { needsWorkspaceSession, transitionUserSession } from "@/lib/user-session-startup";
import { setActiveUserScope } from "@/lib/user-scope";
import { useUserStore } from "@/stores/use-user-store";

export async function applyUserSession(payload: AuthSessionPayload) {
    const userStore = useUserStore.getState();
    const previousUserId = userStore.user?.id ?? null;
    const nextUserId = payload.user?.id ?? null;

    await transitionUserSession({
        needsWorkspace: needsWorkspaceSession(previousUserId, nextUserId),
        restoreWorkspace: async () => {
            const { applyWorkspaceSession } = await import("@/lib/user-workspace-session");
            await applyWorkspaceSession(payload);
        },
        prepareAnonymousScope: () => setActiveUserScope(null),
        commitIdentity: () => useUserStore.getState().setUser(payload.user),
        commitRuntimeLimits: () => useUserStore.getState().setRuntimeLimits(payload.runtimeLimits),
        onFailure: async (sessionError) => {
            setActiveUserScope(null);
            useUserStore.getState().clearSession();
            try {
                const { clearWorkspaceSessionMemory } = await import("@/lib/user-workspace-session");
                clearWorkspaceSessionMemory();
            } catch (cleanupError: unknown) {
                throw new AggregateError([sessionError, cleanupError], "用户工作区会话恢复及隔离清理失败");
            }
        },
        setHydrated: (hydrated) => useUserStore.getState().setHydrated(hydrated),
    });
}

export async function refreshSystemChannels() {
    // 系统模型由后端统一维护，后台变更后只刷新这一层，避免重跑整套用户数据同步。
    const [{ getSystemChannels }, { useConfigStore }] = await Promise.all([import("@/services/api/auth"), import("@/stores/use-config-store")]);
    const payload = await getSystemChannels();
    useConfigStore.getState().mergeSystemChannels(payload.channels || [], payload.agentDefaultModel);
}
