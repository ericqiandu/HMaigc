import { useEffect, useState } from "react";
import { Music2, RefreshCw, Video } from "lucide-react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import { resourceFileUrl, resourceIdFromStorageKey } from "@/services/api/resources";
import { CanvasNodeAction, CanvasNodeEmptyState, CanvasNodeStatusLayout } from "./canvas-node-ui";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

type CanvasMediaNodeContentProps = {
    node: CanvasNodeData;
    theme: CanvasTheme;
    reduceMediaEffects: boolean;
};

export function CanvasMediaNodeContent({ node, theme, reduceMediaEffects }: CanvasMediaNodeContentProps) {
    const [playbackAttempt, setPlaybackAttempt] = useState(0);
    const [playbackFailed, setPlaybackFailed] = useState(false);
    const playbackUrl = canvasMediaPlaybackUrl(node);

    useEffect(() => {
        setPlaybackFailed(false);
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

    if (playbackFailed) {
        return (
            <CanvasNodeStatusLayout
                icon={node.type === CanvasNodeType.Audio ? <Music2 className="size-5" /> : <Video className="size-5" />}
                title="媒体加载失败"
                detail="请检查资源存储或网络后重试"
                tone="danger"
                theme={theme}
                actions={
                    <CanvasNodeAction
                        icon={<RefreshCw />}
                        label="重新加载"
                        onClick={() => {
                            setPlaybackFailed(false);
                            setPlaybackAttempt((attempt) => attempt + 1);
                        }}
                    />
                }
            />
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
                <audio key={playbackAttempt} src={playbackUrl} controls preload="metadata" className="canvas-audio-node-player-control" data-canvas-no-zoom onError={() => setPlaybackFailed(true)} />
            </div>
        );
    }

    return <video key={playbackAttempt} src={playbackUrl} controls playsInline preload={reduceMediaEffects ? "none" : "metadata"} className="h-full w-full bg-black object-contain" data-canvas-no-zoom onError={() => setPlaybackFailed(true)} />;
}

export function canvasMediaPlaybackUrl(node: CanvasNodeData) {
    const resourceId = resourceIdFromStorageKey(node.metadata?.storageKey);
    return resourceId ? resourceFileUrl(resourceId) : node.metadata?.content || "";
}

function audioNodeTitle(node: CanvasNodeData) {
    const title = node.title?.trim();
    return !title || title.toLocaleLowerCase() === "audio" ? "音频节点" : title;
}
