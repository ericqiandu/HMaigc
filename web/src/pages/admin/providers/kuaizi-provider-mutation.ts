export type KuaiziMutationScope = "endpoint" | `credential:${string}`;

export type KuaiziAwaitingSyncOperation = {
    phase: "awaiting-sync";
    scope: KuaiziMutationScope;
    mutationError: Error;
    syncError: Error;
};

export type KuaiziProviderOperation = KuaiziMutationScope | KuaiziAwaitingSyncOperation | null;

export function isKuaiziAwaitingSync(operation: KuaiziProviderOperation): operation is KuaiziAwaitingSyncOperation {
    return typeof operation === "object" && operation?.phase === "awaiting-sync";
}

export function createKuaiziAwaitingSync(scope: KuaiziMutationScope, mutationError: Error, syncError: Error): KuaiziAwaitingSyncOperation {
    return { phase: "awaiting-sync", scope, mutationError, syncError };
}

export function kuaiziAwaitingSyncError(operation: KuaiziAwaitingSyncOperation): Error {
    const scope = operation.scope === "endpoint" ? "服务地址" : `${operation.scope.slice("credential:".length)} 凭据`;
    return new Error(`写入结果待同步（${scope}）：${operation.syncError.message}`);
}
