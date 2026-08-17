export type CanvasProjectDeletionTarget = {
    id: string;
    requiresRemoteDelete: boolean;
    canManage: boolean;
};

export type CanvasProjectDeletionFailure = {
    id: string;
    reason: string;
};

export type CanvasProjectDeletionResult = {
    deletedIds: string[];
    failures: CanvasProjectDeletionFailure[];
};

export type CanvasProjectDeletionDependencies = {
    resolveTarget: (id: string) => CanvasProjectDeletionTarget | null;
    hasRemoteSession: () => boolean;
    isRemoteDeleteStaged: (id: string) => boolean;
    stageRemoteDelete: (ids: string[]) => Promise<void>;
    cancelRemoteDelete: (ids: string[]) => Promise<void>;
    waitForRemoteWrites: () => Promise<void>;
    deleteRemote: (id: string) => Promise<void>;
    deleteLocal: (ids: string[]) => void | Promise<void>;
};

export function createCanvasProjectDeletionService(dependencies: CanvasProjectDeletionDependencies) {
    return async (ids: string[]): Promise<CanvasProjectDeletionResult> => {
        const localOnlyIds: string[] = [];
        const remoteTargets: CanvasProjectDeletionTarget[] = [];
        const failures: CanvasProjectDeletionFailure[] = [];

        for (const id of [...new Set(ids)]) {
            const target = dependencies.resolveTarget(id);
            if (!target) {
                failures.push({ id, reason: "画布不存在或已被删除" });
                continue;
            }
            if (!target.canManage) {
                failures.push({ id, reason: "当前用户不能删除该画布" });
                continue;
            }
            if (target.requiresRemoteDelete) {
                if (!dependencies.hasRemoteSession()) {
                    failures.push({ id, reason: "尚未建立云端同步会话" });
                    continue;
                }
                remoteTargets.push(target);
                continue;
            }
            localOnlyIds.push(id);
        }

        const freshRemoteIds = remoteTargets.map((target) => target.id).filter((id) => !dependencies.isRemoteDeleteStaged(id));
        let stagedRemoteIds = remoteTargets.map((target) => target.id);
        if (freshRemoteIds.length > 0) {
            try {
                await dependencies.stageRemoteDelete(freshRemoteIds);
            } catch (error) {
                const reason = deletionErrorMessage(error, "无法持久化画布删除请求");
                failures.push(...freshRemoteIds.map((id) => ({ id, reason })));
                const failedStageIds = new Set(freshRemoteIds);
                stagedRemoteIds = stagedRemoteIds.filter((id) => !failedStageIds.has(id));
            }
        }

        const remoteDeletedIds: string[] = [];
        const remoteFailedIds: string[] = [];
        if (stagedRemoteIds.length > 0) {
            try {
                await dependencies.waitForRemoteWrites();
            } catch (error) {
                const reason = deletionErrorMessage(error, "等待云端同步写入完成失败");
                failures.push(...stagedRemoteIds.map((id) => ({ id, reason })));
                return { deletedIds: [], failures };
            }
        }
        for (const id of stagedRemoteIds) {
            try {
                await dependencies.deleteRemote(id);
                remoteDeletedIds.push(id);
            } catch (error) {
                if (isAlreadyDeleted(error)) {
                    remoteDeletedIds.push(id);
                    continue;
                }
                remoteFailedIds.push(id);
                failures.push({ id, reason: deletionErrorMessage(error) });
            }
        }

        if (remoteFailedIds.length > 0) {
            try {
                await dependencies.cancelRemoteDelete(remoteFailedIds);
            } catch (error) {
                const reason = deletionErrorMessage(error, "无法撤销画布删除请求");
                for (const failure of failures) {
                    if (remoteFailedIds.includes(failure.id)) failure.reason = `${failure.reason}；${reason}`;
                }
            }
        }

        const completedIds = [...localOnlyIds, ...remoteDeletedIds];
        if (completedIds.length === 0) return { deletedIds: [], failures };
        try {
            await dependencies.deleteLocal(completedIds);
            return { deletedIds: completedIds, failures };
        } catch (error) {
            const reason = deletionErrorMessage(error, "本地画布删除状态保存失败");
            failures.push(...completedIds.map((id) => ({ id, reason })));
            return { deletedIds: [], failures };
        }
    };
}

function isAlreadyDeleted(error: unknown): error is Error & { status: 404 } {
    return error instanceof Error && "status" in error && error.status === 404;
}

function deletionErrorMessage(error: unknown, fallback = "云端删除失败") {
    if (error instanceof Error && error.message.trim()) return error.message;
    return fallback;
}
