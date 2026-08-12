import { getResourceOSSUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { imageToDataUrl } from "@/services/image-storage";
import type { CanvasAssistantReference } from "@/types/canvas";

type CanvasAgentImageInputDependencies = {
    getResourceOSSUrl: typeof getResourceOSSUrl;
    imageToDataUrl: typeof imageToDataUrl;
};

const defaultDependencies: CanvasAgentImageInputDependencies = {
    getResourceOSSUrl,
    imageToDataUrl,
};

export async function resolveCanvasAgentImageInput(reference: CanvasAssistantReference, dependencies: CanvasAgentImageInputDependencies = defaultDependencies) {
    if (resourceIdFromStorageKey(reference.storageKey)) return dependencies.getResourceOSSUrl(reference.storageKey);
    return dependencies.imageToDataUrl(reference);
}
