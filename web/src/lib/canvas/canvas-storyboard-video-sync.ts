import { CanvasNodeType, type CanvasNodeData, type StoryboardRow } from "@/types/canvas";

type StoryboardRowsUpdater = (rows: StoryboardRow[]) => StoryboardRow[];

export function updateStoryboardRowsAndLinkedVideos(nodes: CanvasNodeData[], scriptNodeId: string, updateRows: StoryboardRowsUpdater): CanvasNodeData[] {
    const scriptNode = nodes.find((node) => node.id === scriptNodeId && node.type === CanvasNodeType.Script);
    if (!scriptNode) return nodes;

    const storyboard = scriptNode.metadata?.storyboard;
    const rows = updateRows(storyboard?.rows || []);
    const rowByVideoNodeId = new Map(rows.flatMap((row) => (row.videoNodeId ? [[row.videoNodeId, row] as const] : [])));

    return nodes.map((node) => {
        if (node.id === scriptNodeId) {
            return {
                ...node,
                metadata: {
                    ...node.metadata,
                    storyboard: {
                        rows,
                        visibleColumns: storyboard?.visibleColumns || ["shotNumber", "durationSeconds", "plotDescription", "dialogue"],
                        referenceNodeIds: storyboard?.referenceNodeIds || [],
                    },
                },
            };
        }

        const row = node.type === CanvasNodeType.Video ? rowByVideoNodeId.get(node.id) : undefined;
        if (!row) return node;
        const prompt = (row.videoMotionPrompt || row.plotDescription).trim();
        return {
            ...node,
            title: `镜头 ${row.shotNumber} · 视频`,
            metadata: {
                ...node.metadata,
                prompt,
                composerContent: prompt,
                seconds: String(row.durationSeconds),
                shotIndex: row.shotNumber,
                workflowKind: "shot",
                workflowTitle: `镜头 ${row.shotNumber} 视频`,
                generationMode: "video",
                videoEditOperation: node.metadata?.videoEditOperation || "text_to_video",
            },
        };
    });
}
