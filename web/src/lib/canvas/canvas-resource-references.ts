import { imageReferenceLabel } from "@/lib/image-reference-prompt";
import { seedanceReferenceLabel } from "@/lib/seedance-video";
import { canvasMediaPlaybackUrl } from "@/lib/canvas-media-playback";
import { replaceTextRange, type TextRange } from "@/lib/audio-pause";
import { resourceIdFromStorageKey } from "@/services/api/resources";
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

export type CanvasReferenceManifestEntry = {
    assetKey: string;
    sourceNodeId: string;
    targetNodeId: string;
    edgeId: string;
    mediaType: "image" | "video" | "audio";
    semanticRole: string;
    handle?: string;
    artifactId?: string;
    revisionId?: string;
    resourceId: string;
    resourceUrl: string;
    sourceRevision?: string;
    ordinal: number;
};

export type CanvasReferenceRejection = {
    edgeId: string;
    sourceNodeId: string;
    targetNodeId: string;
    code: "missing_resource_url" | "unsupported_source_type";
    message: string;
};

export type CanvasReferenceManifest = {
    entries: CanvasReferenceManifestEntry[];
    rejections: CanvasReferenceRejection[];
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
    incomingConnectionsByNodeId: Map<string, CanvasConnection[]>;
    configTargetByNodeId: Map<string, string>;
    resourceNodes: CanvasNodeData[];
};

export function createCanvasResourceGraph(nodes: CanvasNodeData[], connections: CanvasConnection[]): CanvasResourceGraph {
    const nodeById = new Map(nodes.map((node) => [node.id, node]));
    const incomingByNodeId = new Map<string, CanvasNodeData[]>();
    const incomingConnectionsByNodeId = new Map<string, CanvasConnection[]>();
    const configTargetByNodeId = new Map<string, string>();
    connections.forEach((connection) => {
        const source = nodeById.get(connection.fromNodeId);
        const target = nodeById.get(connection.toNodeId);
        if (!source || !target) return;
        const incoming = incomingByNodeId.get(target.id) || [];
        incoming.push(source);
        incomingByNodeId.set(target.id, incoming);
        const incomingConnections = incomingConnectionsByNodeId.get(target.id) || [];
        incomingConnections.push(connection);
        incomingConnectionsByNodeId.set(target.id, incomingConnections);
        if (target.type === CanvasNodeType.Config && !configTargetByNodeId.has(source.id)) configTargetByNodeId.set(source.id, target.id);
    });
    return { nodeById, incomingByNodeId, incomingConnectionsByNodeId, configTargetByNodeId, resourceNodes: nodes.filter(isResourceNode) };
}

export function buildCanvasReferenceManifest(nodeId: string, nodes: CanvasNodeData[], connections: CanvasConnection[]): CanvasReferenceManifest {
    return buildCanvasReferenceManifestFromGraph(nodeId, createCanvasResourceGraph(nodes, connections));
}

export function buildCanvasReferenceManifestFromGraph(nodeId: string, graph: CanvasResourceGraph): CanvasReferenceManifest {
    const entries: CanvasReferenceManifestEntry[] = [];
    const rejections: CanvasReferenceRejection[] = [];
    getReferenceConnections(nodeId, graph).forEach((connection) => {
        const source = graph.nodeById.get(connection.fromNodeId);
        if (!source) return;
        if (source.type === CanvasNodeType.Skill) return;
        const mediaType = referenceMediaType(source);
        if (!mediaType) {
            if (source.type === CanvasNodeType.Text) return;
            rejections.push({
                edgeId: connection.id,
                sourceNodeId: source.id,
                targetNodeId: nodeId,
                code: "unsupported_source_type",
                message: `节点“${source.title}”不是可执行的图片、视频或音频素材`,
            });
            return;
        }
        const resourceId = resourceIdFromStorageKey(source.metadata?.storageKey);
        if (!resourceId) {
            rejections.push({
                edgeId: connection.id,
                sourceNodeId: source.id,
                targetNodeId: nodeId,
                code: "missing_resource_url",
                message: `素材“${source.title}”缺少已持久化的 Resource URL`,
            });
            return;
        }
        const handle = connection.toHandleId || connection.fromHandleId;
        entries.push({
            assetKey: source.id,
            sourceNodeId: source.id,
            targetNodeId: nodeId,
            edgeId: connection.id,
            mediaType,
            semanticRole: handle || (source.metadata?.workflowKind === "character" ? "character" : "reference"),
            handle,
            artifactId: source.metadata?.artifactId,
            revisionId: source.metadata?.artifactRevisionId,
            resourceId,
            resourceUrl: canvasMediaPlaybackUrl(source),
            sourceRevision: source.metadata?.artifactRevisionId,
            ordinal: entries.length + 1,
        });
    });
    return { entries, rejections };
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
    return getContextResourceNodes(nodeId, graph);
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

function getReferenceConnections(nodeId: string, graph: CanvasResourceGraph) {
    const configNodeId = graph.configTargetByNodeId.get(nodeId);
    const targetNodeId = configNodeId || nodeId;
    return (graph.incomingConnectionsByNodeId.get(targetNodeId) || []).filter((connection) => connection.fromNodeId !== nodeId);
}

function referenceMediaType(node: CanvasNodeData): CanvasReferenceManifestEntry["mediaType"] | null {
    if (node.metadata?.workflowKind === "character" && node.metadata.characterAssetId) return "image";
    if (node.type === CanvasNodeType.Image) return "image";
    if (node.type === CanvasNodeType.Video) return "video";
    if (node.type === CanvasNodeType.Audio) return "audio";
    return null;
}

export function insertCanvasResourceMention(value: string, selection: TextRange, reference: CanvasResourceReference) {
    const range = normalizedTextRange(value, selection);
    const replacementRange = range.start === range.end ? pendingMentionRange(value, range.start) || range : range;
    const nextCharacter = value[replacementRange.end] || "";
    const separator = nextCharacter && /[\s，。！？、,.!?;:]/u.test(nextCharacter) ? "" : " ";
    return replaceTextRange(value, replacementRange, `${canvasResourceMentionToken(reference)}${separator}`);
}

export function canvasResourceMentionQueryAt(value: string, cursor: number) {
    const range = pendingMentionRange(value, Math.max(0, Math.min(value.length, cursor)));
    return range ? { start: range.start, query: value.slice(range.start + 1, range.end) } : null;
}

export function reconcileCanvasResourceMentions(value: string, references: CanvasResourceReference[]) {
    const activeNodeIds = new Set(references.filter((reference) => reference.active && reference.kind !== "skill").map((reference) => reference.nodeId));
    const mentionPattern = /@\[node:([^\]]+)\]/g;
    let next = "";
    let cursor = 0;
    for (const match of value.matchAll(mentionPattern)) {
        const index = match.index;
        const token = match[0];
        const nodeId = match[1];
        next += value.slice(cursor, index);
        cursor = index + token.length;
        if (activeNodeIds.has(nodeId)) {
            next += token;
            continue;
        }
        if (next.endsWith(" ") && value[cursor] === " ") cursor += 1;
    }
    return next + value.slice(cursor);
}

function normalizedTextRange(value: string, range: TextRange): TextRange {
    const start = Math.max(0, Math.min(value.length, range.start));
    const end = Math.max(start, Math.min(value.length, range.end));
    return { start, end };
}

function pendingMentionRange(value: string, cursor: number): TextRange | null {
    const mentionStart = value.lastIndexOf("@", cursor - 1);
    if (mentionStart < 0) return null;
    const query = value.slice(mentionStart + 1, cursor);
    if (/\s|[@\[\]{}()<>，。！？、,.!?;:]/u.test(query)) return null;
    return { start: mentionStart, end: cursor };
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
