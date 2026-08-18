import { useEffect, useState } from "react";
import { Button, Modal } from "antd";
import { CircleAlert, RefreshCw } from "lucide-react";

import { canvasMediaPlaybackUrl } from "@/lib/canvas-media-playback";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

type CanvasMediaPreviewDialogProps = {
    node: CanvasNodeData | null;
    onClose: () => void;
};

export function CanvasMediaPreviewDialog({ node, onClose }: CanvasMediaPreviewDialogProps) {
    const [playbackAttempt, setPlaybackAttempt] = useState(0);
    const [playbackFailed, setPlaybackFailed] = useState(false);
    const previewUrl = node ? canvasMediaPlaybackUrl(node) : "";

    useEffect(() => {
        setPlaybackAttempt(0);
        setPlaybackFailed(false);
    }, [previewUrl]);

    return (
        <Modal
            rootClassName="canvas-overlay-modal canvas-overlay-modal--media-preview"
            className="canvas-media-preview-modal"
            title={node?.type === CanvasNodeType.Video ? "视频预览" : "图片预览"}
            open={Boolean(previewUrl)}
            centered
            onCancel={onClose}
            footer={null}
            width="min(1200px, calc(100vw - 32px))"
            styles={{ body: { padding: 0, display: "flex", justifyContent: "center", alignItems: "center", maxHeight: "84vh", overflow: "hidden", background: "#090909" } }}
        >
            {previewUrl && node?.type === CanvasNodeType.Video && !playbackFailed ? (
                <video
                    key={`${previewUrl}-${playbackAttempt}`}
                    src={previewUrl}
                    controls
                    playsInline
                    preload="metadata"
                    onError={() => setPlaybackFailed(true)}
                    className="canvas-media-preview-video max-h-[84vh] max-w-full bg-black object-contain"
                />
            ) : null}
            {previewUrl && playbackFailed ? (
                <div className="canvas-media-preview-error flex min-h-64 w-full flex-col items-center justify-center gap-3 px-6 py-10 text-center" role="alert">
                    <CircleAlert aria-hidden="true" className="canvas-media-preview-error-icon size-6 text-[var(--status-danger)]" />
                    <strong className="canvas-media-preview-error-title text-sm font-semibold text-white">{node?.type === CanvasNodeType.Video ? "视频加载失败" : "图片加载失败"}</strong>
                    <span className="canvas-media-preview-error-detail text-xs text-white/60">请检查资源存储或网络状态后重新加载</span>
                    <Button
                        className="canvas-media-preview-retry"
                        icon={<RefreshCw aria-hidden="true" className="canvas-media-preview-retry-icon size-4" />}
                        onClick={() => {
                            setPlaybackFailed(false);
                            setPlaybackAttempt((attempt) => attempt + 1);
                        }}
                    >
                        重新加载
                    </Button>
                </div>
            ) : null}
            {previewUrl && node?.type === CanvasNodeType.Image && !playbackFailed ? (
                <img
                    key={`${previewUrl}-${playbackAttempt}`}
                    src={previewUrl}
                    alt={node.title || "图片"}
                    onError={() => setPlaybackFailed(true)}
                    className="canvas-media-preview-image max-h-[84vh] max-w-full object-contain"
                />
            ) : null}
        </Modal>
    );
}
