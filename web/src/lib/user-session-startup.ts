export type UserSessionStartup = {
    restoreLocalSession: () => Promise<void>;
    startRemoteSync?: () => Promise<void>;
    onRemoteSyncError: (error: unknown) => void;
};

export async function startUserSession(startup: UserSessionStartup) {
    await startup.restoreLocalSession();
    if (!startup.startRemoteSync) return;
    void startup.startRemoteSync().catch(startup.onRemoteSyncError);
}
