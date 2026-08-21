export const personalWorkspaceScope = { kind: "personal" } as const;

export type WorkspaceScope = typeof personalWorkspaceScope | { kind: "team"; teamId: string };

type WorkspaceProjectIdentity = {
    userId: string;
    teamId?: string;
};

function workspaceStorageKey(userId: string) {
    return `hmaigc:workspace:user:${userId}`;
}

function parseWorkspaceScope(value: unknown): WorkspaceScope | null {
    if (!value || typeof value !== "object") return null;
    const candidate = value as Record<string, unknown>;
    if (candidate.kind === "personal") return personalWorkspaceScope;
    if (candidate.kind === "team" && typeof candidate.teamId === "string" && candidate.teamId.trim()) {
        return { kind: "team", teamId: candidate.teamId.trim() };
    }
    return null;
}

export function readWorkspaceScope(userId: string): WorkspaceScope {
    if (!userId || typeof window === "undefined") return personalWorkspaceScope;
    try {
        const raw = window.localStorage.getItem(workspaceStorageKey(userId));
        if (!raw) return personalWorkspaceScope;
        return parseWorkspaceScope(JSON.parse(raw)) ?? personalWorkspaceScope;
    } catch (error) {
        console.warn("读取工作区偏好失败，当前会话使用个人空间", error);
        return personalWorkspaceScope;
    }
}

export function writeWorkspaceScope(userId: string, scope: WorkspaceScope) {
    if (!userId || typeof window === "undefined") return;
    try {
        window.localStorage.setItem(workspaceStorageKey(userId), JSON.stringify(scope));
    } catch (error) {
        // 工作区选择已经写入内存状态；持久化失败只影响下次登录恢复，不能中断当前会话。
        console.warn("保存工作区偏好失败，本次选择仅在当前会话生效", error);
    }
}

export function projectBelongsToWorkspace(project: WorkspaceProjectIdentity, userId: string, scope: WorkspaceScope) {
    if (scope.kind === "team") return project.teamId === scope.teamId;
    return project.userId === userId && !project.teamId;
}
