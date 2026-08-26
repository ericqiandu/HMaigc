export type UserSessionStartup = {
    restoreLocalSession: () => Promise<void>;
    startRemoteSync?: () => Promise<void>;
    onRemoteSyncError: (error: unknown) => void;
};

export type UserSessionTransition = {
    needsWorkspace: boolean;
    restoreWorkspace: () => Promise<void>;
    prepareAnonymousScope: () => void;
    commitIdentity: () => void;
    commitRuntimeLimits: () => void;
    onFailure: (error: unknown) => Promise<void> | void;
    setHydrated: (hydrated: boolean) => void;
};

export function needsWorkspaceSession(previousUserId: string | null, nextUserId: string | null) {
    return Boolean(previousUserId || nextUserId);
}

export async function transitionUserSession(transition: UserSessionTransition) {
    transition.setHydrated(false);
    try {
        if (transition.needsWorkspace) {
            await transition.restoreWorkspace();
        } else {
            transition.prepareAnonymousScope();
        }
        transition.commitIdentity();
        transition.commitRuntimeLimits();
    } catch (error: unknown) {
        await transition.onFailure(error);
        throw error;
    } finally {
        transition.setHydrated(true);
    }
}

export async function startUserSession(startup: UserSessionStartup) {
    await startup.restoreLocalSession();
    if (!startup.startRemoteSync) return;
    void startup.startRemoteSync().catch(startup.onRemoteSyncError);
}
