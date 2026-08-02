import { CircleAlert, LoaderCircle, Scissors } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import type { MergeVideoProgress } from "@/lib/canvas/canvas-video-merge";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasNodeData } from "@/types/canvas";

import "./canvas-video-composition-node.css";

type CanvasVideoCompositionNodeProps = {
    node: CanvasNodeData;
    sourceVideos: CanvasNodeData[];
    isMerging: boolean;
    progress: MergeVideoProgress | null;
    onOpenEditor: () => void;
};

const failureReason = (node: CanvasNodeData): string | null => {
    const details = node.metadata?.errorDetails?.trim();
    if (details) return details;
    return node.metadata?.status === "error" ? "未记录错误原因" : null;
};

export function CanvasVideoCompositionNode({ node, sourceVideos, isMerging, progress, onOpenEditor }: CanvasVideoCompositionNodeProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const errorDetails = failureReason(node);
    const canOpenEditor = sourceVideos.length >= 1 && !isMerging;
    const actionLabel = isMerging
        ? `渲染中 ${Math.round(progress?.progress ?? 0)}%`
        : sourceVideos.length >= 1
            ? "打开视频合成"
            : "请连接视频";

    return (
        <section className="canvas-video-composition-node" style={{ color: theme.node.text }}>
            <Scissors
                aria-hidden="true"
                className="canvas-video-composition-icon"
                style={{ color: theme.node.text }}
                strokeWidth={1.45}
            />
            <div className="canvas-video-composition-action-row">
                <button
                    aria-label={`${actionLabel}，当前已连接 ${sourceVideos.length} 段视频`}
                    className="canvas-video-composition-action"
                    disabled={!canOpenEditor}
                    style={{
                        borderColor: theme.node.stroke,
                        color: theme.node.text,
                    }}
                    type="button"
                    onClick={(event) => {
                        event.stopPropagation();
                        onOpenEditor();
                    }}
                >
                    {isMerging ? <LoaderCircle aria-hidden="true" className="canvas-video-composition-spinner" /> : null}
                    <span className="canvas-video-composition-action-label">{actionLabel}</span>
                </button>
                {errorDetails ? (
                    <span
                        aria-label={`导出失败：${errorDetails}`}
                        className="canvas-video-composition-error"
                        role="status"
                        title={`导出失败：${errorDetails}`}
                    >
                        <CircleAlert aria-hidden="true" className="canvas-video-composition-error-icon" />
                    </span>
                ) : null}
            </div>
        </section>
    );
}
