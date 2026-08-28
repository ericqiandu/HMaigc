import type { CanvasNodeMetadata } from "@/types/canvas";

export function buildVideoLastFrameMetadata(sourceNodeId: string, metadata: CanvasNodeMetadata): CanvasNodeMetadata {
    return {
        ...metadata,
        mediaProvenance: {
            kind: "video_last_frame",
            sourceNodeId,
        },
    };
}
