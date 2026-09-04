import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent } from "react";
import { Camera, Pause, Play, Volume2, VolumeX } from "lucide-react";

import { captureVideoFrameAtTime } from "@/lib/canvas/canvas-video-frame";

import "./canvas-video-player.css";

export type CanvasVideoPlayerProps = {
    nodeId: string;
    src: string;
    captureSource: Blob | string;
    title: string;
    preload: "none" | "metadata";
    onPlaybackError: (error: CanvasVideoPlaybackError) => void;
};

export type CanvasVideoPlaybackError = {
    kind: "media" | "fullscreen" | "capture";
    message: string;
};

type CanvasVideoProgressStyle = CSSProperties & {
    "--canvas-video-progress-ratio": string;
};

export function CanvasVideoPlayer({ nodeId, src, captureSource, title, preload, onPlaybackError }: CanvasVideoPlayerProps) {
    const playerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<HTMLVideoElement>(null);
    const [isPlaying, setIsPlaying] = useState(false);
    const [isMuted, setIsMuted] = useState(false);
    const [currentTime, setCurrentTime] = useState(0);
    const [duration, setDuration] = useState(0);
    const [errorMessage, setErrorMessage] = useState<string | null>(null);

    useEffect(() => {
        const video = videoRef.current;
        return () => video?.pause();
    }, []);

    const reportError = useCallback(
        (kind: CanvasVideoPlaybackError["kind"], error: unknown, fallbackMessage: string) => {
            const message = error instanceof Error && error.message.trim() ? error.message : fallbackMessage;
            setErrorMessage(message);
            onPlaybackError({ kind, message });
        },
        [onPlaybackError],
    );

    const togglePlayback = useCallback(async () => {
        const video = videoRef.current;
        if (!video) return;
        setErrorMessage(null);
        try {
            if (isPlaying) video.pause();
            else await video.play();
        } catch (error: unknown) {
            reportError("media", error, "视频播放失败");
        }
    }, [isPlaying, reportError]);

    const seekTo = useCallback((nextTime: number) => {
        const video = videoRef.current;
        if (!video || !Number.isFinite(nextTime)) return;
        const safeDuration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0;
        const boundedTime = Math.max(0, Math.min(nextTime, safeDuration));
        video.currentTime = boundedTime;
        setCurrentTime(boundedTime);
    }, []);

    const toggleMuted = useCallback(() => {
        const video = videoRef.current;
        if (!video) return;
        const nextMuted = !video.muted;
        video.muted = nextMuted;
        setIsMuted(nextMuted);
    }, []);

    const openFullscreen = useCallback(async () => {
        const player = playerRef.current;
        if (!player) return;
        setErrorMessage(null);
        if (!player.requestFullscreen) {
            reportError("fullscreen", new Error("浏览器不支持视频全屏"), "浏览器不支持视频全屏");
            return;
        }
        try {
            await player.requestFullscreen();
        } catch (error: unknown) {
            reportError("fullscreen", error, "无法进入视频全屏");
        }
    }, [reportError]);

    const saveCurrentFrame = useCallback(async () => {
        const video = videoRef.current;
        if (!video) return;
        setErrorMessage(null);
        try {
            const captureTime = video.currentTime;
            const frameBlob = await captureVideoFrameAtTime(captureSource, captureTime);
            downloadFrame(frameBlob, captureTime);
        } catch (error: unknown) {
            reportError("capture", error, "无法保存当前视频帧");
        }
    }, [captureSource, reportError]);

    const handleKeyDown = useCallback(
        (event: ReactKeyboardEvent<HTMLDivElement>) => {
            if (event.target instanceof HTMLInputElement) return;
            const key = event.key.toLocaleLowerCase();
            if (key === " " || key === "enter") {
                event.preventDefault();
                event.stopPropagation();
                void togglePlayback();
                return;
            }
            if (key === "arrowright" || key === "arrowleft") {
                event.preventDefault();
                event.stopPropagation();
                seekTo(currentTime + (key === "arrowright" ? 5 : -5));
                return;
            }
            if (key === "m") {
                event.preventDefault();
                event.stopPropagation();
                toggleMuted();
                return;
            }
            if (key === "f") {
                event.preventDefault();
                event.stopPropagation();
                void openFullscreen();
            }
        },
        [currentTime, openFullscreen, seekTo, toggleMuted, togglePlayback],
    );

    const progressRatio = duration > 0 ? `${Math.max(0, Math.min(100, (currentTime / duration) * 100))}%` : "0%";
    const progressStyle: CanvasVideoProgressStyle = { "--canvas-video-progress-ratio": progressRatio };

    return (
        <div ref={playerRef} data-canvas-video-player="true" data-canvas-no-zoom="true" data-video-node-id={nodeId} className="canvas-video-player" role="group" aria-label={`${title} 播放器`} tabIndex={0} onKeyDown={handleKeyDown}>
            <video
                ref={videoRef}
                src={src}
                playsInline
                preload={preload}
                className="canvas-video-player-media"
                draggable={false}
                onDragStart={(event) => event.preventDefault()}
                onDoubleClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void openFullscreen();
                }}
                onPlay={() => setIsPlaying(true)}
                onPause={() => setIsPlaying(false)}
                onEnded={() => setIsPlaying(false)}
                onLoadedMetadata={(event) => {
                    const nextDuration = event.currentTarget.duration;
                    setDuration(Number.isFinite(nextDuration) && nextDuration > 0 ? nextDuration : 0);
                    const nextTime = event.currentTarget.currentTime;
                    setCurrentTime(Number.isFinite(nextTime) && nextTime > 0 ? nextTime : 0);
                }}
                onDurationChange={(event) => {
                    const nextDuration = event.currentTarget.duration;
                    setDuration(Number.isFinite(nextDuration) && nextDuration > 0 ? nextDuration : 0);
                }}
                onTimeUpdate={(event) => {
                    const nextTime = event.currentTarget.currentTime;
                    setCurrentTime(Number.isFinite(nextTime) && nextTime > 0 ? nextTime : 0);
                }}
                onVolumeChange={(event) => setIsMuted(event.currentTarget.muted)}
                onError={() => reportError("media", new Error("视频加载失败"), "视频加载失败")}
            />

            {errorMessage ? (
                <p className="canvas-video-player-alert" role="alert">
                    {errorMessage}
                </p>
            ) : null}

            <div data-canvas-video-controls="true" className="canvas-video-controls" onPointerDown={stopCanvasEvent} onMouseDown={stopCanvasEvent} onDoubleClick={stopCanvasEvent} onWheel={stopCanvasEvent}>
                <button type="button" className="canvas-video-control-button" aria-label={isPlaying ? "暂停视频" : "播放视频"} onClick={() => void togglePlayback()}>
                    {isPlaying ? <Pause className="canvas-video-control-icon" /> : <Play className="canvas-video-control-icon" />}
                </button>
                <output data-canvas-video-current-time="true" className="canvas-video-time canvas-video-time-current" aria-label="当前播放时间">
                    {formatVideoTime(currentTime)}
                </output>
                <div data-canvas-video-progress-track="true" data-disabled={duration > 0 ? undefined : "true"} className="canvas-video-progress-shell" style={progressStyle}>
                    <span className="canvas-video-progress-track" aria-hidden="true">
                        <span data-canvas-video-progress-fill="true" className="canvas-video-progress-fill" />
                        <span data-canvas-video-progress-thumb="true" className="canvas-video-progress-thumb" />
                    </span>
                    <input
                        className="canvas-video-progress-input"
                        type="range"
                        min="0"
                        max={duration || 0}
                        step="0.1"
                        value={Math.min(currentTime, duration || 0)}
                        disabled={!duration}
                        aria-label="视频进度"
                        onInput={(event) => seekTo(Number(event.currentTarget.value))}
                    />
                </div>
                <output data-canvas-video-duration="true" className="canvas-video-time canvas-video-time-duration" aria-label="视频总时长">
                    {formatVideoTime(duration)}
                </output>
                <button type="button" className="canvas-video-control-button" aria-label={isMuted ? "取消静音" : "静音"} onClick={toggleMuted}>
                    {isMuted ? <VolumeX className="canvas-video-control-icon" /> : <Volume2 className="canvas-video-control-icon" />}
                </button>
                <button type="button" className="canvas-video-control-button" aria-label="保存当前视频帧" onClick={() => void saveCurrentFrame()}>
                    <Camera className="canvas-video-control-icon" />
                </button>
            </div>
        </div>
    );
}

function stopCanvasEvent(event: { stopPropagation: () => void }) {
    event.stopPropagation();
}

function formatVideoTime(seconds: number) {
    if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
    const wholeSeconds = Math.floor(seconds);
    const minutes = Math.floor(wholeSeconds / 60);
    const remainingSeconds = wholeSeconds % 60;
    return `${minutes}:${String(remainingSeconds).padStart(2, "0")}`;
}

function downloadFrame(frameBlob: Blob, currentTime: number) {
    const objectUrl = URL.createObjectURL(frameBlob);
    const link = document.createElement("a");
    const frameTime = Number.isFinite(currentTime) && currentTime > 0 ? Math.floor(currentTime * 1_000) : 0;
    link.href = objectUrl;
    link.download = `video-frame-${frameTime}.png`;
    link.hidden = true;
    document.body.append(link);
    try {
        link.click();
    } finally {
        link.remove();
        URL.revokeObjectURL(objectUrl);
    }
}
