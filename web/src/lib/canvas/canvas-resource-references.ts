import { imageReferenceLabel } from "@/lib/image-reference-prompt";
import { seedanceReferenceLabel } from "@/lib/seedance-video";
import { canvasMediaPlaybackUrl } from "@/lib/canvas-media-playback";
import type { PlatformSkill } from "@/services/api/skills";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "@/types/canvas";

export type CanvasResourceKind = "image" | "video" | "audio" | "text" | "skill" | "character";

export type CanvasResourceReference = {
    id: string;
    nodeId: string;
    kind: CanvasResourceKind;
    label: string;
    title: string;
    previewUrl?: string;
    storageKey?: string;
    text?: string;
    active: boolean;
    sourceType?: CanvasNodeType;
    skill?: PlatformSkill;
};

export function canvasResourceMentionToken(reference: CanvasResourceReference) {
    if (reference.kind === "skill" && reference.skill?.dir) return `@[skill:${reference.skill.dir}]`;
    return `@[node:${reference.nodeId}]`;
}

export function selectVideoReferenceCandidates(references: CanvasResourceReference[], targetNodeId: string) {
    return references.filter((reference) => reference.nodeId !== targetNodeId && Boolean(reference.previewUrl) && (reference.kind === "image" || reference.kind === "video" || reference.kind === "audio"));
}

export type CanvasResourceGraph = {
    nodeById: Map<string, CanvasNodeData>;
    incomingByNodeId: Map<string, CanvasNodeData[]>;
    configTargetByNodeId: Map<string, string>;
    resourceNodes: CanvasNodeData[];
};

export function createCanvasResourceGraph(nodes: CanvasNodeData[], connections: CanvasConnection[]): CanvasResourceGraph {
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const incomingByNodeId = new Map<string, CanvasNodeData[]>();
    const configTargetByNodeId = new Map<string, string>();
    connections.forEach((connection) => {
        const source = nodeById.get(connection.fromNodeId);
        const target = nodeById.get(connection.toNodeId);
        if (!source || !target) return;
        const incoming = incomingByNodeId.get(target.id) || [];
        incoming.push(source);
        incomingByNodeId.set(target.id, incoming);
        if (target.type === CanvasNodeType.Config && !configTargetByNodeId.has(source.id)) configTargetByNodeId.set(source.id, target.id);
    });
    return { nodeById, incomingByNodeId, configTargetByNodeId, resourceNodes: nodes.filter(isResourceNode) };
}

export function buildCanvasResourceReferences(nodes: CanvasNodeData[], connections: CanvasConnection[], contextNodeId?: string | null) {
    return buildCanvasResourceReferencesFromGraph(createCanvasResourceGraph(nodes, connections), contextNodeId);
}

export function buildCanvasResourceReferencesFromGraph(graph: CanvasResourceGraph, contextNodeId?: string | null) {
    const contextNodes = contextNodeId ? getMentionResourceNodes(contextNodeId, graph) : [];
    const globalReferences = labelResourceNodes(graph.resourceNodes, false);
    const activeByNodeId = new Map(labelResourceNodes(contextNodes, true).map((reference) => [reference.nodeId, reference]));
    return globalReferences.map((reference) => activeByNodeId.get(reference.nodeId) || reference);
}

export function buildNodeMentionReferences(node: CanvasNodeData, nodes: CanvasNodeData[], connections: CanvasConnection[]) {
    return buildNodeMentionReferencesFromGraph(node, createCanvasResourceGraph(nodes, connections));
}

export function buildNodeMentionReferencesFromGraph(node: CanvasNodeData, graph: CanvasResourceGraph) {
    return labelResourceNodes(getMentionResourceNodes(node.id, graph), true);
}

export function buildNodeMentionReferencesByNodeId(nodes: CanvasNodeData[], graph: CanvasResourceGraph) {
    return new Map(nodes.map((node) => [node.id, buildNodeMentionReferencesFromGraph(node, graph)]));
}

export function getMentionResourceNodes(nodeId: string, graph: CanvasResourceGraph) {
    const configInputs = getConnectedConfigResourceNodes(nodeId, graph);
    if (configInputs.length) return configInputs;
    const ownInputs = getContextResourceNodes(nodeId, graph);
    if (ownInputs.length) return ownInputs;
    const node = graph.nodeById.get(nodeId);
    return node && isResourceNode(node) ? [node] : [];
}

export function getGenerationResourceNodes(nodeId: string, nodes: CanvasNodeData[], connections: CanvasConnection[]) {
    return getGenerationResourceNodesFromGraph(nodeId, createCanvasResourceGraph(nodes, connections));
}

export function getGenerationResourceNodesFromGraph(nodeId: string, graph: CanvasResourceGraph) {
    const configInputs = getConnectedConfigResourceNodes(nodeId, graph);
    if (configInputs.length) return configInputs;
    const ownInputs = getContextResourceNodes(nodeId, graph);
    if (ownInputs.length) return ownInputs;
    return [];
}

function getContextResourceNodes(nodeId: string, graph: CanvasResourceGraph) {
    return (graph.incomingByNodeId.get(nodeId) || []).filter(isResourceNode);
}

function getConnectedConfigResourceNodes(nodeId: string, graph: CanvasResourceGraph) {
    const configNodeId = graph.configTargetByNodeId.get(nodeId);
    if (!configNodeId) return [];
    return getContextResourceNodes(configNodeId, graph).filter((node) => node.id !== nodeId);
}

function labelResourceNodes(nodes: CanvasNodeData[], active: boolean) {
    const counts: Record<CanvasResourceKind, number> = { image: 0, video: 0, audio: 0, text: 0, skill: 0, character: 0 };
    return nodes.flatMap((node): CanvasResourceReference[] => {
        const kind = resourceKind(node);
        if (!kind) return [];
        const index = counts[kind]++;
        const label = labelForKind(kind, index);
        return [
            {
                id: node.id,
                nodeId: node.id,
                kind,
                label,
                title: node.title || label,
                previewUrl: resourcePreviewUrl(node, kind),
                storageKey: node.metadata?.storageKey,
                text:
                    node.metadata?.workflowKind === "character"
                        ? node.metadata.characterPrompt
                        : node.type === CanvasNodeType.Text
                          ? node.metadata?.content || node.metadata?.prompt
                          : node.type === CanvasNodeType.Skill
                            ? skillResourceText(node)
                            : undefined,
                active,
                sourceType: node.type,
            },
        ];
    });
}

function resourcePreviewUrl(node: CanvasNodeData, kind: CanvasResourceKind) {
    if (kind === "character") return node.metadata?.characterCoverUrl;
    if (kind === "image" || kind === "video" || kind === "audio") return canvasMediaPlaybackUrl(node);
    return node.metadata?.content;
}

function labelForKind(kind: CanvasResourceKind, index: number) {
    if (kind === "character") return `角色${index + 1}`;
    if (kind === "image") return imageReferenceLabel(index);
    if (kind === "video") return seedanceReferenceLabel("video", index);
    if (kind === "audio") return seedanceReferenceLabel("audio", index);
    if (kind === "skill") return `技能${index + 1}`;
    return `文本${index + 1}`;
}

function isResourceNode(node: CanvasNodeData) {
    return Boolean(resourceKind(node));
}

function resourceKind(node: CanvasNodeData): CanvasResourceKind | null {
    if (node.metadata?.workflowKind === "character" && node.metadata.characterAssetId) return "character";
    if (node.type === CanvasNodeType.Image && node.metadata?.content) return "image";
    if (node.type === CanvasNodeType.Video && node.metadata?.content) return "video";
    if (node.type === CanvasNodeType.Audio && node.metadata?.content) return "audio";
    if (node.type === CanvasNodeType.Text && (node.metadata?.content || node.metadata?.prompt)) return "text";
    if (node.type === CanvasNodeType.Skill && (node.metadata?.skillSnapshot || node.metadata?.content)) return "text";
    return null;
}

function skillResourceText(node: CanvasNodeData) {
    const skill = node.metadata?.skillSnapshot;
    if (!skill) return node.metadata?.content || "";
    return [skill.name, skill.description, skill.template, skill.outputContract].filter(Boolean).join("\n\n");
}
