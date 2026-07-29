import type { CanvasBackgroundMode } from "@/lib/canvas-theme";
import type { CanvasMutationPatch } from "@/services/api/canvas-collaboration";
import type { CanvasConnection, CanvasNodeData } from "@/types/canvas";
import type { DirectorScene } from "@/types/director";

export type CanvasCollaborationDocument = {
    title: string;
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
    backgroundMode: CanvasBackgroundMode;
    showImageInfo: boolean;
    directorScenes: DirectorScene[];
};

export function diffCanvasCollaborationDocument(
    base: CanvasCollaborationDocument,
    current: CanvasCollaborationDocument,
): CanvasMutationPatch {
    const nodeChanges = diffEntities(base.nodes, current.nodes);
    const connectionChanges = diffEntities(base.connections, current.connections);
    const document: NonNullable<CanvasMutationPatch["document"]> = {};
    if (base.title !== current.title) document.title = current.title;
    if (base.backgroundMode !== current.backgroundMode) document.backgroundMode = current.backgroundMode;
    if (base.showImageInfo !== current.showImageInfo) document.showImageInfo = current.showImageInfo;
    if (!sameValue(base.directorScenes, current.directorScenes)) document.directorScenes = current.directorScenes;

    return compactCanvasMutationPatch({
        upsertNodes: nodeChanges.upserts,
        deleteNodeIds: nodeChanges.deletes,
        upsertConnections: connectionChanges.upserts,
        deleteConnectionIds: connectionChanges.deletes,
        document,
    });
}

export function applyCanvasMutationPatch(
    source: CanvasCollaborationDocument,
    patch: CanvasMutationPatch,
): CanvasCollaborationDocument {
    return {
        title: patch.document?.title ?? source.title,
        nodes: applyEntityPatch(source.nodes, patch.upsertNodes || [], patch.deleteNodeIds || []),
        connections: applyEntityPatch(source.connections, patch.upsertConnections || [], patch.deleteConnectionIds || []),
        backgroundMode: patch.document?.backgroundMode ?? source.backgroundMode,
        showImageInfo: patch.document?.showImageInfo ?? source.showImageInfo,
        directorScenes: patch.document?.directorScenes ?? source.directorScenes,
    };
}

export function isEmptyCanvasMutationPatch(patch: CanvasMutationPatch) {
    return !patch.upsertNodes?.length &&
        !patch.deleteNodeIds?.length &&
        !patch.upsertConnections?.length &&
        !patch.deleteConnectionIds?.length &&
        !patch.document;
}

function diffEntities<T extends { id: string }>(base: T[], current: T[]) {
    const baseByID = new Map(base.map((item) => [item.id, item]));
    const currentByID = new Map(current.map((item) => [item.id, item]));
    const upserts = current.filter((item) => {
        const previous = baseByID.get(item.id);
        return !previous || !sameValue(previous, item);
    });
    const deletes = base.filter((item) => !currentByID.has(item.id)).map((item) => item.id);
    return { upserts, deletes };
}

function applyEntityPatch<T extends { id: string }>(source: T[], upserts: T[], deletes: string[]) {
    const deleteSet = new Set(deletes);
    const upsertByID = new Map(upserts.map((item) => [item.id, item]));
    const result: T[] = [];
    const seen = new Set<string>();
    source.forEach((item) => {
        if (deleteSet.has(item.id)) return;
        result.push(upsertByID.get(item.id) || item);
        seen.add(item.id);
    });
    upserts.forEach((item) => {
        if (!seen.has(item.id)) result.push(item);
    });
    return result;
}

function compactCanvasMutationPatch(patch: CanvasMutationPatch): CanvasMutationPatch {
    const result: CanvasMutationPatch = {};
    if (patch.upsertNodes?.length) result.upsertNodes = patch.upsertNodes;
    if (patch.deleteNodeIds?.length) result.deleteNodeIds = patch.deleteNodeIds;
    if (patch.upsertConnections?.length) result.upsertConnections = patch.upsertConnections;
    if (patch.deleteConnectionIds?.length) result.deleteConnectionIds = patch.deleteConnectionIds;
    if (patch.document && Object.keys(patch.document).length) result.document = patch.document;
    return result;
}

function sameValue(left: unknown, right: unknown) {
    return JSON.stringify(left) === JSON.stringify(right);
}
