import { useEffect, useState } from "react";
import { Music2, RefreshCw, Video } from "lucide-react";

import { canvasMediaPlaybackUrl } from "@/lib/canvas-media-playback";
import type { CanvasTheme } from "@/lib/canvas-theme";
import type { CanvasMediaVisibilityTier } from "@/lib/canvas/canvas-render-index";
import { CanvasNodeAction, CanvasNodeEmptyState, CanvasNodeStatusLayout } from "./canvas-node-ui";
import { CanvasVideoPlayer, type CanvasVideoPlaybackError } from "./canvas-video-player";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

type CanvasMediaNodeContentProps = {
    node: CanvasNodeData;
    theme: CanvasTheme;
    reduceMediaEffects: boolean;
    mediaVisibilityTier: CanvasMediaVisibilityTier;
};

type CanvasMediaPlaybackFailure = CanvasVideoPlaybackError & {
    nodeId: string;
    resourceUrl: string;
};

export function CanvasMediaNodeContent({ node, theme, reduceMediaEffects, mediaVisibilityTier }: CanvasMediaNodeContentProps) {
    const [playbackAttempt, setPlaybackAttempt] = useState(0);
    const [playbackFailure, setPlaybackFailure] = useState<CanvasMediaPlaybackFailure | null>(null);
    const playbackUrl = canvasMediaPlaybackUrl(node);

    useEffect(() => {
        setPlaybackFailure(null);
        setPlaybackAttempt(0);
    }, [playbackUrl]);

    if (!node.metadata?.content) {
        return node.type === CanvasNodeType.Audio ? (
            <CanvasNodeEmptyState icon={<Music2 className="size-5" />} title="空音频节点" description="输入文字生成音频" theme={theme} />
        ) : (
            <CanvasNodeEmptyState icon={<Video className="size-5" />} title="空视频节点" theme={theme} />
        );
    }

    if (!playbackUrl) {
        return <CanvasNodeStatusLayout icon={<Video className="size-5" />} title="媒体地址无效" detail="资源缺少可播放地址" tone="danger" theme={theme} />;
    }

    if (node.type === CanvasNodeType.Video && mediaVisibilityTier === "near") {
        return (
            <div className="canvas-deferred-media grid size-full place-items-center" data-canvas-deferred-media="video" role="status" aria-label={`${node.title || "视频"}位于预加载区域`}>
                <Video className="canvas-deferred-media-icon size-5 opacity-45" />
            </div>
        );
    }

    if (playbackFailure) {
        return (
            <div
                className="canvas-media-playback-failure size-full"
                data-canvas-media-error-kind={playbackFailure.kind}
                data-canvas-media-error-node={playbackFailure.nodeId}
                data-canvas-media-error-resource={playbackFailure.resourceUrl}
                data-canvas-media-error-message={playbackFailure.message}
            >
                <CanvasNodeStatusLayout
                    icon={node.type === CanvasNodeType.Audio ? <Music2 className="size-5" /> : <Video className="size-5" />}
                    title="媒体加载失败"
                    detail={playbackFailure.message}
                    tone="danger"
                    theme={theme}
                    actions={
                        <CanvasNodeAction
                            icon={<RefreshCw />}
                            label="重新加载"
                            onClick={() => {
                                setPlaybackFailure(null);
                                setPlaybackAttempt((attempt) => attempt + 1);
                            }}
                        />
                    }
                />
            </div>
        );
    }

    if (node.type === CanvasNodeType.Audio) {
        return (
            <div className="canvas-audio-node-player">
                <div className="canvas-audio-node-player-copy">
                    <span className="canvas-audio-node-player-icon">
                        <Music2 className="canvas-audio-node-player-wave size-4" />
                    </span>
                    <span className="canvas-audio-node-player-title">{audioNodeTitle(node)}</span>
                </div>
                <audio
                    key={playbackAttempt}
                    src={playbackUrl}
                    controls
                    preload="metadata"
                    className="canvas-audio-node-player-control"
                    data-canvas-no-zoom
                    onError={() => setPlaybackFailure(createPlaybackFailure(node.id, playbackUrl, { kind: "media", message: "音频加载失败" }))}
                />
            </div>
        );
    }

    return (
        <CanvasVideoPlayer
            key={playbackAttempt}
            nodeId={node.id}
            src={playbackUrl}
            title={node.title || "视频"}
            preload={reduceMediaEffects ? "none" : "metadata"}
            onPlaybackError={(error) => setPlaybackFailure(createPlaybackFailure(node.id, playbackUrl, error))}
        />
    );
}

function createPlaybackFailure(nodeId: string, resourceUrl: string, error: CanvasVideoPlaybackError): CanvasMediaPlaybackFailure {
    return { nodeId, resourceUrl: credentialFreeUrlSummary(resourceUrl), ...error };
}

function credentialFreeUrlSummary(resourceUrl: string) {
    try {
        const parsed = new URL(resourceUrl, "https://canvas.invalid");
        return parsed.origin === "https://canvas.invalid" ? parsed.pathname : `${parsed.origin}${parsed.pathname}`;
    } catch {
        return "invalid-media-url";
    }
}

function audioNodeTitle(node: CanvasNodeData) {
    const title = node.title?.trim();
    return !title || title.toLocaleLowerCase() === "audio" ? "音频节点" : title;
}
