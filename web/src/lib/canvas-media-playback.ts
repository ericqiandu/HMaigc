import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import type { CanvasNodeData } from "@/types/canvas";

export function canvasMediaPlaybackUrl(node: CanvasNodeData) {
    const resourceId = resourceIdFromStorageKey(node.metadata?.storageKey);
    return resourceId ? resourceFileUrl(resourceId) : node.metadata?.content || "";
}
