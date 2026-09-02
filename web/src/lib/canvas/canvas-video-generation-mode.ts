import type { CanvasNodeMetadata, CanvasVideoEditOperation, CanvasVideoGenerationMode } from "@/types/canvas";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

export type VideoReferenceCounts = { image: number; video: number; audio: number };
export type VideoGenerationReferenceContext = {
    referenceImages: ReferenceImage[];
    referenceVideos: ReferenceVideo[];
    referenceAudios: ReferenceAudio[];
    imageCount: number;
    videoCount: number;
    audioCount: number;
};

export function resolveVideoGenerationMode(metadata?: CanvasNodeMetadata): CanvasVideoGenerationMode {
    if (metadata?.videoGenerationMode) return metadata.videoGenerationMode;
    if (metadata?.videoStartFrameNodeId && metadata.videoEndFrameNodeId) return "first_last_frame";
    if (metadata?.videoStartFrameNodeId) return "image";
    if (metadata?.videoEditOperation === "image_to_video") return "image_reference";
    return "text";
}

export function videoGenerationModeConflictReason(metadata: CanvasNodeMetadata | undefined, counts: VideoReferenceCounts): string | undefined {
    return resolveVideoGenerationMode(metadata) === "text" && counts.image > 0 ? "已连接图片，断开后可使用文生视频" : undefined;
}

export function videoModeOperation(mode: CanvasVideoGenerationMode, counts: VideoReferenceCounts): CanvasVideoEditOperation {
    if (mode === "text") return "text_to_video";
    if (mode === "omni_reference") {
        if (counts.image > 0) return "image_to_video";
        if (counts.video > 0) return "extend";
        if (counts.audio > 0) return "audio_to_video";
    }
    return "image_to_video";
}

export function videoModeMetadataPatch({
    mode,
    metadata,
    frameNodeIds,
    counts,
}: {
    mode: CanvasVideoGenerationMode;
    metadata?: CanvasNodeMetadata;
    frameNodeIds: string[];
    counts: VideoReferenceCounts;
}): Partial<CanvasNodeMetadata> {
    const storedStartFrame = metadata?.videoStartFrameNodeId;
    const storedEndFrame = metadata?.videoEndFrameNodeId;
    const firstFrame = frameNodeIds.includes(storedStartFrame || "") ? storedStartFrame : frameNodeIds[0];
    const lastFrame = frameNodeIds.includes(storedEndFrame || "") && storedEndFrame !== firstFrame
        ? storedEndFrame
        : frameNodeIds.find((nodeId) => nodeId !== firstFrame);
    if (mode === "image" && !firstFrame) throw new Error("图生视频需要一张已连接的参考图片");
    if (mode === "first_last_frame" && (!firstFrame || !lastFrame)) throw new Error("首尾帧模式需要两张不同的参考图片");
    return {
        videoGenerationMode: mode,
        videoEditOperation: videoModeOperation(mode, counts),
        videoStartFrameNodeId: mode === "image" || mode === "first_last_frame" ? firstFrame : undefined,
        videoEndFrameNodeId: mode === "first_last_frame" ? lastFrame : undefined,
    };
}

export function selectVideoGenerationContext<T extends VideoGenerationReferenceContext>(metadata: CanvasNodeMetadata | undefined, context: T): T {
    const mode = resolveVideoGenerationMode(metadata);
    if (mode === "text") {
        const conflictReason = videoGenerationModeConflictReason(metadata, { image: context.referenceImages.length, video: context.referenceVideos.length, audio: context.referenceAudios.length });
        if (conflictReason) throw new Error(conflictReason);
        return { ...context, referenceImages: [], referenceVideos: [], referenceAudios: [], imageCount: 0, videoCount: 0, audioCount: 0 };
    }
    if (mode === "image" || mode === "first_last_frame") {
        const requiredIds = [metadata?.videoStartFrameNodeId, mode === "first_last_frame" ? metadata?.videoEndFrameNodeId : undefined].filter((value): value is string => Boolean(value));
        const referenceImages = requiredIds.map((id) => context.referenceImages.find((image) => image.id === id)).filter((image): image is ReferenceImage => Boolean(image));
        if (referenceImages.length !== requiredIds.length || requiredIds.length !== (mode === "first_last_frame" ? 2 : 1)) {
            throw new Error(mode === "first_last_frame" ? "首尾帧素材未连接或尚未生成" : "图生视频的参考图片未连接或尚未生成");
        }
        return { ...context, referenceImages, referenceVideos: [], referenceAudios: [], imageCount: referenceImages.length, videoCount: 0, audioCount: 0 };
    }
    if (mode === "image_reference") {
        if (!context.referenceImages.length) throw new Error("图片参考模式需要至少一张已连接的参考图片");
        return { ...context, referenceVideos: [], referenceAudios: [], videoCount: 0, audioCount: 0 };
    }
    if (!context.referenceImages.length && !context.referenceVideos.length && !context.referenceAudios.length) throw new Error("全能参考模式需要至少一个图片、视频或音频素材");
    return context;
}

export function shouldRestoreStoredVideoReferenceImages(metadata: CanvasNodeMetadata | undefined, currentImageCount: number) {
    if (currentImageCount > 0) return false;
    const mode = resolveVideoGenerationMode(metadata);
    return mode === "image" || mode === "first_last_frame" || mode === "image_reference";
}
