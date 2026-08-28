import { getMediaBlob } from "@/services/file-storage";
import { getImageBlob, resolveImageUrl } from "@/services/image-storage";
import { createRemoteCanvasProject, deleteRemoteAsset, deleteRemoteCanvasProject, getRemoteAsset, getRemoteCanvasProject, listRemoteAssets, listRemoteCanvasProjects, upsertRemoteAsset, type RemoteUserDataSummary } from "@/services/api/user-data";
import { resourceFileUrl, resourceIdFromStorageKey, resourceStorageKey, uploadResourceFile } from "@/services/api/resources";
import type { Asset } from "@/stores/use-asset-store";
import { useAssetStore } from "@/stores/use-asset-store";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import { flushCanvasStorePersistence, useCanvasStore } from "@/stores/canvas/use-canvas-store";
import { nanoid } from "nanoid";
import { canvasAgentProjectTitle, createCanvasAgentLaunchRequest } from "@/lib/canvas/canvas-agent-launch";
import type { CanvasAgentDraft } from "@/lib/canvas/canvas-agent-draft";
import { getNodeSpec } from "@/constant/canvas";
import { createCanvasProjectDeletionService, type CanvasProjectDeletionResult } from "@/services/canvas-project-deletion";
import type { UploadedImage } from "@/services/image-storage";
import type { CanvasNodeData } from "@/types/canvas";
import { CanvasNodeType } from "@/types/canvas";
import { remoteCanvasCreationRequired } from "@/lib/canvas/canvas-persistence-policy";
import { isRemoteCanvasDeletedError, mergeCanvasProjects } from "@/lib/canvas/canvas-sync-state";

let activeRemoteUserId = "";
let remoteSessionRevision = 0;
let applyingRemoteState = false;
let syncTimer: number | null = null;
let syncOperation: RemoteWriteOperation | null = null;
let remoteProjectCreationOperations = new Map<string, RemoteProjectCreationOperation>();
let remoteAssetWriteOperations = new Map<string, RemoteAssetWriteOperation>();
let subscriptionsInstalled = false;
let remoteAssetVersions = new Map<string, string>();
let remoteProjectVersions = new Map<string, string>();

const LOCAL_STORAGE_KEY_PATTERN = /^(image|video|audio|file|video-reference|audio-reference):/;

type RemoteSession = Readonly<{ revision: number; userId: string }>;
type RemoteWriteOperation = { session: RemoteSession; promise: Promise<void>; queued: boolean };
type RemoteProjectCreationOperation = { session: RemoteSession; promise: Promise<void> };
type RemoteAssetWriteOperation = { session: RemoteSession; promise: Promise<Asset> };
type AgentCanvasProjectCreation = { id: string; remoteReady: Promise<void> };

export async function syncRemoteUserData(userId?: string | null) {
    activeRemoteUserId = userId || "";
    if (!activeRemoteUserId) return;
    const session = { revision: ++remoteSessionRevision, userId: activeRemoteUserId };
    applyingRemoteState = true;
    try {
        await resumePendingCanvasProjectDeletions();
        if (!ownsRemoteSession(session)) return;
        const [remoteCanvas, remoteAssets] = await Promise.all([listRemoteCanvasProjects(), listRemoteAssets()]);
        if (!ownsRemoteSession(session)) return;
        remoteProjectVersions = versionMap(remoteCanvas.projects);
        remoteAssetVersions = versionMap(remoteAssets.assets);
        const localProjects = useCanvasStore.getState().projects;
        const localAssets = useAssetStore.getState().assets;
        const [changedProjects, changedAssets] = await Promise.all([
            fetchNewerRemoteItems(localProjects, remoteCanvas.projects, async (id) => (await getRemoteCanvasProject(id)).project),
            fetchNewerRemoteItems(localAssets, remoteAssets.assets, async (id) => (await getRemoteAsset(id)).asset),
        ]);
        if (!ownsRemoteSession(session)) return;
        const mergedProjects = mergeCanvasProjects(localProjects, changedProjects, remoteCanvas.deletions);
        const mergedAssets = mergeById(localAssets, await hydrateAssets(changedAssets));
        if (!ownsRemoteSession(session)) return;
        useCanvasStore.getState().replaceProjects(mergedProjects);
        useAssetStore.getState().replaceAssets(mergedAssets);
    } finally {
        if (ownsRemoteSession(session)) applyingRemoteState = false;
    }
    if (!ownsRemoteSession(session)) return;
    // 首次登录可能带有尚未创建到云端的本地画布；先完成一次 upsert，避免详情页保存/分享先于项目创建。
    try {
        await saveRemoteUserDataNow();
    } catch (error) {
        console.warn("登录后画布首次同步失败，保留本地项目等待重试", error);
    }
    scheduleRemoteUserDataSync();
}

export function installRemoteUserDataAutoSync() {
    if (subscriptionsInstalled) return;
    subscriptionsInstalled = true;
    useCanvasStore.subscribe((state, previous) => {
        if (state.projects !== previous.projects) scheduleRemoteUserDataSync();
    });
    useAssetStore.subscribe((state, previous) => {
        if (state.assets !== previous.assets) scheduleRemoteUserDataSync();
    });
}

export function resetRemoteUserDataSync() {
    remoteSessionRevision += 1;
    activeRemoteUserId = "";
    applyingRemoteState = false;
    remoteAssetVersions.clear();
    remoteProjectVersions.clear();
    remoteProjectCreationOperations.clear();
    remoteAssetWriteOperations.clear();
    if (syncTimer) {
        window.clearTimeout(syncTimer);
        syncTimer = null;
    }
}

function currentRemoteSession(): RemoteSession | null {
    return activeRemoteUserId ? { revision: remoteSessionRevision, userId: activeRemoteUserId } : null;
}

function ownsRemoteSession(session: RemoteSession) {
    return session.revision === remoteSessionRevision && session.userId === activeRemoteUserId;
}

export function scheduleRemoteUserDataSync() {
    if (!activeRemoteUserId || applyingRemoteState) return;
    if (syncOperation && ownsRemoteSession(syncOperation.session)) {
        syncOperation.queued = true;
        return;
    }
    if (syncTimer) window.clearTimeout(syncTimer);
    syncTimer = window.setTimeout(() => {
        syncTimer = null;
        void saveRemoteUserDataNow().catch((error) => console.warn("云端自动同步失败", error));
    }, 1200);
}

export async function createCanvasProjectWithRemoteSync(title: string, projectId?: string) {
    const id = useCanvasStore.getState().createProject(title, projectId);
    if (!activeRemoteUserId) return { id, syncError: new Error("尚未建立云端同步会话") };
    try {
        await saveRemoteUserDataNow();
        return { id };
    } catch (syncError) {
        scheduleRemoteUserDataSync();
        return { id, syncError };
    }
}

export function createAgentCanvasProjectWithRemoteSync(input: { draft: CanvasAgentDraft; referenceImages: Array<UploadedImage & { name: string }> }): AgentCanvasProjectCreation {
    const now = new Date().toISOString();
    const store = useCanvasStore.getState();
    const id = store.createProject(canvasAgentProjectTitle(input.draft.prompt));
    const referenceNodes = createAgentReferenceNodes(input.referenceImages);
    store.updateProject(id, {
        nodes: referenceNodes,
        pendingAgentLaunch: createCanvasAgentLaunchRequest({
            draft: input.draft,
            id: nanoid(),
            createdAt: now,
        }),
    });
    return { id, remoteReady: ensureRemoteCanvasProjectReady(id) };
}

export function ensureRemoteCanvasProjectReady(projectId: string): Promise<void> {
    const session = currentRemoteSession();
    if (!session) return Promise.reject(new Error("尚未建立云端同步会话"));
    if (remoteProjectVersions.has(projectId)) return Promise.resolve();
    const activeCreation = remoteProjectCreationOperations.get(projectId);
    if (activeCreation && ownsRemoteSession(activeCreation.session)) return activeCreation.promise;

    const operation: RemoteProjectCreationOperation = {
        session,
        promise: createRemoteCanvasProjectById(session, projectId),
    };
    remoteProjectCreationOperations.set(projectId, operation);
    operation.promise.then(
        () => {
            if (remoteProjectCreationOperations.get(projectId) === operation) remoteProjectCreationOperations.delete(projectId);
        },
        () => {
            if (remoteProjectCreationOperations.get(projectId) === operation) remoteProjectCreationOperations.delete(projectId);
            scheduleRemoteUserDataSync();
        },
    );
    return operation.promise;
}

async function createRemoteCanvasProjectById(session: RemoteSession, projectId: string) {
    const project = useCanvasStore.getState().openProject(projectId);
    if (!project) throw new Error("待创建的画布不存在");
    await createRemoteCanvasProject(project);
    if (!ownsRemoteSession(session)) throw new Error("云端同步会话已变更，无法确认画布创建结果");
    remoteProjectVersions.set(project.id, project.updatedAt);
}

function createAgentReferenceNodes(images: Array<UploadedImage & { name: string }>): CanvasNodeData[] {
    const spec = getNodeSpec(CanvasNodeType.Image);
    return images.map((image, index) => ({
        id: nanoid(),
        type: CanvasNodeType.Image,
        title: image.name || `参考图片 ${index + 1}`,
        position: { x: index * (spec.width + 40), y: 0 },
        width: spec.width,
        height: spec.height,
        metadata: {
            ...spec.metadata,
            content: image.url,
            status: "success",
            storageKey: image.storageKey,
            naturalWidth: image.width,
            naturalHeight: image.height,
            mimeType: image.mimeType,
            bytes: image.bytes,
            workflowKind: "reference_set",
        },
    }));
}

export async function deleteAssetWithRemoteSync(id: string) {
    if (activeRemoteUserId) {
        await deleteRemoteAsset(id);
        remoteAssetVersions.delete(id);
    }
    useAssetStore.getState().removeAsset(id);
}

const deleteCanvasProjects = createCanvasProjectDeletionService({
    resolveTarget: (id) => {
        const store = useCanvasStore.getState();
        const project = store.projects.find((item) => item.id === id);
        const pendingDeletion = store.pendingDeletionIds.includes(id);
        if (!project && !pendingDeletion) return null;
        return {
            id,
            requiresRemoteDelete: pendingDeletion || Boolean(activeRemoteUserId || remoteProjectVersions.has(id) || project?.ownerUserId || project?.teamId),
            canManage: pendingDeletion || project?.canManage !== false,
        };
    },
    hasRemoteSession: () => Boolean(activeRemoteUserId),
    isRemoteDeleteStaged: (id) => useCanvasStore.getState().pendingDeletionIds.includes(id),
    stageRemoteDelete: async (ids) => {
        const previousPendingDeletionIds = useCanvasStore.getState().pendingDeletionIds;
        useCanvasStore.getState().stageProjectDeletions(ids);
        try {
            await flushCanvasStorePersistence();
        } catch (error) {
            useCanvasStore.setState({ pendingDeletionIds: previousPendingDeletionIds });
            throw error;
        }
    },
    cancelRemoteDelete: async (ids) => {
        const previousPendingDeletionIds = useCanvasStore.getState().pendingDeletionIds;
        useCanvasStore.getState().cancelProjectDeletions(ids);
        try {
            await flushCanvasStorePersistence();
        } catch (error) {
            useCanvasStore.setState({ pendingDeletionIds: previousPendingDeletionIds });
            throw error;
        }
    },
    waitForRemoteWrites: async () => {
        const activeSync = syncOperation;
        if (activeSync && ownsRemoteSession(activeSync.session)) await activeSync.promise;
    },
    deleteRemote: async (id) => {
        await deleteRemoteCanvasProject(id);
    },
    deleteLocal: async (ids) => {
        const previousProjects = useCanvasStore.getState().projects;
        const previousPendingDeletionIds = useCanvasStore.getState().pendingDeletionIds;
        for (const id of ids) remoteProjectVersions.delete(id);
        useCanvasStore.getState().finishProjectDeletions(ids);
        try {
            await flushCanvasStorePersistence();
        } catch (error) {
            useCanvasStore.setState({ projects: previousProjects, pendingDeletionIds: previousPendingDeletionIds });
            throw error;
        }
    },
});

export function deleteCanvasProjectsWithRemoteSync(ids: string[]): Promise<CanvasProjectDeletionResult> {
    return deleteCanvasProjects(ids);
}

async function resumePendingCanvasProjectDeletions() {
    const pendingDeletionIds = [...useCanvasStore.getState().pendingDeletionIds];
    if (pendingDeletionIds.length === 0) return;
    const result = await deleteCanvasProjects(pendingDeletionIds);
    if (result.failures.length > 0) {
        console.warn("未完成的画布删除请求恢复失败", { failures: result.failures });
    }
}

export async function saveRemoteUserDataNow() {
    const session = currentRemoteSession();
    if (!session) return;
    if (syncOperation && ownsRemoteSession(syncOperation.session)) {
        syncOperation.queued = true;
        await Promise.all([syncOperation.promise, waitForRemoteProjectCreations(session)]);
        return;
    }
    await waitForRemoteProjectCreations(session);
    if (!ownsRemoteSession(session)) return;
    if (syncOperation && ownsRemoteSession(syncOperation.session)) {
        syncOperation.queued = true;
        return syncOperation.promise;
    }
    const operation: RemoteWriteOperation = { session, promise: Promise.resolve(), queued: false };
    operation.promise = drainRemoteUserDataChanges(operation);
    syncOperation = operation;
    try {
        await operation.promise;
    } finally {
        if (syncOperation === operation) syncOperation = null;
    }
}

export function saveRemoteAssetNow(assetId: string): Promise<Asset> {
    const session = currentRemoteSession();
    if (!session) return Promise.reject(new Error("尚未建立云端同步会话"));
    const pending = remoteAssetWriteOperations.get(assetId);
    if (pending && ownsRemoteSession(pending.session)) return pending.promise;

    const operation: RemoteAssetWriteOperation = {
        session,
        promise: persistRemoteAssetById(session, assetId),
    };
    remoteAssetWriteOperations.set(assetId, operation);
    operation.promise.finally(() => {
        if (remoteAssetWriteOperations.get(assetId) === operation) remoteAssetWriteOperations.delete(assetId);
    }).catch(() => undefined);
    return operation.promise;
}

async function persistRemoteAssetById(session: RemoteSession, assetId: string) {
    const uploaded = new Map<string, string>();
    while (ownsRemoteSession(session)) {
        const asset = useAssetStore.getState().assets.find((item) => item.id === assetId);
        if (!asset) throw new Error("待同步的素材不存在");
        const [prepared] = await prepareRemoteAssets([asset], uploaded);
        if (!prepared) throw new Error("素材远端引用准备失败");
        if (!ownsRemoteSession(session)) break;

        const currentAssets = useAssetStore.getState().assets;
        const current = currentAssets.find((item) => item.id === assetId);
        // 资源上传期间允许用户继续编辑；只有快照仍是当前版本时才能回写，避免旧字段覆盖新输入。
        if (current !== asset) continue;

        const previousApplyingRemoteState = applyingRemoteState;
        applyingRemoteState = true;
        try {
            useAssetStore.getState().replaceAssets(replaceById(currentAssets, [prepared]));
        } finally {
            applyingRemoteState = previousApplyingRemoteState;
        }
        await upsertRemoteAsset(prepared);
        if (!ownsRemoteSession(session)) break;
        remoteAssetVersions.set(prepared.id, prepared.updatedAt);
        if (useAssetStore.getState().assets.find((item) => item.id === assetId) === prepared) return prepared;
    }
    throw new Error("云端同步会话已变更，无法确认素材写入结果");
}

async function waitForRemoteProjectCreations(session: RemoteSession) {
    const pending = Array.from(remoteProjectCreationOperations.values())
        .filter((operation) => operation.session.revision === session.revision && operation.session.userId === session.userId)
        .map((operation) => operation.promise);
    if (pending.length) await Promise.all(pending);
}

async function drainRemoteUserDataChanges(operation: RemoteWriteOperation) {
    do {
        operation.queued = false;
        await saveRemoteUserDataBatch(operation.session);
    } while (operation.queued && ownsRemoteSession(operation.session));
}

async function saveRemoteUserDataBatch(session: RemoteSession) {
    if (!ownsRemoteSession(session)) return;
    try {
        const canvasState = useCanvasStore.getState();
        const currentProjects = canvasState.projects;
        const currentAssets = useAssetStore.getState().assets;
        const pendingDeletionIds = new Set(canvasState.pendingDeletionIds);
        const dirtyProjects = currentProjects.filter((item) => !pendingDeletionIds.has(item.id) && !remoteProjectCreationOperations.has(item.id) && remoteCanvasCreationRequired(remoteProjectVersions, item.id));
        const dirtyAssets = currentAssets.filter((item) => remoteAssetWriteRequired(remoteAssetVersions.get(item.id), item.updatedAt));
        const deletedAssetIds = missingIds(remoteAssetVersions, currentAssets);
        if (!dirtyProjects.length && !dirtyAssets.length && !deletedAssetIds.length) return;
        const uploaded = new Map<string, string>();
        const projects = await prepareRemoteCanvasProjects(dirtyProjects, uploaded);
        const assets = await prepareRemoteAssets(dirtyAssets, uploaded);
        if (!ownsRemoteSession(session)) return;
        applyingRemoteState = true;
        if (projects.length) useCanvasStore.getState().replaceProjects(replaceById(currentProjects, projects));
        if (assets.length) useAssetStore.getState().replaceAssets(replaceById(currentAssets, assets));
        applyingRemoteState = false;
        // SQLite 和接口频控都要求写入保持有界；逐项提交还能准确记录已完成版本。
        for (const project of projects) {
            if (!ownsRemoteSession(session)) return;
            try {
                await createRemoteCanvasProject(project);
            } catch (error) {
                if (!isRemoteCanvasDeletedError(error)) throw error;
                remoteProjectVersions.delete(project.id);
                useCanvasStore.getState().finishProjectDeletions([project.id]);
                await flushCanvasStorePersistence();
                console.info("已按云端删除事实清理陈旧本地画布", { canvasId: project.id });
                continue;
            }
            if (!ownsRemoteSession(session)) return;
            remoteProjectVersions.set(project.id, project.updatedAt);
        }
        for (const asset of assets) {
            if (!ownsRemoteSession(session)) return;
            await writeRemoteAsset(session, asset);
        }
        for (const id of deletedAssetIds) {
            if (!ownsRemoteSession(session)) return;
            await deleteRemoteAsset(id);
            if (!ownsRemoteSession(session)) return;
            remoteAssetVersions.delete(id);
        }
    } finally {
        if (ownsRemoteSession(session)) applyingRemoteState = false;
    }
}

async function writeRemoteAsset(session: RemoteSession, asset: Asset) {
    const pending = remoteAssetWriteOperations.get(asset.id);
    if (pending && ownsRemoteSession(pending.session)) {
        await pending.promise;
        if (!ownsRemoteSession(session)) throw new Error("云端同步会话已变更，无法继续素材写入");
    }
    if (!remoteAssetWriteRequired(remoteAssetVersions.get(asset.id), asset.updatedAt)) return;
    await upsertRemoteAsset(asset);
    if (!ownsRemoteSession(session)) throw new Error("云端同步会话已变更，无法确认素材写入结果");
    remoteAssetVersions.set(asset.id, asset.updatedAt);
}

async function hydrateAssets(assets: Asset[]): Promise<Asset[]> {
    return Promise.all(
        assets.map(async (asset) => {
            if (asset.kind === "image" && asset.data.storageKey) {
                const dataUrl = await resolveImageUrl(asset.data.storageKey, asset.data.dataUrl);
                return { ...asset, coverUrl: shouldReplaceEphemeralUrl(asset.coverUrl) ? dataUrl : asset.coverUrl, data: { ...asset.data, dataUrl } };
            }
            if (asset.kind === "video" && asset.data.storageKey) {
                const url = await resolveResourceOrMediaUrl(asset.data.storageKey, asset.data.url);
                return { ...asset, coverUrl: shouldReplaceEphemeralUrl(asset.coverUrl) ? url : asset.coverUrl, data: { ...asset.data, url } };
            }
            if (asset.kind === "audio" && asset.data.storageKey) {
                const url = await resolveResourceOrMediaUrl(asset.data.storageKey, asset.data.url);
                return { ...asset, coverUrl: shouldReplaceEphemeralUrl(asset.coverUrl) ? url : asset.coverUrl, data: { ...asset.data, url } };
            }
            if (asset.kind === "model" && asset.data.storageKey) {
                const url = await resolveResourceOrMediaUrl(asset.data.storageKey, asset.data.url);
                return { ...asset, data: { ...asset.data, url } };
            }
            return asset;
        }),
    );
}

async function prepareRemoteAssets(assets: Asset[], uploaded: Map<string, string>) {
    const result: Asset[] = [];
    for (const asset of assets) result.push(await ensureRemoteAssetResourceReferences(asset, uploaded));
    return result;
}

async function ensureRemoteAssetResourceReferences(asset: Asset, uploaded: Map<string, string>): Promise<Asset> {
    if (asset.kind === "text" || asset.kind === "entity") return ensureRemoteResourceReferences(asset, uploaded);
    const data = await ensureRemoteResourceReferences(asset.data, uploaded);
    const storageKey = data.storageKey;
    if (!storageKey || !resourceIdFromStorageKey(storageKey)) return { ...asset, data } as Asset;
    const url = resourceFileUrl(storageKey.slice("resource:".length));
    return {
        ...asset,
        coverUrl: shouldReplaceEphemeralUrl(asset.coverUrl) ? url : asset.coverUrl,
        data,
    } as Asset;
}

async function prepareRemoteCanvasProjects(projects: CanvasProject[], uploaded: Map<string, string>) {
    const result: CanvasProject[] = [];
    for (const project of projects) result.push(await ensureRemoteResourceReferences(project, uploaded));
    return result;
}

async function ensureRemoteResourceReferences<T>(value: T, uploaded = new Map<string, string>()): Promise<T> {
    if (!value || typeof value !== "object") return value;
    if (Array.isArray(value)) {
        const result: unknown[] = [];
        for (const item of value) result.push(await ensureRemoteResourceReferences(item, uploaded));
        return result as T;
    }

    const next: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
        next[key] = await ensureRemoteResourceReferences(child, uploaded);
    }

    const storageKey = typeof next.storageKey === "string" ? next.storageKey : "";
    const remoteResourceId = resourceIdFromStorageKey(storageKey);
    if (remoteResourceId) return applyResourceReference(next, storageKey) as T;

    if (!isLocalStorageKey(storageKey)) {
        const inline = inlineMediaDataUrl(next);
        if (!inline) return next as T;
        const cacheKey = `inline:${inline}`;
        const resourceStorage = uploaded.get(cacheKey) || (await uploadInlineDataUrl(inline));
        uploaded.set(cacheKey, resourceStorage);
        return applyResourceReference(next, resourceStorage) as T;
    }

    const cached = uploaded.get(storageKey);
    const resourceStorage = cached || (await uploadLocalStorageKey(storageKey, next));
    uploaded.set(storageKey, resourceStorage);
    return applyResourceReference(next, resourceStorage) as T;
}

function applyResourceReference(payload: Record<string, unknown>, storageKey: string) {
    const url = resourceFileUrl(storageKey.slice("resource:".length));
    payload.storageKey = storageKey;
    for (const key of ["content", "dataUrl", "url", "coverUrl"]) {
        if (typeof payload[key] === "string") payload[key] = url;
    }
    return payload;
}

function inlineMediaDataUrl(payload: Record<string, unknown>) {
    for (const key of ["dataUrl", "content", "url", "coverUrl"]) {
        const value = payload[key];
        if (typeof value === "string" && /^data:(image|video|audio)\//i.test(value)) return value;
    }
    return "";
}

async function uploadInlineDataUrl(dataUrl: string) {
    const blob = await (await fetch(dataUrl)).blob();
    const kind: "image" | "video" | "audio" | "file" = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind);
    return resourceStorageKey(resource.id);
}

async function uploadLocalStorageKey(storageKey: string, payload: Record<string, unknown>) {
    const blob = storageKey.startsWith("image:") ? await getImageBlob(storageKey) : await getMediaBlob(storageKey);
    if (!blob) throw new Error(`本地媒体数据不存在，无法同步：${storageKey}`);
    const kind = blob.type.startsWith("image/") ? "image" : blob.type.startsWith("video/") ? "video" : blob.type.startsWith("audio/") ? "audio" : "file";
    const resource = await uploadResourceFile(blob, kind, {
        width: numberValue(payload.naturalWidth) || numberValue(payload.width),
        height: numberValue(payload.naturalHeight) || numberValue(payload.height),
    });
    return resourceStorageKey(resource.id);
}

function mergeById<T extends { id?: string; updatedAt?: string }>(local: T[], remote: T[]) {
    const items = new Map<string, T>();
    remote.forEach((item) => {
        if (item.id) items.set(item.id, item);
    });
    local.forEach((item) => {
        if (!item.id) return;
        const current = items.get(item.id);
        if (!current || timeValue(item.updatedAt) >= timeValue(current.updatedAt)) items.set(item.id, item);
    });
    return Array.from(items.values()).sort((a, b) => timeValue(b.updatedAt) - timeValue(a.updatedAt));
}

async function fetchNewerRemoteItems<T extends { id: string; updatedAt?: string }>(local: T[], remote: RemoteUserDataSummary[], fetchItem: (id: string) => Promise<T>) {
    const localById = new Map(local.map((item) => [item.id, item]));
    const pending = remote.filter((item) => {
        const current = localById.get(item.id);
        return !current || timeValue(item.updatedAt) > timeValue(current.updatedAt);
    });
    return Promise.all(pending.map((item) => fetchItem(item.id)));
}

function versionMap(items: RemoteUserDataSummary[]) {
    return new Map(items.map((item) => [item.id, item.updatedAt]));
}

function missingIds<T extends { id: string }>(remote: Map<string, string>, local: T[]) {
    const localIds = new Set(local.map((item) => item.id));
    return Array.from(remote.keys()).filter((id) => !localIds.has(id));
}

function replaceById<T extends { id: string }>(current: T[], changed: T[]) {
    const changedById = new Map(changed.map((item) => [item.id, item]));
    return current.map((item) => changedById.get(item.id) || item);
}

function timeValue(value?: string) {
    const time = value ? Date.parse(value) : 0;
    return Number.isFinite(time) ? time : 0;
}

function remoteAssetWriteRequired(remoteVersion: string | undefined, localVersion: string) {
    if (!remoteVersion) return true;
    const remoteTime = Date.parse(remoteVersion);
    const localTime = Date.parse(localVersion);
    if (!Number.isFinite(remoteTime) || !Number.isFinite(localTime)) return true;
    return remoteTime !== localTime;
}

function isLocalStorageKey(value: string) {
    return LOCAL_STORAGE_KEY_PATTERN.test(value) && !resourceIdFromStorageKey(value);
}

function shouldReplaceEphemeralUrl(value: string) {
    return !value || value.startsWith("blob:") || value.startsWith("data:");
}

async function resolveResourceOrMediaUrl(storageKey: string, fallback: string) {
    const resourceId = resourceIdFromStorageKey(storageKey);
    if (resourceId) return resourceFileUrl(resourceId);
    const { resolveMediaUrl } = await import("@/services/file-storage");
    return resolveMediaUrl(storageKey, fallback);
}

function numberValue(value: unknown) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : undefined;
}
