import "./setup-happy-dom";

import { afterEach, expect, mock, test } from "bun:test";

const upsertedAssetIds: string[] = [];
const remoteAssetSummary = {
    id: "asset-1",
    kind: "text",
    title: "测试素材",
    createdAt: "2026-08-18T23:34:43.41Z",
    updatedAt: "2026-08-18T23:34:43.41Z",
};
let listRemoteAssetsImpl = async () => ({ assets: [remoteAssetSummary] });
let getRemoteAssetImpl = async () => {
    throw new Error("相同版本不应读取素材详情");
};

class MockUserDataRequestError extends Error {
    constructor(
        message: string,
        readonly status?: number,
    ) {
        super(message);
        this.name = "UserDataRequestError";
    }
}

mock.module("@/services/api/user-data", () => ({
    UserDataRequestError: MockUserDataRequestError,
    createRemoteCanvasProject: async () => undefined,
    deleteRemoteAsset: async () => undefined,
    deleteRemoteCanvasProject: async () => undefined,
    getRemoteAsset: () => getRemoteAssetImpl(),
    getRemoteCanvasProject: async () => {
        throw new Error("测试未配置远端画布");
    },
    listRemoteAssets: () => listRemoteAssetsImpl(),
    listRemoteCanvasProjects: async () => ({ projects: [], deletions: [] }),
    upsertRemoteAsset: async (asset: { id: string }) => {
        upsertedAssetIds.push(asset.id);
        return { asset };
    },
}));

afterEach(async () => {
    const { resetRemoteUserDataSync } = await import("../src/services/user-data-sync");
    resetRemoteUserDataSync();
    upsertedAssetIds.length = 0;
    listRemoteAssetsImpl = async () => ({ assets: [remoteAssetSummary] });
    getRemoteAssetImpl = async () => {
        throw new Error("相同版本不应读取素材详情");
    };
});

test("同一时刻的不同 RFC3339 精度不触发素材重复写回", async () => {
    const [{ syncRemoteUserData }, { useAssetStore }, { useCanvasStore }] = await Promise.all([import("../src/services/user-data-sync"), import("../src/stores/use-asset-store"), import("../src/stores/canvas/use-canvas-store")]);
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({
        assets: [
            {
                id: "asset-1",
                kind: "text",
                title: "测试素材",
                coverUrl: "",
                tags: [],
                createdAt: "2026-08-18T23:34:43.410Z",
                updatedAt: "2026-08-18T23:34:43.410Z",
                data: { content: "测试内容" },
            },
        ],
    });

    await syncRemoteUserData("user-1");

    expect(upsertedAssetIds).toEqual([]);
});

test("已退出的旧会话不会把迟到的远端素材写入当前用户状态", async () => {
    let releaseRemoteAssets: (() => void) | undefined;
    let markRemoteAssetsStarted: (() => void) | undefined;
    const remoteAssetsStarted = new Promise<void>((resolve) => {
        markRemoteAssetsStarted = resolve;
    });
    listRemoteAssetsImpl = () =>
        new Promise((resolve) => {
            markRemoteAssetsStarted?.();
            releaseRemoteAssets = () => resolve({ assets: [remoteAssetSummary] });
        });
    getRemoteAssetImpl = async () => ({
        asset: {
            id: remoteAssetSummary.id,
            kind: "text" as const,
            title: remoteAssetSummary.title,
            coverUrl: "",
            tags: [],
            createdAt: remoteAssetSummary.createdAt,
            updatedAt: remoteAssetSummary.updatedAt,
            data: { content: "旧用户内容" },
        },
    });
    const [{ resetRemoteUserDataSync, syncRemoteUserData }, { useAssetStore }, { useCanvasStore }] = await Promise.all([import("../src/services/user-data-sync"), import("../src/stores/use-asset-store"), import("../src/stores/canvas/use-canvas-store")]);
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({ assets: [] });

    const staleSync = syncRemoteUserData("old-user");
    await remoteAssetsStarted;
    resetRemoteUserDataSync();
    useAssetStore.setState({
        assets: [
            {
                id: "current-user-asset",
                kind: "text",
                title: "当前用户素材",
                coverUrl: "",
                tags: [],
                createdAt: "2026-08-20T00:00:00.000Z",
                updatedAt: "2026-08-20T00:00:00.000Z",
                data: { content: "当前用户内容" },
            },
        ],
    });
    releaseRemoteAssets?.();
    await staleSync;

    expect(useAssetStore.getState().assets.map((asset) => asset.id)).toEqual(["current-user-asset"]);
});

test("旧会话退出后不会继续提交尚未开始的远端素材写入", async () => {
    listRemoteAssetsImpl = async () => ({ assets: [] });
    const [{ resetRemoteUserDataSync, saveRemoteUserDataNow, syncRemoteUserData }, { useAssetStore }, { useCanvasStore }] = await Promise.all([
        import("../src/services/user-data-sync"),
        import("../src/stores/use-asset-store"),
        import("../src/stores/canvas/use-canvas-store"),
    ]);
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({ assets: [] });
    await syncRemoteUserData("old-user");
    useAssetStore.setState({
        assets: [
            {
                id: "stale-write-asset",
                kind: "text",
                title: "旧会话待写素材",
                coverUrl: "",
                tags: [],
                createdAt: "2026-08-20T00:00:00.000Z",
                updatedAt: "2026-08-20T00:00:00.000Z",
                data: { content: "不应写入" },
            },
        ],
    });

    const staleWrite = saveRemoteUserDataNow();
    resetRemoteUserDataSync();
    await staleWrite;

    expect(upsertedAssetIds).toEqual([]);
});
