import type { CanvasNodeData, CanvasVideoCompositionClip } from "@/types/canvas";

export type ResolvedCompositionClip = CanvasVideoCompositionClip & {
    source: CanvasNodeData;
    durationMs: number;
};

export const reconcileCompositionClips = (
    sources: CanvasNodeData[],
    saved: CanvasVideoCompositionClip[] | undefined,
): CanvasVideoCompositionClip[] => {
    const sourceIds = new Set(sources.map((source) => source.id));
    const retained = (saved || []).filter((clip) => sourceIds.has(clip.sourceNodeId));
    const represented = new Set(retained.map((clip) => clip.sourceNodeId));
    return [...retained, ...sources.filter((source) => !represented.has(source.id)).map((source) => ({ id: crypto.randomUUID(), sourceNodeId: source.id, trimStartMs: 0 }))];
};

export const resolveCompositionClips = (
    clips: CanvasVideoCompositionClip[],
    sources: CanvasNodeData[],
): ResolvedCompositionClip[] => {
    const sourceById = new Map(sources.map((source) => [source.id, source]));
    return clips.flatMap((clip) => {
        const source = sourceById.get(clip.sourceNodeId);
        if (!source) return [];
        const sourceDurationMs = Math.max(1, source.metadata?.durationMs || 5000);
        const endMs = Math.min(sourceDurationMs, clip.trimEndMs ?? sourceDurationMs);
        return [{ ...clip, source, trimStartMs: Math.min(clip.trimStartMs, endMs - 1), trimEndMs: endMs, durationMs: endMs - Math.min(clip.trimStartMs, endMs - 1) }];
    });
};

export const splitCompositionClip = (
    clips: CanvasVideoCompositionClip[],
    clipId: string,
    splitAtSourceMs: number,
): CanvasVideoCompositionClip[] => clips.flatMap((clip) => {
    if (clip.id !== clipId) return [clip];
    const endMs = clip.trimEndMs;
    if (splitAtSourceMs <= clip.trimStartMs || (endMs !== undefined && splitAtSourceMs >= endMs)) return [clip];
    return [
        { ...clip, trimEndMs: splitAtSourceMs },
        { ...clip, id: crypto.randomUUID(), trimStartMs: splitAtSourceMs },
    ];
});
