import { UserDataRequestError, type RemoteCanvasDeletion } from "@/services/api/user-data";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

export function mergeCanvasProjects(local: CanvasProject[], remote: CanvasProject[], deletions: RemoteCanvasDeletion[]) {
    const deletedIds = new Set(deletions.map((deletion) => deletion.id));
    const items = new Map<string, CanvasProject>();
    remote.forEach((item) => {
        if (!deletedIds.has(item.id)) items.set(item.id, item);
    });
    local.forEach((item) => {
        if (deletedIds.has(item.id)) return;
        const current = items.get(item.id);
        if (!current || timeValue(item.updatedAt) >= timeValue(current.updatedAt)) items.set(item.id, item);
    });
    return Array.from(items.values()).sort((a, b) => timeValue(b.updatedAt) - timeValue(a.updatedAt));
}

export function isRemoteCanvasDeletedError(error: unknown): error is UserDataRequestError {
    return error instanceof UserDataRequestError && error.status === 410;
}

function timeValue(value?: string) {
    const time = value ? Date.parse(value) : 0;
    return Number.isFinite(time) ? time : 0;
}
