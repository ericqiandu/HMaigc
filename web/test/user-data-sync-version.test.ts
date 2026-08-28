import "./setup-happy-dom";

import { afterEach, expect, mock, test } from "bun:test";

const upsertedAssetIds: string[] = [];
const createdCanvasIds: string[] = [];
const projectAssetEvents: string[] = [];
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
let createRemoteCanvasProjectImpl = async (project: { id: string }) => {
    createdCanvasIds.push(project.id);
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
    createRemoteCanvasProject: (project: { id: string }) => createRemoteCanvasProjectImpl(project),
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
        projectAssetEvents.push(`upsert:${asset.id}`);
        return { asset };
    },
}));

mock.module("@/services/api/projects", () => ({
    linkProjectAsset: async (projectId: string, input: { assetId: string; category: string }) => {
        projectAssetEvents.push(`link:${projectId}:${input.assetId}`);
        return {
            asset: {
                id: input.assetId,
                title: "画布图片",
                mediaType: "image",
                category: input.category,
                status: "confirmed",
                versionCount: 1,
                usages: [],
                updatedAt: "2026-08-28T00:00:00.000Z",
            },
        };
    },
    updateProjectAssetCategory: async (projectId: string, assetId: string, category: string) => ({
        asset: {
            id: assetId,
            title: "画布图片",
            mediaType: "image",
            category,
            status: "confirmed",
            versionCount: 1,
            usages: [],
            updatedAt: "2026-08-28T00:00:00.000Z",
        },
    }),
}));

afterEach(async () => {
    const { resetRemoteUserDataSync } = await import("../src/services/user-data-sync");
    resetRemoteUserDataSync();
    upsertedAssetIds.length = 0;
    createdCanvasIds.length = 0;
    projectAssetEvents.length = 0;
    createRemoteCanvasProjectImpl = async (project: { id: string }) => {
        createdCanvasIds.push(project.id);
    };
    listRemoteAssetsImpl = async () => ({ assets: [remoteAssetSummary] });
    getRemoteAssetImpl = async () => {
        throw new Error("相同版本不应读取素材详情");
    };
});

test("画布上传只定向写入当前素材后再建立项目关联", async () => {
    const [{ ensureCanvasNodeAsset }, { syncRemoteUserData }, { useAssetStore }, { useCanvasStore }, { CanvasNodeType }] = await Promise.all([
        import("../src/services/project-asset-sync"),
        import("../src/services/user-data-sync"),
        import("../src/stores/use-asset-store"),
        import("../src/stores/canvas/use-canvas-store"),
        import("../src/types/canvas"),
    ]);
    listRemoteAssetsImpl = async () => ({ assets: [] });
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({ assets: [] });
    await syncRemoteUserData("user-1");
    useAssetStore.setState({
        assets: [{
            id: "unrelated-dirty-asset",
            kind: "text",
            title: "无关待同步素材",
            coverUrl: "",
            tags: [],
            createdAt: "2026-08-28T00:00:00.000Z",
            updatedAt: "2026-08-28T00:00:00.000Z",
            data: { content: "不应由本次上传扫描写入" },
        }],
    });

    const result = await ensureCanvasNodeAsset({
        canvasId: "canvas-1",
        domainProjectId: "project-1",
        source: "canvas-upload",
        node: {
            id: "uploaded-node",
            type: CanvasNodeType.Image,
            title: "上传图片",
            position: { x: 0, y: 0 },
            width: 320,
            height: 180,
            metadata: {
                content: "/api/resources/uploaded-resource/file?direct=1",
                storageKey: "resource:uploaded-resource",
            },
        },
    });

    expect(upsertedAssetIds).not.toContain("unrelated-dirty-asset");
    expect(projectAssetEvents[0]).toBe(`upsert:${result.assetId}`);
    expect(projectAssetEvents).toContain(`link:project-1:${result.assetId}`);
});

test("上传完成不等待项目查询失效请求", async () => {
    const source = await Bun.file(new URL("../src/pages/canvas/use-canvas-upload.ts", import.meta.url)).text();

    expect(source).not.toContain('await queryClient.invalidateQueries({ queryKey: ["project", domainProjectId] })');
    expect(source).toContain("项目资产查询刷新失败");
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

test("首页 Agent 创建立即返回本地画布，并只等待该画布的定向远端创建", async () => {
    let releaseRemoteCreate: (() => void) | undefined;
    const remoteCreateGate = new Promise<void>((resolve) => {
        releaseRemoteCreate = resolve;
    });
    createRemoteCanvasProjectImpl = async (project) => {
        createdCanvasIds.push(project.id);
        await remoteCreateGate;
    };
    const [{ createAgentCanvasProjectWithRemoteSync, ensureRemoteCanvasProjectReady, syncRemoteUserData }, { useAssetStore }, { useCanvasStore }] = await Promise.all([
        import("../src/services/user-data-sync"),
        import("../src/stores/use-asset-store"),
        import("../src/stores/canvas/use-canvas-store"),
    ]);
    listRemoteAssetsImpl = async () => ({ assets: [] });
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({ assets: [] });
    await syncRemoteUserData("user-1");
    const unrelatedCanvasId = useCanvasStore.getState().createProject("尚未同步的历史画布");

    const creation = createAgentCanvasProjectWithRemoteSync({
        draft: {
            prompt: "创建一个五秒产品短片",
            attachments: [],
            generationModels: { image: "", video: "" },
            skillSelections: [],
            executionMode: "automatic",
        },
        referenceImages: [],
    });

    expect(creation).not.toBeInstanceOf(Promise);
    expect(creation.id).not.toBe(unrelatedCanvasId);
    expect(useCanvasStore.getState().openProject(creation.id)?.pendingAgentLaunch?.prompt).toBe("创建一个五秒产品短片");

    const readiness = ensureRemoteCanvasProjectReady(creation.id);
    let ready = false;
    void readiness.then(() => {
        ready = true;
    });
    await Promise.resolve();
    expect(ready).toBe(false);

    releaseRemoteCreate?.();
    await Promise.all([creation.remoteReady, readiness]);

    expect(createdCanvasIds).toEqual([creation.id]);
});

test("全量同步已在运行时，立即保存仍等待新画布的定向远端创建", async () => {
    let releaseFullSync: (() => void) | undefined;
    let releaseTargetedCreate: (() => void) | undefined;
    let markFullSyncStarted: (() => void) | undefined;
    let markTargetedCreateStarted: (() => void) | undefined;
    const fullSyncGate = new Promise<void>((resolve) => {
        releaseFullSync = resolve;
    });
    const targetedCreateGate = new Promise<void>((resolve) => {
        releaseTargetedCreate = resolve;
    });
    const fullSyncStarted = new Promise<void>((resolve) => {
        markFullSyncStarted = resolve;
    });
    const targetedCreateStarted = new Promise<void>((resolve) => {
        markTargetedCreateStarted = resolve;
    });
    const [{ createAgentCanvasProjectWithRemoteSync, saveRemoteUserDataNow, syncRemoteUserData }, { useAssetStore }, { useCanvasStore }] = await Promise.all([
        import("../src/services/user-data-sync"),
        import("../src/stores/use-asset-store"),
        import("../src/stores/canvas/use-canvas-store"),
    ]);
    listRemoteAssetsImpl = async () => ({ assets: [] });
    useCanvasStore.setState({ projects: [], pendingDeletionIds: [] });
    useAssetStore.setState({ assets: [] });
    await syncRemoteUserData("user-1");
    const unrelatedCanvasId = useCanvasStore.getState().createProject("尚未同步的历史画布");
    createRemoteCanvasProjectImpl = async (project) => {
        createdCanvasIds.push(project.id);
        if (project.id === unrelatedCanvasId) {
            markFullSyncStarted?.();
            await fullSyncGate;
            return;
        }
        markTargetedCreateStarted?.();
        await targetedCreateGate;
    };

    const activeFullSync = saveRemoteUserDataNow();
    await fullSyncStarted;
    const creation = createAgentCanvasProjectWithRemoteSync({
        draft: {
            prompt: "创建新画布",
            attachments: [],
            generationModels: { image: "", video: "" },
            skillSelections: [],
            executionMode: "automatic",
        },
        referenceImages: [],
    });
    await targetedCreateStarted;
    let flushCompleted = false;
    const flush = saveRemoteUserDataNow().then(() => {
        flushCompleted = true;
    });

    releaseFullSync?.();
    await activeFullSync;
    await Promise.resolve();
    expect(flushCompleted).toBe(false);

    releaseTargetedCreate?.();
    await Promise.all([creation.remoteReady, flush]);
    expect(createdCanvasIds).toContain(creation.id);
});
