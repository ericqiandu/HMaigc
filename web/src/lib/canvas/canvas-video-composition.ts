import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "@/types/canvas";

export const VIDEO_COMPOSITION_NODE_SIZE = 350;

export const isVideoCompositionNode = (node: CanvasNodeData): boolean =>
    node.type === CanvasNodeType.Video && node.metadata?.videoEditOperation === "concat";

export const supportsVideoAssetUpload = (node: CanvasNodeData): boolean =>
    node.type === CanvasNodeType.Video && !isVideoCompositionNode(node);

export const normalizeVideoCompositionNode = (node: CanvasNodeData): CanvasNodeData =>
    isVideoCompositionNode(node) && (node.width !== VIDEO_COMPOSITION_NODE_SIZE || node.height !== VIDEO_COMPOSITION_NODE_SIZE)
        ? { ...node, width: VIDEO_COMPOSITION_NODE_SIZE, height: VIDEO_COMPOSITION_NODE_SIZE }
        : node;

export const getCompositionSourceVideos = (
    targetNodeId: string,
    nodes: CanvasNodeData[],
    connections: CanvasConnection[],
): CanvasNodeData[] => {
    const sourceIds = new Set(connections.filter((connection) => connection.toNodeId === targetNodeId).map((connection) => connection.fromNodeId));
    return nodes
        .filter((node) => sourceIds.has(node.id) && node.type === CanvasNodeType.Video && !isVideoCompositionNode(node) && Boolean(node.metadata?.content))
        .sort((left, right) => {
            const leftShot = left.metadata?.shotIndex ?? Number.MAX_SAFE_INTEGER;
            const rightShot = right.metadata?.shotIndex ?? Number.MAX_SAFE_INTEGER;
            return leftShot - rightShot || left.position.y - right.position.y || left.position.x - right.position.x;
        });
};
