import type { CanvasConnection, CanvasNodeData } from "@/types/canvas";

const RETIRED_DRAWING_NODE_TYPE = "drawing";

export type CanvasGraph = {
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
};

export type CanvasProjectGraph = CanvasGraph & {
    updatedAt: string;
};

export function removeRetiredCanvasNodes(graph: CanvasGraph): CanvasGraph {
    const retiredNodeIds = new Set(
        graph.nodes
            .filter((node) => String(node.type) === RETIRED_DRAWING_NODE_TYPE)
            .map((node) => node.id),
    );
    if (!retiredNodeIds.size) return graph;
    return {
        nodes: graph.nodes.filter((node) => !retiredNodeIds.has(node.id)),
        connections: graph.connections.filter(
            (connection) =>
                !retiredNodeIds.has(connection.fromNodeId) &&
                !retiredNodeIds.has(connection.toNodeId),
        ),
    };
}

export function removeRetiredCanvasProjectContent<T extends CanvasProjectGraph>(
    project: T,
): T {
    const graph = removeRetiredCanvasNodes(project);
    if (graph === project) return project;
    return {
        ...project,
        ...graph,
    };
}
