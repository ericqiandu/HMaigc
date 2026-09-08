import { isFrameNode } from "@/lib/canvas/canvas-frame";
import { getConnectionTargetAnchor } from "@/lib/canvas/canvas-project-domain";
import type { CanvasConnection, CanvasNodeData } from "@/types/canvas";

export type CanvasWorldRect = {
    left: number;
    top: number;
    right: number;
    bottom: number;
};

export type CanvasMediaVisibilityTier = "visible" | "near";

export type CanvasRenderQuery = {
    nodeIds: readonly string[];
    mediaTierByNodeId: ReadonlyMap<string, CanvasMediaVisibilityTier>;
    connectionIds: readonly string[];
};

export type CanvasRenderIndex = {
    query: (input: { visible: CanvasWorldRect; nearPadding: number }) => CanvasRenderQuery;
};

export type CanvasDisplayConnection = {
    connection: CanvasConnection;
    from: CanvasNodeData;
    to: CanvasNodeData;
};

type IndexedNode = {
    id: string;
    order: number;
    frame: boolean;
    bounds: CanvasWorldRect;
};

type IndexedConnection = {
    id: string;
    order: number;
    bounds: CanvasWorldRect;
};

type ConnectionCurve = {
    start: { x: number; y: number };
    firstControl: { x: number; y: number };
    secondControl: { x: number; y: number };
    end: { x: number; y: number };
    bounds: CanvasWorldRect;
};

type BoundsTree<T extends { bounds: CanvasWorldRect }> = {
    bounds: CanvasWorldRect;
    entries?: readonly T[];
    first?: BoundsTree<T>;
    second?: BoundsTree<T>;
};

const CELL_SIZE = 512;
const MAX_NODE_BUCKETS = 256;
const BOUNDS_TREE_LEAF_SIZE = 16;
const CONNECTION_SAMPLE_DISTANCE = CELL_SIZE / 6;

export function createCanvasRenderIndex(nodes: readonly CanvasNodeData[], connections: readonly CanvasConnection[], hiddenNodeIds: ReadonlySet<string>): CanvasRenderIndex {
    const nodeById = new Map<string, CanvasNodeData>();
    const nodeBuckets = new Map<string, IndexedNode[]>();
    const oversizedNodes: IndexedNode[] = [];
    const connectionBuckets = new Map<string, IndexedConnection[]>();
    const connectionIds = new Set<string>();

    nodes.forEach((node, order) => {
        if (nodeById.has(node.id)) throw new Error(`画布节点 ID 重复：${node.id}`);
        const bounds = nodeBounds(node);
        nodeById.set(node.id, node);
        if (hiddenNodeIds.has(node.id)) return;
        const indexed = { id: node.id, order, frame: isFrameNode(node), bounds };
        if (!addNodeToBuckets(nodeBuckets, indexed, bounds)) oversizedNodes.push(indexed);
    });

    connections.forEach((connection, order) => {
        if (connectionIds.has(connection.id)) throw new Error(`画布连线 ID 重复：${connection.id}`);
        connectionIds.add(connection.id);
        if (!nodeById.has(connection.fromNodeId) || !nodeById.has(connection.toNodeId)) throw new Error(`画布连线 ${connection.id} 引用了不存在的节点`);
        const displayConnection = resolveCanvasDisplayConnection(connection, nodeById, hiddenNodeIds);
        if (!displayConnection) return;
        const curve = connectionCurve(displayConnection);
        addConnectionToBuckets(connectionBuckets, { id: connection.id, order, bounds: curve.bounds }, curve);
    });
    const oversizedNodeTree = buildBoundsTree(oversizedNodes);

    return {
        query: ({ visible, nearPadding }) => {
            validateQuery(visible, nearPadding);
            const near = expandRect(visible, nearPadding);
            const nodeCandidates = collectCandidates(nodeBuckets, near);
            queryOversizedNodeTree(oversizedNodeTree, near, nodeCandidates);
            const orderedNodes = [...nodeCandidates.values()].filter((entry) => intersects(entry.bounds, near)).sort((left, right) => Number(right.frame) - Number(left.frame) || left.order - right.order);
            const mediaTierByNodeId = new Map<string, CanvasMediaVisibilityTier>();
            for (const entry of orderedNodes) mediaTierByNodeId.set(entry.id, intersects(entry.bounds, visible) ? "visible" : "near");

            const connectionCandidates = collectCandidates(connectionBuckets, visible);
            const visibleConnectionIds = [...connectionCandidates.values()]
                .filter((entry) => intersects(entry.bounds, visible))
                .sort((left, right) => left.order - right.order)
                .map((entry) => entry.id);

            return {
                nodeIds: orderedNodes.map((entry) => entry.id),
                mediaTierByNodeId,
                connectionIds: visibleConnectionIds,
            };
        },
    };
}

function buildBoundsTree<T extends { bounds: CanvasWorldRect }>(entries: readonly T[]): BoundsTree<T> | null {
    if (!entries.length) return null;
    const bounds = mergeBounds(entries);
    if (entries.length <= BOUNDS_TREE_LEAF_SIZE) return { bounds, entries };
    const splitOnX = bounds.right - bounds.left >= bounds.bottom - bounds.top;
    const sorted = [...entries].sort((left, right) => {
        const leftCenter = splitOnX ? left.bounds.left + left.bounds.right : left.bounds.top + left.bounds.bottom;
        const rightCenter = splitOnX ? right.bounds.left + right.bounds.right : right.bounds.top + right.bounds.bottom;
        return leftCenter - rightCenter;
    });
    const midpoint = Math.floor(sorted.length / 2);
    const first = buildBoundsTree(sorted.slice(0, midpoint));
    const second = buildBoundsTree(sorted.slice(midpoint));
    if (!first || !second) throw new Error("画布超大节点空间索引构建失败");
    return { bounds, first, second };
}

function mergeBounds(entries: readonly { bounds: CanvasWorldRect }[]): CanvasWorldRect {
    let left = Number.POSITIVE_INFINITY;
    let top = Number.POSITIVE_INFINITY;
    let right = Number.NEGATIVE_INFINITY;
    let bottom = Number.NEGATIVE_INFINITY;
    for (const entry of entries) {
        left = Math.min(left, entry.bounds.left);
        top = Math.min(top, entry.bounds.top);
        right = Math.max(right, entry.bounds.right);
        bottom = Math.max(bottom, entry.bounds.bottom);
    }
    return { left, top, right, bottom };
}

function queryOversizedNodeTree(tree: BoundsTree<IndexedNode> | null, bounds: CanvasWorldRect, matches: Map<string, IndexedNode>) {
    if (!tree || !intersects(tree.bounds, bounds)) return;
    if (tree.entries) {
        for (const entry of tree.entries) if (intersects(entry.bounds, bounds)) matches.set(entry.id, entry);
        return;
    }
    queryOversizedNodeTree(tree.first || null, bounds, matches);
    queryOversizedNodeTree(tree.second || null, bounds, matches);
}

export function resolveCanvasDisplayConnection(connection: CanvasConnection, nodeById: ReadonlyMap<string, CanvasNodeData>, hiddenNodeIds: ReadonlySet<string>): CanvasDisplayConnection | null {
    const fromNode = nodeById.get(connection.fromNodeId);
    const toNode = nodeById.get(connection.toNodeId);
    if (!fromNode || !toNode) return null;
    if (isCollapsedBatchChild(fromNode, nodeById) || isCollapsedBatchChild(toNode, nodeById)) return null;

    const displayFrom = collapsedParentFrame(fromNode, nodeById) || fromNode;
    const displayTo = collapsedParentFrame(toNode, nodeById) || toNode;
    if ((hiddenNodeIds.has(fromNode.id) && displayFrom === fromNode) || (hiddenNodeIds.has(toNode.id) && displayTo === toNode) || displayFrom.id === displayTo.id) return null;
    return { connection, from: displayFrom, to: displayTo };
}

function isCollapsedBatchChild(node: CanvasNodeData, nodeById: ReadonlyMap<string, CanvasNodeData>) {
    const root = node.metadata?.batchRootId ? nodeById.get(node.metadata.batchRootId) : null;
    return Boolean(root && !root.metadata?.batchExpanded);
}

function collapsedParentFrame(node: CanvasNodeData, nodeById: ReadonlyMap<string, CanvasNodeData>) {
    const parent = node.parentId ? nodeById.get(node.parentId) : null;
    return parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed ? parent : null;
}

function nodeBounds(node: CanvasNodeData): CanvasWorldRect {
    const values = [node.position.x, node.position.y, node.width, node.height];
    if (!values.every(Number.isFinite) || node.width <= 0 || node.height <= 0) throw new Error(`画布节点 ${node.id} 的几何数据无效`);
    return { left: node.position.x, top: node.position.y, right: node.position.x + node.width, bottom: node.position.y + node.height };
}

function connectionCurve({ connection, from, to }: CanvasDisplayConnection): ConnectionCurve {
    const start = getConnectionTargetAnchor(from, { nodeId: to.id, handleType: "target" }, connection.fromHandleId);
    const end = getConnectionTargetAnchor(to, { nodeId: from.id, handleType: "source" }, connection.toHandleId);
    const startX = start.x;
    const startY = start.y;
    const endX = end.x;
    const endY = end.y;
    const curvature = Math.max(Math.abs(endX - startX) * 0.5, 50);
    const firstControlX = startX + curvature;
    const secondControlX = endX - curvature;
    const firstControl = { x: firstControlX, y: startY };
    const secondControl = { x: secondControlX, y: endY };
    return {
        start: { x: startX, y: startY },
        firstControl,
        secondControl,
        end: { x: endX, y: endY },
        bounds: {
            left: Math.min(startX, endX, firstControlX, secondControlX),
            top: Math.min(startY, endY, from.position.y, to.position.y),
            right: Math.max(startX, endX, firstControlX, secondControlX),
            bottom: Math.max(startY, endY, from.position.y + from.height, to.position.y + to.height),
        },
    };
}

function validateQuery(visible: CanvasWorldRect, nearPadding: number) {
    if (![visible.left, visible.top, visible.right, visible.bottom].every(Number.isFinite)) throw new Error("画布可见区域必须是有限矩形");
    if (visible.right < visible.left || visible.bottom < visible.top) throw new Error("画布可见区域边界无效");
    if (!Number.isFinite(nearPadding) || nearPadding < 0) throw new Error("画布近区边距必须是非负有限数字");
}

function expandRect(rect: CanvasWorldRect, padding: number): CanvasWorldRect {
    return { left: rect.left - padding, top: rect.top - padding, right: rect.right + padding, bottom: rect.bottom + padding };
}

function intersects(left: CanvasWorldRect, right: CanvasWorldRect) {
    return left.right > right.left && left.left < right.right && left.bottom > right.top && left.top < right.bottom;
}

function addNodeToBuckets<T>(buckets: Map<string, T[]>, entry: T, bounds: CanvasWorldRect) {
    const minColumn = Math.floor(bounds.left / CELL_SIZE);
    const maxColumn = Math.floor(bounds.right / CELL_SIZE);
    const minRow = Math.floor(bounds.top / CELL_SIZE);
    const maxRow = Math.floor(bounds.bottom / CELL_SIZE);
    const bucketCount = (maxColumn - minColumn + 1) * (maxRow - minRow + 1);
    if (bucketCount > MAX_NODE_BUCKETS) return false;
    for (let column = minColumn; column <= maxColumn; column += 1) {
        for (let row = minRow; row <= maxRow; row += 1) {
            addToBucket(buckets, entry, column, row);
        }
    }
    return true;
}

function addConnectionToBuckets(buckets: Map<string, IndexedConnection[]>, entry: IndexedConnection, curve: ConnectionCurve) {
    const width = curve.bounds.right - curve.bounds.left;
    const height = curve.bounds.bottom - curve.bounds.top;
    const sampleCount = Math.max(1, Math.ceil(Math.max(width, height) / CONNECTION_SAMPLE_DISTANCE));
    const visited = new Set<string>();
    for (let index = 0; index <= sampleCount; index += 1) {
        const point = cubicBezierPoint(curve, index / sampleCount);
        const column = Math.floor(point.x / CELL_SIZE);
        const row = Math.floor(point.y / CELL_SIZE);
        for (let columnOffset = -1; columnOffset <= 1; columnOffset += 1) {
            for (let rowOffset = -1; rowOffset <= 1; rowOffset += 1) {
                const key = `${column + columnOffset}:${row + rowOffset}`;
                if (visited.has(key)) continue;
                visited.add(key);
                addToBucket(buckets, entry, column + columnOffset, row + rowOffset);
            }
        }
    }
}

function cubicBezierPoint(curve: ConnectionCurve, ratio: number) {
    const inverse = 1 - ratio;
    const firstWeight = inverse * inverse * inverse;
    const secondWeight = 3 * inverse * inverse * ratio;
    const thirdWeight = 3 * inverse * ratio * ratio;
    const fourthWeight = ratio * ratio * ratio;
    return {
        x: firstWeight * curve.start.x + secondWeight * curve.firstControl.x + thirdWeight * curve.secondControl.x + fourthWeight * curve.end.x,
        y: firstWeight * curve.start.y + secondWeight * curve.firstControl.y + thirdWeight * curve.secondControl.y + fourthWeight * curve.end.y,
    };
}

function addToBucket<T>(buckets: Map<string, T[]>, entry: T, column: number, row: number) {
    const key = `${column}:${row}`;
    const entries = buckets.get(key) || [];
    entries.push(entry);
    buckets.set(key, entries);
}

function collectCandidates<T extends { id: string }>(buckets: ReadonlyMap<string, readonly T[]>, bounds: CanvasWorldRect) {
    const candidates = new Map<string, T>();
    const minColumn = Math.floor(bounds.left / CELL_SIZE);
    const maxColumn = Math.floor(bounds.right / CELL_SIZE);
    const minRow = Math.floor(bounds.top / CELL_SIZE);
    const maxRow = Math.floor(bounds.bottom / CELL_SIZE);
    for (let column = minColumn; column <= maxColumn; column += 1) {
        for (let row = minRow; row <= maxRow; row += 1) {
            for (const entry of buckets.get(`${column}:${row}`) || []) candidates.set(entry.id, entry);
        }
    }
    return candidates;
}
