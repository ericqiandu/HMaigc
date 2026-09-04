import { resourceFileUrl, resourceIdFromStorageKey, resourceProxyFileUrl } from "@/services/api/resources";
import type { CanvasNodeData } from "@/types/canvas";

export function canvasResourceDisplayUrl(storageKey?: string, fallback = "") {
    const resourceId = resourceIdFromStorageKey(storageKey);
    return resourceId ? resourceFileUrl(resourceId) : fallback;
}

export function canvasResourceCaptureUrl(storageKey?: string, fallback = "") {
    const resourceId = resourceIdFromStorageKey(storageKey);
    return resourceId ? resourceProxyFileUrl(resourceId) : fallback;
}

export function canvasMediaPlaybackUrl(node: CanvasNodeData) {
    return canvasResourceDisplayUrl(node.metadata?.storageKey, node.metadata?.content || "");
}
