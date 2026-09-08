import { useMemo, useRef } from "react";

import { buildNodeGenerationInputs, type NodeGenerationInput } from "@/components/canvas/canvas-node-generation";
import { isFrameNode } from "@/lib/canvas/canvas-frame";
import { sameNodeSemanticData } from "@/lib/canvas/canvas-project-domain";
import { shouldReduceCanvasMediaEffects } from "@/lib/canvas/canvas-performance-mode";
import { createCanvasRenderIndex, resolveCanvasDisplayConnection } from "@/lib/canvas/canvas-render-index";
import { buildCanvasResourceReferencesFromGraph, buildNodeMentionReferencesByNodeId, createCanvasResourceGraph } from "@/lib/canvas/canvas-resource-references";
import { buildSkillMentionReferences } from "@/lib/canvas/canvas-skill-mentions";
import type { PlatformSkill } from "@/services/api/skills";
import type { Asset, ImageAsset } from "@/stores/use-asset-store";
import type { DirectorScene } from "@/types/director";
import { CanvasNodeType, type CanvasConnection, type CanvasMediaPerformanceMode, type CanvasNodeData, type ContextMenuState, type ViewportTransform } from "@/types/canvas";

type UseCanvasRenderModelOptions = {
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
    assets: Asset[];
    viewport: ViewportTransform;
    viewportSize: { width: number; height: number };
    mediaPerformanceMode: CanvasMediaPerformanceMode;
    selectedNodeIds: Set<string>;
    hoveredNodeId: string | null;
    collapsingBatchIds: Set<string>;
    activatedSkills: PlatformSkill[];
    directorScenes?: DirectorScene[];
    toolbarNodeId: string | null;
    infoNodeId: string | null;
    cropNodeId: string | null;
    maskEditNodeId: string | null;
    annotationNodeId: string | null;
    splitNodeId: string | null;
    upscaleNodeId: string | null;
    superResolveNodeId: string | null;
    angleNodeId: string | null;
    emotionNodeId: string | null;
    previewNodeId: string | null;
    contextMenu: ContextMenuState | null;
    versionCompareRootId: string | null;
    directorNodeId: string | null;
    scriptEditorNodeId: string | null;
    dialogNodeId: string | null;
};

type CanvasRenderIndexCache = {
    nodes: readonly CanvasNodeData[];
    connections: readonly CanvasConnection[];
    hiddenNodeIds: ReadonlySet<string>;
    index: ReturnType<typeof createCanvasRenderIndex>;
};

export function useCanvasRenderModel({
    nodes,
    connections,
    assets,
    viewport,
    viewportSize,
    mediaPerformanceMode,
    selectedNodeIds,
    hoveredNodeId,
    collapsingBatchIds,
    activatedSkills,
    directorScenes,
    toolbarNodeId,
    infoNodeId,
    cropNodeId,
    maskEditNodeId,
    annotationNodeId,
    splitNodeId,
    upscaleNodeId,
    superResolveNodeId,
    angleNodeId,
    emotionNodeId,
    previewNodeId,
    contextMenu,
    versionCompareRootId,
    directorNodeId,
    scriptEditorNodeId,
    dialogNodeId,
}: UseCanvasRenderModelOptions) {
    const reduceMediaEffects = useMemo(() => shouldReduceCanvasMediaEffects(mediaPerformanceMode, nodes), [mediaPerformanceMode, nodes]);
    const nodeById = useMemo(() => new Map(nodes.map((node) => [node.id, node])), [nodes]);
    const connectionById = useMemo(() => new Map(connections.map((connection) => [connection.id, connection])), [connections]);
    const collapsedBatchChildIds = useMemo(() => {
        const hidden = new Set<string>();
        nodes.forEach((node) => {
            const rootId = node.metadata?.batchRootId;
            if (!rootId) return;
            const root = nodeById.get(rootId);
            if (root && !root.metadata?.batchExpanded) hidden.add(node.id);
        });
        return hidden;
    }, [nodeById, nodes]);
    const renderHiddenNodeIds = useMemo(() => {
        const hidden = new Set(collapsedBatchChildIds);
        nodes.forEach((node) => {
            const rootId = node.metadata?.batchRootId;
            if (rootId && collapsingBatchIds.has(rootId)) hidden.delete(node.id);
            const parent = node.parentId ? nodeById.get(node.parentId) : null;
            if (parent && isFrameNode(parent) && parent.metadata?.frame?.collapsed) hidden.add(node.id);
        });
        return hidden;
    }, [collapsedBatchChildIds, collapsingBatchIds, nodeById, nodes]);
    const connectionLayerBounds = useMemo(() => {
        if (!nodes.length) return { left: -1, top: -1, width: 2, height: 2 };
        const padding = 320;
        let left = Number.POSITIVE_INFINITY;
        let top = Number.POSITIVE_INFINITY;
        let right = Number.NEGATIVE_INFINITY;
        let bottom = Number.NEGATIVE_INFINITY;
        nodes.forEach((node) => {
            left = Math.min(left, node.position.x - padding);
            top = Math.min(top, node.position.y - padding);
            right = Math.max(right, node.position.x + node.width + padding);
            bottom = Math.max(bottom, node.position.y + node.height + padding);
        });
        return { left, top, width: Math.max(2, right - left), height: Math.max(2, bottom - top) };
    }, [nodes]);
    const renderIndexCacheRef = useRef<CanvasRenderIndexCache | null>(null);
    const renderIndex = useMemo(() => {
        const previous = renderIndexCacheRef.current;
        if (previous && sameRenderGeometry(previous.nodes, nodes) && sameConnectionStructure(previous.connections, connections) && sameStringSet(previous.hiddenNodeIds, renderHiddenNodeIds)) return previous.index;
        const index = createCanvasRenderIndex(nodes, connections, renderHiddenNodeIds);
        renderIndexCacheRef.current = { nodes, connections, hiddenNodeIds: renderHiddenNodeIds, index };
        return index;
    }, [connections, nodes, renderHiddenNodeIds]);
    const renderQuery = useMemo(() => {
        const visible = {
            left: -viewport.x / viewport.k,
            top: -viewport.y / viewport.k,
            right: (-viewport.x + viewportSize.width) / viewport.k,
            bottom: (-viewport.y + viewportSize.height) / viewport.k,
        };
        const nearPadding = (reduceMediaEffects ? Math.max(240, Math.max(viewportSize.width, viewportSize.height) * 0.4) : Math.max(800, Math.max(viewportSize.width, viewportSize.height) * 1.5)) / viewport.k;
        return renderIndex.query({ visible, nearPadding });
    }, [reduceMediaEffects, renderIndex, viewport.k, viewport.x, viewport.y, viewportSize.height, viewportSize.width]);
    const visibleNodes = useMemo(() => renderQuery.nodeIds.flatMap((nodeId) => {
        const node = nodeById.get(nodeId);
        return node ? [node] : [];
    }), [nodeById, renderQuery.nodeIds]);

    const imageAssets = useMemo(() => assets.filter((asset): asset is ImageAsset => asset.kind === "image"), [assets]);
    const canvasImageNodes = useMemo(
        () => nodes.filter((node) => node.type === CanvasNodeType.Image && Boolean(node.metadata?.content) && !collapsedBatchChildIds.has(node.id) && !(node.parentId && nodeById.get(node.parentId)?.metadata?.frame?.collapsed)),
        [collapsedBatchChildIds, nodeById, nodes],
    );
    const semanticNodesRef = useRef(nodes);
    const semanticNodes = useMemo(() => {
        const previous = semanticNodesRef.current;
        const positionOnlyChange = previous.length === nodes.length && nodes.every((node, index) => sameNodeSemanticData(node, previous[index]));
        if (!positionOnlyChange) semanticNodesRef.current = nodes;
        return semanticNodesRef.current;
    }, [nodes]);
    const versionCompareNodes = useMemo(() => {
        if (!versionCompareRootId) return [];
        return nodes.filter((node) => (node.metadata?.versionOfNodeId || node.id) === versionCompareRootId).sort((a, b) => (a.metadata?.versionLabel || "").localeCompare(b.metadata?.versionLabel || ""));
    }, [nodes, versionCompareRootId]);

    const toolbarCandidate = toolbarNodeId ? nodeById.get(toolbarNodeId) || null : null;
    const toolbarNode = isFrameNode(toolbarCandidate) ? null : toolbarCandidate;
    const infoNode = infoNodeId ? nodeById.get(infoNodeId) || null : null;
    const cropNode = cropNodeId ? nodeById.get(cropNodeId) || null : null;
    const maskEditNode = maskEditNodeId ? nodeById.get(maskEditNodeId) || null : null;
    const annotationNode = annotationNodeId ? nodeById.get(annotationNodeId) || null : null;
    const splitNode = splitNodeId ? nodeById.get(splitNodeId) || null : null;
    const upscaleNode = upscaleNodeId ? nodeById.get(upscaleNodeId) || null : null;
    const superResolveNode = superResolveNodeId ? nodeById.get(superResolveNodeId) || null : null;
    const angleNode = angleNodeId ? nodeById.get(angleNodeId) || null : null;
    const emotionNode = emotionNodeId ? nodeById.get(emotionNodeId) || null : null;
    const previewNode = previewNodeId ? nodeById.get(previewNodeId) || null : null;
    const contextMenuNode = contextMenu?.type === "node" ? nodeById.get(contextMenu.nodeId) || null : null;
    const activeNodeId = selectedNodeIds.size > 1 ? null : hoveredNodeId || (selectedNodeIds.size === 1 ? Array.from(selectedNodeIds)[0] : null);

    const selectedNodeBounds = useMemo(() => {
        if (selectedNodeIds.size < 2) return null;
        const selectedNodes = nodes.filter((node) => selectedNodeIds.has(node.id) && !renderHiddenNodeIds.has(node.id));
        if (selectedNodes.length < 2) return null;
        const left = Math.min(...selectedNodes.map((node) => node.position.x));
        const top = Math.min(...selectedNodes.map((node) => node.position.y));
        const right = Math.max(...selectedNodes.map((node) => node.position.x + node.width));
        const bottom = Math.max(...selectedNodes.map((node) => node.position.y + node.height));
        return { left, top, width: right - left, height: bottom - top, count: selectedNodes.length };
    }, [nodes, renderHiddenNodeIds, selectedNodeIds]);
    const selectedVideoNodes = useMemo(
        () =>
            nodes
                .filter((node) => selectedNodeIds.has(node.id) && node.type === CanvasNodeType.Video && Boolean(node.metadata?.content) && !renderHiddenNodeIds.has(node.id))
                .sort((a, b) => {
                    const shotA = a.metadata?.shotIndex ?? Number.MAX_SAFE_INTEGER;
                    const shotB = b.metadata?.shotIndex ?? Number.MAX_SAFE_INTEGER;
                    return shotA - shotB || a.position.y - b.position.y || a.position.x - b.position.x;
                }),
        [nodes, renderHiddenNodeIds, selectedNodeIds],
    );
    const batchChildCountById = useMemo(() => {
        const map = new Map<string, number>();
        nodes.forEach((node) => {
            if (node.metadata?.isBatchRoot) map.set(node.id, node.metadata.batchChildIds?.length || 0);
        });
        return map;
    }, [nodes]);
    const frameChildrenById = useMemo(() => {
        const map = new Map<string, CanvasNodeData[]>();
        nodes.forEach((node) => {
            if (!node.parentId) return;
            const children = map.get(node.parentId) || [];
            children.push(node);
            map.set(node.parentId, children);
        });
        return map;
    }, [nodes]);
    const batchMotionById = useMemo(() => {
        const map = new Map<string, { x: number; y: number; index: number }>();
        nodes.forEach((node) => {
            const rootId = node.metadata?.batchRootId;
            if (!rootId) return;
            const root = nodeById.get(rootId);
            const index = root?.metadata?.batchChildIds?.indexOf(node.id) ?? 0;
            const stackX = root ? root.position.x + 34 + index * 14 : node.position.x;
            const stackY = root ? root.position.y + 14 + index * 8 : node.position.y;
            map.set(node.id, { x: stackX - node.position.x, y: stackY - node.position.y, index: Math.max(index, 0) });
        });
        return map;
    }, [nodeById, nodes]);
    const relatedHighlight = useMemo(() => {
        const nodeIds = new Set<string>();
        const connectionIds = new Set<string>();
        if (!activeNodeId) return { nodeIds, connectionIds };
        nodeIds.add(activeNodeId);
        connections.forEach((connection) => {
            if (connection.fromNodeId !== activeNodeId && connection.toNodeId !== activeNodeId) return;
            connectionIds.add(connection.id);
            nodeIds.add(connection.fromNodeId);
            nodeIds.add(connection.toNodeId);
        });
        return { nodeIds, connectionIds };
    }, [activeNodeId, connections]);
    const displayConnections = useMemo(() => renderQuery.connectionIds.flatMap((connectionId) => {
        const connection = connectionById.get(connectionId);
        if (!connection) return [];
        const displayConnection = resolveCanvasDisplayConnection(connection, nodeById, renderHiddenNodeIds);
        return displayConnection ? [displayConnection] : [];
    }), [connectionById, nodeById, renderHiddenNodeIds, renderQuery.connectionIds]);

    const configInputsById = useMemo(() => {
        const map = new Map<string, NodeGenerationInput[]>();
        semanticNodes.forEach((node) => {
            if (node.type === CanvasNodeType.Config) map.set(node.id, buildNodeGenerationInputs(node.id, semanticNodes, connections));
        });
        return map;
    }, [connections, semanticNodes]);
    const activeDirectorNode = useMemo(() => semanticNodes.find((node) => node.id === directorNodeId) || null, [directorNodeId, semanticNodes]);
    const activeStylePresetId = useMemo(() => semanticNodes.find((node) => node.metadata?.workflowKind === "styleboard")?.metadata?.stylePresetId, [semanticNodes]);
    const activeScriptNode = useMemo(() => semanticNodes.find((node) => node.id === scriptEditorNodeId && node.type === CanvasNodeType.Script) || null, [scriptEditorNodeId, semanticNodes]);
    const activeDirectorScene = useMemo(() => directorScenes?.find((scene) => scene.id === activeDirectorNode?.metadata?.directorSceneId) || null, [activeDirectorNode?.metadata?.directorSceneId, directorScenes]);
    const resourceGraph = useMemo(() => createCanvasResourceGraph(semanticNodes, connections), [connections, semanticNodes]);
    const canvasResourceReferences = useMemo(() => buildCanvasResourceReferencesFromGraph(resourceGraph, dialogNodeId || activeNodeId), [activeNodeId, dialogNodeId, resourceGraph]);
    const resourceReferenceByNodeId = useMemo(() => new Map(canvasResourceReferences.map((reference) => [reference.nodeId, reference])), [canvasResourceReferences]);
    const skillMentionReferences = useMemo(() => buildSkillMentionReferences(activatedSkills), [activatedSkills]);
    const mentionReferencesByNodeId = useMemo(() => {
        const map = buildNodeMentionReferencesByNodeId(semanticNodes, resourceGraph);
        map.forEach((references, nodeId) => map.set(nodeId, [...references, ...skillMentionReferences]));
        return map;
    }, [resourceGraph, semanticNodes, skillMentionReferences]);

    return {
        activeDirectorNode,
        activeDirectorScene,
        activeNodeId,
        activeScriptNode,
        activeStylePresetId,
        angleNode,
        emotionNode,
        annotationNode,
        batchChildCountById,
        batchMotionById,
        canvasImageNodes,
        canvasResourceReferences,
        configInputsById,
        connectionLayerBounds,
        contextMenuNode,
        cropNode,
        displayConnections,
        frameChildrenById,
        imageAssets,
        infoNode,
        maskEditNode,
        mediaTierByNodeId: renderQuery.mediaTierByNodeId,
        mentionReferencesByNodeId,
        nodeById,
        previewNode,
        reduceMediaEffects,
        relatedHighlight,
        resourceReferenceByNodeId,
        selectedNodeBounds,
        selectedVideoNodes,
        semanticNodes,
        skillMentionReferences,
        splitNode,
        superResolveNode,
        toolbarNode,
        upscaleNode,
        versionCompareNodes,
        visibleNodes,
    };
}

function sameRenderGeometry(previous: readonly CanvasNodeData[], current: readonly CanvasNodeData[]) {
    return previous.length === current.length && current.every((node, index) => {
        const candidate = previous[index];
        return candidate.id === node.id && candidate.type === node.type && candidate.parentId === node.parentId && candidate.position.x === node.position.x && candidate.position.y === node.position.y && candidate.width === node.width && candidate.height === node.height;
    });
}

function sameConnectionStructure(previous: readonly CanvasConnection[], current: readonly CanvasConnection[]) {
    return previous.length === current.length && current.every((connection, index) => {
        const candidate = previous[index];
        return candidate.id === connection.id && candidate.fromNodeId === connection.fromNodeId && candidate.toNodeId === connection.toNodeId && candidate.fromHandleId === connection.fromHandleId && candidate.toHandleId === connection.toHandleId;
    });
}

function sameStringSet(previous: ReadonlySet<string>, current: ReadonlySet<string>) {
    return previous.size === current.size && [...current].every((value) => previous.has(value));
}
