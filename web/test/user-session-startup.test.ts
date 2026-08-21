import { expect, test } from "bun:test";

import { startUserSession } from "../src/lib/user-session-startup";

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
