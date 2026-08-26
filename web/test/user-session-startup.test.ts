import { expect, test } from "bun:test";

import { needsWorkspaceSession, startUserSession, transitionUserSession } from "../src/lib/user-session-startup";

test("匿名首访不加载工作区，登录与退出都经过工作区边界", () => {
    expect(needsWorkspaceSession(null, null)).toBeFalse();
    expect(needsWorkspaceSession(null, "user-1")).toBeTrue();
    expect(needsWorkspaceSession("user-1", "user-1")).toBeTrue();
    expect(needsWorkspaceSession("user-1", null)).toBeTrue();
});

test("工作区准备成功后才提交新身份", async () => {
    const transitions: string[] = [];

    await transitionUserSession({
        needsWorkspace: true,
        restoreWorkspace: async () => {
            transitions.push("workspace");
        },
        prepareAnonymousScope: () => transitions.push("guest-scope"),
        commitIdentity: () => transitions.push("identity"),
        commitRuntimeLimits: () => transitions.push("limits"),
        onFailure: (error) => transitions.push(`failure:${error instanceof Error ? error.message : "unknown"}`),
        setHydrated: (hydrated) => transitions.push(`hydrated:${hydrated}`),
    });

    expect(transitions).toEqual(["hydrated:false", "workspace", "identity", "limits", "hydrated:true"]);
});

test("匿名首访先切到 guest 作用域再提交匿名身份", async () => {
    const transitions: string[] = [];

    await transitionUserSession({
        needsWorkspace: false,
        restoreWorkspace: async () => transitions.push("workspace"),
        prepareAnonymousScope: () => transitions.push("guest-scope"),
        commitIdentity: () => transitions.push("identity"),
        commitRuntimeLimits: () => transitions.push("limits"),
        onFailure: (error) => transitions.push(`failure:${error instanceof Error ? error.message : "unknown"}`),
        setHydrated: (hydrated) => transitions.push(`hydrated:${hydrated}`),
    });

    expect(transitions).toEqual(["hydrated:false", "guest-scope", "identity", "limits", "hydrated:true"]);
});

test("工作区准备失败时不提交身份或运行限制", async () => {
    const transitions: string[] = [];

    await expect(
        transitionUserSession({
            needsWorkspace: true,
            restoreWorkspace: async () => {
                transitions.push("workspace");
                throw new Error("workspace unavailable");
            },
            prepareAnonymousScope: () => transitions.push("guest-scope"),
            commitIdentity: () => transitions.push("identity"),
            commitRuntimeLimits: () => transitions.push("limits"),
            onFailure: (error) => transitions.push(`failure:${error instanceof Error ? error.message : "unknown"}`),
            setHydrated: (hydrated) => transitions.push(`hydrated:${hydrated}`),
        }),
    ).rejects.toThrow("workspace unavailable");

    expect(transitions).toEqual(["hydrated:false", "workspace", "failure:workspace unavailable", "hydrated:true"]);
});

test("本地会话恢复完成后立即解除首屏阻塞，不等待远端数据同步", async () => {
    let localSessionReady = false;
    let remoteSyncStarted = false;
    let releaseRemoteSync: (() => void) | null = null;
    const remoteSync = new Promise<void>((resolve) => {
        releaseRemoteSync = resolve;
    });

    await startUserSession({
        restoreLocalSession: async () => {
            localSessionReady = true;
        },
        startRemoteSync: () => {
            remoteSyncStarted = true;
            return remoteSync;
        },
        onRemoteSyncError: () => undefined,
    });

    expect(localSessionReady).toBeTrue();
    expect(remoteSyncStarted).toBeTrue();
    releaseRemoteSync?.();
    await remoteSync;
});

test("后台同步失败交给显式错误处理而不回退为首屏阻塞", async () => {
    const failure = new Error("远端素材读取失败");
    const reported: unknown[] = [];

    await startUserSession({
        restoreLocalSession: async () => undefined,
        startRemoteSync: async () => {
            throw failure;
        },
        onRemoteSyncError: (error) => reported.push(error),
    });
    await Promise.resolve();

    expect(reported).toEqual([failure]);
});
