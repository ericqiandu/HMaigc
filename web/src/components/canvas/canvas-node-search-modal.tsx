import { useMemo, useState } from "react";
import { Input, Modal } from "antd";
import { BookOpenText, FileText, Image, Pencil, Search, Video } from "lucide-react";

import { WorkspaceState } from "@/components/layout/workspace-state";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";

export function CanvasNodeSearchModal({ open, nodes, onClose, onFocus }: { open: boolean; nodes: CanvasNodeData[]; onClose: () => void; onFocus: (nodeId: string) => void }) {
    const [query, setQuery] = useState("");
    const results = useMemo(() => {
        const keyword = query.trim().toLocaleLowerCase();
        if (!keyword) return nodes.slice(0, 40);
        return nodes.filter((node) => [node.title, node.type, nodeTypeLabel(node.type), node.metadata?.prompt, node.metadata?.composerContent, node.metadata?.model, node.metadata?.chapterTitle, node.metadata?.workflowTitle, node.metadata?.workflowDescription, typeof node.metadata?.shotIndex === "number" ? `镜头 ${node.metadata.shotIndex + 1}` : "", ...(node.metadata?.assetTags || [])]
            .some((value) => typeof value === "string" && value.toLocaleLowerCase().includes(keyword))).slice(0, 80);
    }, [nodes, query]);

    return (
        <Modal rootClassName="canvas-overlay-modal canvas-overlay-modal--search" title="搜索画布节点" open={open} footer={null} width="min(680px, 90vw)" onCancel={onClose} afterClose={() => setQuery("")} centered>
            <Input className="canvas-node-search-input" autoFocus allowClear value={query} onChange={(event) => setQuery(event.target.value)} prefix={<Search className="size-4 opacity-50" />} placeholder="搜索节点、章节、镜头、模型或标签" />
            <div className="canvas-overlay-list canvas-node-search-results thin-scrollbar max-h-[50vh] overflow-y-auto">
                {results.length ? results.map((node) => (
                    <button
                        key={node.id}
                        type="button"
                        className="canvas-overlay-list-item canvas-node-search-result flex w-full items-center gap-3 px-3 py-2.5 text-left transition"
                        onClick={() => { onFocus(node.id); onClose(); }}
                    >
                        <span className="canvas-overlay-list-item-icon canvas-node-search-result-icon grid size-8 shrink-0 place-items-center">
                            {node.type === CanvasNodeType.Image ? <Image className="size-4" /> : node.type === CanvasNodeType.Video ? <Video className="size-4" /> : node.type === CanvasNodeType.Drawing ? <Pencil className="size-4" /> : <FileText className="size-4" />}
                        </span>
                        <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium">{node.title}</span>
                            <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs opacity-55">
                                {node.metadata?.chapterTitle ? <BookOpenText className="size-3 shrink-0" /> : null}
                                <span className="truncate">{nodeContextLabel(node)}</span>
                            </span>
                        </span>
                    </button>
                )) : <WorkspaceState icon="canvas" compact title="没有匹配节点" description="换一个节点、章节、镜头、模型或标签继续搜索。" />}
            </div>
        </Modal>
    );
}

function nodeContextLabel(node: CanvasNodeData) {
    const location = [node.metadata?.chapterTitle, typeof node.metadata?.shotIndex === "number" ? `镜头 ${node.metadata.shotIndex + 1}` : ""].filter(Boolean).join(" · ");
    return location || node.metadata?.prompt || node.metadata?.composerContent || node.metadata?.workflowDescription || nodeTypeLabel(node.type);
}

function nodeTypeLabel(type: CanvasNodeType) {
    return ({
        [CanvasNodeType.Image]: "图片节点",
        [CanvasNodeType.Text]: "文本节点",
        [CanvasNodeType.Drawing]: "绘图节点",
        [CanvasNodeType.Script]: "分镜脚本节点",
        [CanvasNodeType.Skill]: "技能节点",
        [CanvasNodeType.Config]: "生成配置节点",
        [CanvasNodeType.Video]: "视频节点",
        [CanvasNodeType.Audio]: "音频节点",
        [CanvasNodeType.Frame]: "背板",
    } as Record<CanvasNodeType, string>)[type] || "未知节点";
}
