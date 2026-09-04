import { CanvasNodeType, type CanvasMediaIntrinsicSize, type CanvasNodeData } from "@/types/canvas";

const VIDEO_GEOMETRY_EPSILON = 0.001;

export function fitVideoNodeToIntrinsicSize(node: CanvasNodeData, size: CanvasMediaIntrinsicSize): CanvasNodeData {
    if (!Number.isFinite(size.width) || size.width <= 0 || !Number.isFinite(size.height) || size.height <= 0) {
        throw new Error(`视频节点 ${node.id} 返回了无效的媒体尺寸`);
    }
    if (node.type !== CanvasNodeType.Video) return node;

    const height = (node.width * size.height) / size.width;
    const sameGeometry = Math.abs(node.height - height) < VIDEO_GEOMETRY_EPSILON;
    const sameIntrinsicSize = node.metadata?.naturalWidth === size.width && node.metadata?.naturalHeight === size.height;
    if (sameGeometry && sameIntrinsicSize) return node;

    return {
        ...node,
        height,
        metadata: { ...node.metadata, naturalWidth: size.width, naturalHeight: size.height },
    };
}
