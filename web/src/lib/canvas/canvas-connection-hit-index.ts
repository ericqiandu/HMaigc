import { isFrameNode } from "@/lib/canvas/canvas-frame";
import type { CanvasNodeData, Position } from "@/types/canvas";

type IndexedCanvasNode = {
    node: CanvasNodeData;
    order: number;
};

export type CanvasConnectionHitIndex = {
    bucketSize: number;
    buckets: Map<string, IndexedCanvasNode[]>;
    nodeById: Map<string, CanvasNodeData>;
};

export function createCanvasConnectionHitIndex(nodes: CanvasNodeData[], bucketSize = 320): CanvasConnectionHitIndex {
    const normalizedBucketSize = Math.max(64, bucketSize);
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const buckets = new Map<string, IndexedCanvasNode[]>();

    nodes.forEach((node, order) => {
        const batchRoot = node.metadata?.batchRootId ? nodeById.get(node.metadata.batchRootId) : undefined;
        const parent = node.parentId ? nodeById.get(node.parentId) : undefined;
        const hiddenByBatch = Boolean(batchRoot && !batchRoot.metadata?.batchExpanded);
        const hiddenByFrame = Boolean(parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed);
        if (isFrameNode(node) || hiddenByBatch || hiddenByFrame) return;

        const minColumn = Math.floor(node.position.x / normalizedBucketSize);
        const maxColumn = Math.floor((node.position.x + node.width) / normalizedBucketSize);
        const minRow = Math.floor(node.position.y / normalizedBucketSize);
        const maxRow = Math.floor((node.position.y + node.height) / normalizedBucketSize);
        for (let column = minColumn; column <= maxColumn; column += 1) {
            for (let row = minRow; row <= maxRow; row += 1) {
                const key = bucketKey(column, row);
                const entries = buckets.get(key) || [];
                entries.push({ node, order });
                buckets.set(key, entries);
            }
        }
    });

    return { bucketSize: normalizedBucketSize, buckets, nodeById };
}

export function queryCanvasConnectionHitIndex(index: CanvasConnectionHitIndex, point: Position, radius: number) {
    const normalizedRadius = Math.max(0, radius);
    const minColumn = Math.floor((point.x - normalizedRadius) / index.bucketSize);
    const maxColumn = Math.floor((point.x + normalizedRadius) / index.bucketSize);
    const minRow = Math.floor((point.y - normalizedRadius) / index.bucketSize);
    const maxRow = Math.floor((point.y + normalizedRadius) / index.bucketSize);
    const candidatesById = new Map<string, IndexedCanvasNode>();

    for (let column = minColumn; column <= maxColumn; column += 1) {
        for (let row = minRow; row <= maxRow; row += 1) {
            for (const candidate of index.buckets.get(bucketKey(column, row)) || []) {
                const { node } = candidate;
                const outsideQuery =
                    point.x + normalizedRadius < node.position.x ||
                    point.x - normalizedRadius > node.position.x + node.width ||
                    point.y + normalizedRadius < node.position.y ||
                    point.y - normalizedRadius > node.position.y + node.height;
                if (!outsideQuery) candidatesById.set(node.id, candidate);
            }
        }
    }

    return [...candidatesById.values()].sort((left, right) => right.order - left.order).map(({ node }) => node);
}

function bucketKey(column: number, row: number) {
    return `${column}:${row}`;
}
