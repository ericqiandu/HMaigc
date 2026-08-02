import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { Button, Modal, Slider, Tooltip } from "antd";
import { ChevronLeft, ChevronRight, Download, Maximize2, Pause, Play, Scissors, SkipBack, SkipForward, X } from "lucide-react";

import { reconcileCompositionClips, resolveCompositionClips, splitCompositionClip } from "@/lib/canvas/canvas-video-composition-editor";
import type { CanvasNodeData, CanvasVideoCompositionClip } from "@/types/canvas";
import "./canvas-video-composition-editor.css";

type CanvasVideoCompositionEditorProps = {
    open: boolean;
    node: CanvasNodeData;
    sourceVideos: CanvasNodeData[];
    exporting: boolean;
    onClose: () => void;
    onSave: (clips: CanvasVideoCompositionClip[]) => void;
    onExport: (clips: CanvasVideoCompositionClip[]) => void;
};

const formatTime = (milliseconds: number) => {
    const seconds = Math.max(0, Math.floor(milliseconds / 1000));
    return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
};

export function CanvasVideoCompositionEditor({ open, node, sourceVideos, exporting, onClose, onSave, onExport }: CanvasVideoCompositionEditorProps) {
    const videoRef = useRef<HTMLVideoElement>(null);
    const [clips, setClips] = useState<CanvasVideoCompositionClip[]>([]);
    const [selectedClipId, setSelectedClipId] = useState<string | null>(null);
    const [playing, setPlaying] = useState(false);
    const [playheadMs, setPlayheadMs] = useState(0);
    const [zoom, setZoom] = useState(0.025);

    useEffect(() => {
        if (!open) return;
        const reconciled = reconcileCompositionClips(sourceVideos, node.metadata?.compositionClips);
        setClips(reconciled);
        setSelectedClipId(reconciled[0]?.id || null);
        setPlayheadMs(0);
    }, [node.id, node.metadata?.compositionClips, open, sourceVideos]);

    const resolved = useMemo(() => resolveCompositionClips(clips, sourceVideos), [clips, sourceVideos]);
    const selectedIndex = Math.max(0, resolved.findIndex((clip) => clip.id === selectedClipId));
    const selected = resolved[selectedIndex];
    const totalDurationMs = resolved.reduce((sum, clip) => sum + clip.durationMs, 0);

    const selectClip = (clipId: string) => {
        setSelectedClipId(clipId);
        setPlayheadMs(0);
        setPlaying(false);
    };
    const updateSelected = (patch: Partial<CanvasVideoCompositionClip>) => {
        if (!selected) return;
        setClips((current) => current.map((clip) => clip.id === selected.id ? { ...clip, ...patch } : clip));
    };
    const moveSelected = (offset: -1 | 1) => {
        if (!selected) return;
        setClips((current) => {
            const index = current.findIndex((clip) => clip.id === selected.id);
            const target = index + offset;
            if (target < 0 || target >= current.length) return current;
            const next = [...current];
            [next[index], next[target]] = [next[target], next[index]];
            return next;
        });
    };
    const togglePlayback = async () => {
        const video = videoRef.current;
        if (!video || !selected) return;
        if (video.paused) {
            const trimEndSeconds = (selected.trimStartMs + selected.durationMs) / 1000;
            if (video.currentTime >= trimEndSeconds - 0.05) {
                video.currentTime = selected.trimStartMs / 1000;
                setPlayheadMs(0);
            }
            await video.play();
        }
        else video.pause();
    };

    return (
        <Modal open={open} footer={null} closable={false} width="calc(100vw - 32px)" className="canvas-video-editor-modal" onCancel={onClose} destroyOnHidden>
            <main className="canvas-video-editor flex h-full min-h-0 flex-col bg-[#171717] text-[#f5f5f5]">
                <header className="canvas-video-editor-header flex h-14 shrink-0 items-center gap-3 px-4">
                    <h1 className="canvas-video-editor-title m-0 flex-1 text-[16px] font-semibold leading-6">视频合成</h1>
                    <span className="canvas-video-editor-summary text-[12px] text-white/45">{resolved.length} 个片段 · {formatTime(totalDurationMs)}</span>
                    <Button className="canvas-video-editor-export !h-9 !rounded-lg" type="primary" icon={<Download className="size-4" />} loading={exporting} disabled={resolved.length < 1} onClick={() => { onSave(clips); onExport(clips); }}>导出</Button>
                    <Button className="canvas-video-editor-close !grid !size-9 !place-items-center !rounded-lg !border-0 !bg-transparent !p-0 !text-white/65" aria-label="退出视频合成" icon={<X className="size-5" />} onClick={() => { onSave(clips); onClose(); }} />
                </header>

                <section className="canvas-video-editor-preview flex min-h-0 flex-1 items-center justify-center bg-[#202020] px-4 py-3">
                    {selected ? (
                        <video
                            key={selected.id}
                            ref={videoRef}
                            className="canvas-video-editor-player h-full max-h-full w-auto max-w-full bg-black object-contain"
                            src={selected.source.metadata?.content}
                            preload="metadata"
                            onLoadedMetadata={(event) => {
                                event.currentTarget.currentTime = selected.trimStartMs / 1000;
                                if (selected.source.metadata?.durationMs !== undefined || selected.trimEndMs !== undefined) return;
                                const detectedDurationMs = Math.round(event.currentTarget.duration * 1000);
                                if (Number.isFinite(detectedDurationMs) && detectedDurationMs > selected.trimStartMs) updateSelected({ trimEndMs: detectedDurationMs });
                            }}
                            onTimeUpdate={(event) => {
                                const relativeMs = Math.max(0, event.currentTarget.currentTime * 1000 - selected.trimStartMs);
                                setPlayheadMs(Math.min(selected.durationMs, relativeMs));
                                if (relativeMs >= selected.durationMs) { event.currentTarget.pause(); setPlaying(false); }
                            }}
                            onPlay={() => setPlaying(true)}
                            onPause={() => setPlaying(false)}
                        />
                    ) : <p className="canvas-video-editor-empty m-0 text-[13px] text-white/45">请先将视频节点连接到视频合成节点</p>}
                </section>

                <section className="canvas-video-editor-timeline flex h-[244px] shrink-0 flex-col bg-[#1d1d1d]">
                    <div className="canvas-video-editor-tools flex h-12 shrink-0 items-center gap-1 border-b border-white/8 px-3">
                        <Tooltip title="在播放头处分割"><Button className="canvas-video-editor-tool !grid !size-8 !place-items-center !border-0 !bg-transparent !p-0 !text-white/70" aria-label="分割片段" icon={<Scissors className="size-4" />} disabled={!selected || playheadMs <= 0 || playheadMs >= (selected?.durationMs || 0)} onClick={() => selected && setClips((current) => splitCompositionClip(current, selected.id, selected.trimStartMs + playheadMs))} /></Tooltip>
                        <Tooltip title="裁剪到播放头"><Button className="canvas-video-editor-tool !grid !size-8 !place-items-center !border-0 !bg-transparent !p-0 !text-white/70" aria-label="设置入点" icon={<SkipBack className="size-4" />} disabled={!selected || playheadMs <= 0} onClick={() => {
                            if (!selected) return;
                            const trimStartMs = selected.trimStartMs + playheadMs;
                            updateSelected({ trimStartMs });
                            setPlayheadMs(0);
                            if (videoRef.current) videoRef.current.currentTime = trimStartMs / 1000;
                        }} /></Tooltip>
                        <Tooltip title="从播放头裁掉右侧"><Button className="canvas-video-editor-tool !grid !size-8 !place-items-center !border-0 !bg-transparent !p-0 !text-white/70" aria-label="设置出点" icon={<SkipForward className="size-4" />} disabled={!selected || playheadMs <= 0} onClick={() => {
                            if (!selected) return;
                            videoRef.current?.pause();
                            updateSelected({ trimEndMs: selected.trimStartMs + playheadMs });
                        }} /></Tooltip>
                        <span className="canvas-video-editor-tool-separator mx-2 h-5 w-px bg-white/10" />
                        <Tooltip title="片段左移"><Button className="canvas-video-editor-tool !grid !size-8 !place-items-center !border-0 !bg-transparent !p-0 !text-white/70" aria-label="片段左移" icon={<ChevronLeft className="size-4" />} disabled={!selected || selectedIndex === 0} onClick={() => moveSelected(-1)} /></Tooltip>
                        <Tooltip title="片段右移"><Button className="canvas-video-editor-tool !grid !size-8 !place-items-center !border-0 !bg-transparent !p-0 !text-white/70" aria-label="片段右移" icon={<ChevronRight className="size-4" />} disabled={!selected || selectedIndex === resolved.length - 1} onClick={() => moveSelected(1)} /></Tooltip>
                        <div className="canvas-video-editor-transport ml-auto flex items-center gap-2">
                            <span className="canvas-video-editor-current text-[12px] tabular-nums">{formatTime(playheadMs)}</span>
                            <Button className="canvas-video-editor-play !grid !size-9 !place-items-center !rounded-full !border-0 !bg-white !p-0 !text-black" aria-label={playing ? "暂停" : "播放"} icon={playing ? <Pause className="size-4" /> : <Play className="ml-0.5 size-4" />} disabled={!selected} onClick={() => void togglePlayback()} />
                            <span className="canvas-video-editor-duration text-[12px] tabular-nums text-white/45">{formatTime(selected?.durationMs || 0)}</span>
                        </div>
                        <Maximize2 className="canvas-video-editor-fullscreen ml-auto size-4 text-white/45" aria-hidden="true" />
                        <Slider className="canvas-video-editor-zoom ml-2 !w-28" min={0.012} max={0.06} step={0.002} value={zoom} onChange={setZoom} aria-label="时间轴缩放" />
                    </div>
                    <div className="canvas-video-editor-ruler h-7 shrink-0 border-b border-white/8 px-16 text-[10px] leading-7 text-white/35">00:00</div>
                    <div className="canvas-video-editor-track-row flex min-h-0 flex-1">
                        <aside className="canvas-video-editor-track-label flex w-16 shrink-0 items-center justify-center border-r border-white/8 text-white/45"><Play className="size-4" /></aside>
                        <div className="canvas-video-editor-timeline-scroll canvas-overlay-scroll-surface flex min-w-0 flex-1 items-center gap-px overflow-x-auto overflow-y-hidden px-2 py-4" style={{ "--timeline-zoom": zoom } as CSSProperties}>
                            {resolved.map((clip, index) => (
                                <button key={clip.id} type="button" className="canvas-video-editor-clip relative h-16 shrink-0 overflow-hidden bg-[#2d2d2d] text-left outline-none focus-visible:ring-2 focus-visible:ring-blue-400" style={{ "--clip-duration-ms": clip.durationMs } as CSSProperties} aria-selected={clip.id === selected?.id} onClick={() => selectClip(clip.id)}>
                                    <video className="canvas-video-editor-clip-thumbnail h-full w-full object-cover opacity-70" src={clip.source.metadata?.content} preload="metadata" muted />
                                    <span className="canvas-video-editor-clip-title absolute inset-x-2 top-1 truncate text-[10px] font-medium text-white drop-shadow">{index + 1}. {clip.source.title}</span>
                                    <span className="canvas-video-editor-clip-duration absolute bottom-1 right-2 text-[10px] tabular-nums text-white/75">{formatTime(clip.durationMs)}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                </section>
            </main>
        </Modal>
    );
}
