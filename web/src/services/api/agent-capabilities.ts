import { array, boundedText, exactObject, flag, integer, object, text } from "./strict-contract";

export type AgentToolName = "canvas.read" | "canvas.apply_ops" | "assets.read" | "assets.publish" | "project.read" | "project.apply_ops" | "media.generate" | "vision.analyze" | "skills.load";

export type AgentCanvasPoint = { x: number; y: number };
export type AgentCanvasViewport = AgentCanvasPoint & { zoom: number };
export type AgentCanvasNodeType = "image" | "text" | "script" | "skill" | "config" | "video" | "audio" | "frame";
export type AgentCanvasNodeStatus = "idle" | "success" | "loading" | "error";
export type AgentCanvasOperation =
    | { operationId: string; type: "add_node"; node: { id: string; type: AgentCanvasNodeType; title: string; position: AgentCanvasPoint; width: number; height: number; metadata?: Record<string, unknown> } }
    | { operationId: string; type: "update_node"; nodeId: string; patch: Record<string, unknown> }
    | { operationId: string; type: "delete_node"; nodeId: string }
    | { operationId: string; type: "connect_nodes"; connection: { id: string; fromNodeId: string; toNodeId: string; fromHandleId?: string; toHandleId?: string } }
    | { operationId: string; type: "delete_connections"; connectionIds: string[] }
    | { operationId: string; type: "set_viewport"; viewport: AgentCanvasViewport }
    | { operationId: string; type: "select_nodes"; nodeIds: string[] };

export type AgentProjectCharacterCandidate = {
    name: string;
    aliases: string[];
    role: string;
    appearance: string;
    clothing: string;
    physique: string;
    personality: string;
    props: string;
    consistencyPrompt: string;
    multiViewPrompt: string;
    voiceLanguage: string;
    voiceAge: string;
    voiceTimbre: string;
};

export type AgentProjectOperation =
    | { operationId: string; type: "create_character_candidates"; unitId: string; candidates: AgentProjectCharacterCandidate[] }
    | { operationId: string; type: "replace_unit_shots"; unitId: string; shots: Array<{ title: string; description: string; durationMs: number }> };

export type AgentCapabilityArguments =
    | { canvasId: string; selectedNodeIds: string[]; includeViewport: boolean }
    | { canvasId: string; baseRevision: number; clientMutationId: string; operations: AgentCanvasOperation[] }
    | { domainProjectId: string; resourceIds: string[]; limit: number }
    | { resourceId: string; domainProjectId: string; displayName: string; clientMutationId: string }
    | { domainProjectId: string; canvasId: string }
    | { domainProjectId: string; baseRevision: number; clientMutationId: string; operations: AgentProjectOperation[] }
    | { mediaKind: "image" | "video" | "audio"; modelRecordId: string; modelKey: string; parameters: Record<string, unknown>; sourceResourceIds: string[]; targetCanvasNodeId: string; clientRequestId: string }
    | { modelRecordId: string; modelKey: string; sourceResourceIds: string[]; prompt: string; detail: "low" | "original"; clientRequestId: string }
    | { skillDir: string; version: number; checksum: string };

export type AgentVisionAnalyzeResult = {
    taskId: string;
    billingOrderId: string;
    modelRecordId: string;
    modelKey: string;
    clientRequestId: string;
    sourceResourceIds: string[];
    detail: "low" | "original";
    analysis: string;
    usage: { inputTokens: number; cachedTokens: number; outputTokens: number };
};

export type AgentMediaGenerateResult = {
    taskId: string;
    billingOrderId: string;
    mediaKind: "image" | "video" | "audio";
    clientRequestId: string;
    resources: Array<{ resourceId: string; kind: "image" | "video" | "audio"; url: string }>;
};

const toolNames = new Set<AgentToolName>(["canvas.read", "canvas.apply_ops", "assets.read", "assets.publish", "project.read", "project.apply_ops", "media.generate", "vision.analyze", "skills.load"]);

export function isAgentToolName(value: unknown): value is AgentToolName {
    return typeof value === "string" && toolNames.has(value as AgentToolName);
}

export function parseAgentCapabilityArguments(toolName: string, value: unknown): AgentCapabilityArguments {
    if (!isAgentToolName(toolName)) throw new Error(`不受支持的 Agent 工具: ${toolName}`);
    if (toolName === "canvas.read") {
        const source = exactObject(value, "canvas.read arguments", ["canvasId", "selectedNodeIds", "includeViewport"]);
        return {
            canvasId: capabilityIdentifier(source.canvasId, "canvasId"),
            selectedNodeIds: capabilityIdentifierList(source.selectedNodeIds, "selectedNodeIds", 100, true),
            includeViewport: flag(source.includeViewport, "includeViewport"),
        };
    }
    if (toolName === "canvas.apply_ops") {
        const source = exactObject(value, "canvas.apply_ops arguments", ["canvasId", "baseRevision", "clientMutationId", "operations"]);
        const rawOperations = array(source.operations, "operations");
        if (rawOperations.length < 1 || rawOperations.length > 100) throw new Error("operations 数量必须在 1 到 100 之间");
        const operations = rawOperations.map((operation, index) => parseCanvasOperation(operation, index));
        const operationIds = new Set(operations.map((operation) => operation.operationId));
        if (operationIds.size !== operations.length) throw new Error("operationId 不能重复");
        return {
            canvasId: capabilityIdentifier(source.canvasId, "canvasId"),
            baseRevision: integer(source.baseRevision, "baseRevision", true),
            clientMutationId: capabilityIdentifier(source.clientMutationId, "clientMutationId"),
            operations,
        };
    }
    if (toolName === "assets.read") {
        const source = exactObject(value, "assets.read arguments", ["domainProjectId", "resourceIds", "limit"]);
        const limit = integer(source.limit, "limit");
        if (limit > 100) throw new Error("limit 不能超过 100");
        return {
            domainProjectId: capabilityIdentifier(source.domainProjectId, "domainProjectId"),
            resourceIds: capabilityResourceIdentifierList(source.resourceIds, "resourceIds", 100, true),
            limit,
        };
    }
    if (toolName === "assets.publish") {
        const source = exactObject(value, "assets.publish arguments", ["resourceId", "domainProjectId", "displayName", "clientMutationId"]);
        return {
            resourceId: capabilityResourceIdentifier(source.resourceId, "resourceId"),
            domainProjectId: capabilityIdentifier(source.domainProjectId, "domainProjectId"),
            displayName: boundedText(source.displayName, "displayName", 240),
            clientMutationId: capabilityIdentifier(source.clientMutationId, "clientMutationId"),
        };
    }
    if (toolName === "project.read") {
        const source = exactObject(value, "project.read arguments", ["domainProjectId", "canvasId"]);
        return {
            domainProjectId: capabilityIdentifier(source.domainProjectId, "domainProjectId"),
            canvasId: capabilityIdentifier(source.canvasId, "canvasId"),
        };
    }
    if (toolName === "project.apply_ops") {
        const source = exactObject(value, "project.apply_ops arguments", ["domainProjectId", "baseRevision", "clientMutationId", "operations"]);
        const rawOperations = array(source.operations, "operations");
        if (rawOperations.length < 1 || rawOperations.length > 100) throw new Error("operations 数量必须在 1 到 100 之间");
        const operations = rawOperations.map((operation, index) => parseProjectOperation(operation, index));
        if (new Set(operations.map((operation) => operation.operationId)).size !== operations.length) throw new Error("operationId 不能重复");
        const unitOperationKeys = operations.map((operation) => `${operation.unitId}\u0000${operation.type}`);
        if (new Set(unitOperationKeys).size !== unitOperationKeys.length) throw new Error("同一章节不能重复声明相同项目操作");
        return {
            domainProjectId: capabilityIdentifier(source.domainProjectId, "domainProjectId"),
            baseRevision: integer(source.baseRevision, "baseRevision", true),
            clientMutationId: capabilityIdentifier(source.clientMutationId, "clientMutationId"),
            operations,
        };
    }
    if (toolName === "media.generate") {
        const source = exactObject(value, "media.generate arguments", ["mediaKind", "modelRecordId", "modelKey", "parameters", "sourceResourceIds", "targetCanvasNodeId", "clientRequestId"]);
        if (source.mediaKind !== "image" && source.mediaKind !== "video" && source.mediaKind !== "audio") {
            throw new Error(`不受支持的媒体能力: ${String(source.mediaKind)}`);
        }
        return {
            mediaKind: source.mediaKind,
            modelRecordId: capabilityIdentifier(source.modelRecordId, "modelRecordId"),
            modelKey: boundedText(source.modelKey, "modelKey", 120),
            parameters: { ...object(source.parameters, "parameters") },
            sourceResourceIds: capabilityResourceIdentifierList(source.sourceResourceIds, "sourceResourceIds", 100, true),
            targetCanvasNodeId: capabilityIdentifier(source.targetCanvasNodeId, "targetCanvasNodeId"),
            clientRequestId: capabilityIdentifier(source.clientRequestId, "clientRequestId"),
        };
    }
    if (toolName === "vision.analyze") {
        const source = exactObject(value, "vision.analyze arguments", ["modelRecordId", "modelKey", "sourceResourceIds", "prompt", "detail", "clientRequestId"]);
        if (source.detail !== "low" && source.detail !== "original") throw new Error(`不受支持的视觉细节等级: ${String(source.detail)}`);
        return {
            modelRecordId: capabilityIdentifier(source.modelRecordId, "modelRecordId"),
            modelKey: boundedText(source.modelKey, "modelKey", 120),
            sourceResourceIds: capabilityResourceIdentifierList(source.sourceResourceIds, "sourceResourceIds", 12, false),
            prompt: boundedText(source.prompt, "prompt", 64 * 1024),
            detail: source.detail,
            clientRequestId: capabilityIdentifier(source.clientRequestId, "clientRequestId"),
        };
    }
    const source = exactObject(value, "skills.load arguments", ["skillDir", "version", "checksum"]);
    const checksum = text(source.checksum, "checksum");
    if (!validCapabilityChecksum(checksum)) throw new Error("checksum 必须是 64 位小写 SHA-256");
    return {
        skillDir: boundedText(source.skillDir, "skillDir", 240),
        version: integer(source.version, "version"),
        checksum,
    };
}

function parseProjectOperation(value: unknown, index: number): AgentProjectOperation {
    const header = object(value, `operations[${index}]`);
    if (header.type === "create_character_candidates") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "unitId", "candidates"]);
        const rawCandidates = array(source.candidates, `operations[${index}].candidates`);
        if (rawCandidates.length < 1 || rawCandidates.length > 100) throw new Error(`operations[${index}].candidates 数量无效`);
        const candidates = rawCandidates.map((candidate, candidateIndex) => parseProjectCharacterCandidate(candidate, `${index}.candidates[${candidateIndex}]`));
        if (new Set(candidates.map((candidate) => candidate.name)).size !== candidates.length) throw new Error(`operations[${index}].candidates 角色名不能重复`);
        return {
            operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`),
            type: header.type,
            unitId: capabilityIdentifier(source.unitId, `operations[${index}].unitId`),
            candidates,
        };
    }
    if (header.type === "replace_unit_shots") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "unitId", "shots"]);
        const rawShots = array(source.shots, `operations[${index}].shots`);
        if (rawShots.length < 1 || rawShots.length > 200) throw new Error(`operations[${index}].shots 数量无效`);
        return {
            operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`),
            type: header.type,
            unitId: capabilityIdentifier(source.unitId, `operations[${index}].unitId`),
            shots: rawShots.map((shot, shotIndex) => {
                const parsed = exactObject(shot, `operations[${index}].shots[${shotIndex}]`, ["title", "description", "durationMs"]);
                const durationMs = integer(parsed.durationMs, `operations[${index}].shots[${shotIndex}].durationMs`);
                if (durationMs < 1_000 || durationMs > 30_000) throw new Error(`operations[${index}].shots[${shotIndex}].durationMs 必须在 1000 到 30000 之间`);
                return {
                    title: boundedText(parsed.title, `operations[${index}].shots[${shotIndex}].title`, 240),
                    description: boundedText(parsed.description, `operations[${index}].shots[${shotIndex}].description`, 8 * 1024),
                    durationMs,
                };
            }),
        };
    }
    throw new Error(`不受支持的项目操作: ${String(header.type)}`);
}

function parseProjectCharacterCandidate(value: unknown, label: string): AgentProjectCharacterCandidate {
    const source = exactObject(value, label, [
        "name", "aliases", "role", "appearance", "clothing", "physique", "personality", "props",
        "consistencyPrompt", "multiViewPrompt", "voiceLanguage", "voiceAge", "voiceTimbre",
    ]);
    const name = boundedText(source.name, `${label}.name`, 240);
    const aliases = array(source.aliases, `${label}.aliases`).map((alias, index) => boundedText(alias, `${label}.aliases[${index}]`, 240));
    if (aliases.length > 20 || new Set(aliases).size !== aliases.length || aliases.includes(name)) throw new Error(`${label}.aliases 无效`);
    const required = (field: keyof Omit<AgentProjectCharacterCandidate, "name" | "aliases" | "props">) => boundedText(source[field], `${label}.${field}`, 2 * 1024);
    return {
        name,
        aliases,
        role: required("role"),
        appearance: required("appearance"),
        clothing: required("clothing"),
        physique: required("physique"),
        personality: required("personality"),
        props: boundedText(source.props, `${label}.props`, 2 * 1024, true),
        consistencyPrompt: required("consistencyPrompt"),
        multiViewPrompt: required("multiViewPrompt"),
        voiceLanguage: required("voiceLanguage"),
        voiceAge: required("voiceAge"),
        voiceTimbre: required("voiceTimbre"),
    };
}

export function parseAgentCapabilityResult(toolName: "vision.analyze", value: unknown): AgentVisionAnalyzeResult;
export function parseAgentCapabilityResult(toolName: "media.generate", value: unknown): AgentMediaGenerateResult;
export function parseAgentCapabilityResult(toolName: AgentToolName, value: unknown): Record<string, unknown>;
export function parseAgentCapabilityResult(toolName: string, value: unknown): Record<string, unknown> {
    if (!isAgentToolName(toolName)) throw new Error(`不受支持的 Agent 工具: ${toolName}`);
    if (toolName === "media.generate") {
        const source = exactObject(value, "media.generate result", ["taskId", "billingOrderId", "mediaKind", "clientRequestId", "resources"]);
        if (source.mediaKind !== "image" && source.mediaKind !== "video" && source.mediaKind !== "audio") {
            throw new Error(`media.generate result.mediaKind 不受支持: ${String(source.mediaKind)}`);
        }
        const rawResources = array(source.resources, "media.generate result.resources");
        if (rawResources.length < 1 || rawResources.length > 100) throw new Error("media.generate result.resources 数量无效");
        const resources = rawResources.map((value, index) => {
            const resource = exactObject(value, `media.generate result.resources[${index}]`, ["resourceId", "kind", "url"]);
            const resourceId = capabilityResourceIdentifier(resource.resourceId, `media.generate result.resources[${index}].resourceId`);
            if (resource.kind !== source.mediaKind) throw new Error(`media.generate result.resources[${index}].kind 与 mediaKind 不一致`);
            const expectedURL = `/api/resources/${resourceId}/file`;
            if (resource.url !== expectedURL) throw new Error(`media.generate result.resources[${index}].url 无效`);
            return { resourceId, kind: source.mediaKind, url: expectedURL };
        });
        if (new Set(resources.map((resource) => resource.resourceId)).size !== resources.length) {
            throw new Error("media.generate result.resources 不能包含重复资源");
        }
        return {
            taskId: capabilityIdentifier(source.taskId, "media.generate result.taskId"),
            billingOrderId: capabilityIdentifier(source.billingOrderId, "media.generate result.billingOrderId"),
            mediaKind: source.mediaKind,
            clientRequestId: capabilityIdentifier(source.clientRequestId, "media.generate result.clientRequestId"),
            resources,
        };
    }
    if (toolName !== "vision.analyze") return { ...object(value, `${toolName} result`) };

    const source = exactObject(value, "vision.analyze result", ["taskId", "billingOrderId", "modelRecordId", "modelKey", "clientRequestId", "sourceResourceIds", "detail", "analysis", "usage"]);
    if (source.detail !== "low" && source.detail !== "original") throw new Error(`不受支持的视觉细节等级: ${String(source.detail)}`);
    const usageSource = exactObject(source.usage, "vision.analyze result.usage", ["inputTokens", "cachedTokens", "outputTokens"]);
    const inputTokens = integer(usageSource.inputTokens, "vision.analyze result.usage.inputTokens", true);
    const cachedTokens = integer(usageSource.cachedTokens, "vision.analyze result.usage.cachedTokens", true);
    const outputTokens = integer(usageSource.outputTokens, "vision.analyze result.usage.outputTokens", true);
    if (cachedTokens > inputTokens) throw new Error("vision.analyze result.usage.cachedTokens 不能超过 inputTokens");
    return {
        taskId: capabilityIdentifier(source.taskId, "taskId"),
        billingOrderId: capabilityIdentifier(source.billingOrderId, "billingOrderId"),
        modelRecordId: capabilityIdentifier(source.modelRecordId, "modelRecordId"),
        modelKey: boundedText(source.modelKey, "modelKey", 120),
        clientRequestId: capabilityIdentifier(source.clientRequestId, "clientRequestId"),
        sourceResourceIds: capabilityResourceIdentifierList(source.sourceResourceIds, "sourceResourceIds", 12, false),
        detail: source.detail,
        analysis: boundedText(source.analysis, "analysis", 64 * 1024),
        usage: { inputTokens, cachedTokens, outputTokens },
    };
}

function parseCanvasOperation(value: unknown, index: number): AgentCanvasOperation {
    const header = object(value, `operations[${index}]`);
    const type = header.type;
    if (type === "add_node") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "node"]);
        const node = exactObject(source.node, `operations[${index}].node`, ["id", "type", "title", "position", "width", "height", "metadata"]);
        const result: Extract<AgentCanvasOperation, { type: "add_node" }> = {
            operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`),
            type,
            node: {
                id: capabilityIdentifier(node.id, `operations[${index}].node.id`),
                type: capabilityCanvasNodeType(node.type, `operations[${index}].node.type`),
                title: boundedText(node.title, `operations[${index}].node.title`, 240),
                position: capabilityPoint(node.position, `operations[${index}].node.position`),
                width: capabilityDimension(node.width, `operations[${index}].node.width`),
                height: capabilityDimension(node.height, `operations[${index}].node.height`),
            },
        };
        if (node.metadata !== undefined) result.node.metadata = capabilityCanvasNodeMetadata(node.metadata, `operations[${index}].node.metadata`);
        return result;
    }
    if (type === "update_node") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "nodeId", "patch"]);
        const patch = object(source.patch, `operations[${index}].patch`);
        if (Object.keys(patch).length === 0) throw new Error(`operations[${index}].patch 不能为空`);
        if (patch.type !== undefined) capabilityCanvasNodeType(patch.type, `operations[${index}].patch.type`);
        if (patch.metadata !== undefined) capabilityCanvasNodeMetadata(patch.metadata, `operations[${index}].patch.metadata`);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, nodeId: capabilityIdentifier(source.nodeId, `operations[${index}].nodeId`), patch: { ...patch } };
    }
    if (type === "delete_node") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "nodeId"]);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, nodeId: capabilityIdentifier(source.nodeId, `operations[${index}].nodeId`) };
    }
    if (type === "connect_nodes") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "connection"]);
        const connection = exactObject(source.connection, `operations[${index}].connection`, ["id", "fromNodeId", "toNodeId", "fromHandleId", "toHandleId"]);
        const fromNodeId = capabilityIdentifier(connection.fromNodeId, `operations[${index}].connection.fromNodeId`);
        const toNodeId = capabilityIdentifier(connection.toNodeId, `operations[${index}].connection.toNodeId`);
        if (fromNodeId === toNodeId) throw new Error(`operations[${index}].connection 不能连接同一节点`);
        const parsedConnection: Extract<AgentCanvasOperation, { type: "connect_nodes" }>["connection"] = {
            id: capabilityIdentifier(connection.id, `operations[${index}].connection.id`),
            fromNodeId,
            toNodeId,
        };
        if (connection.fromHandleId !== undefined) parsedConnection.fromHandleId = capabilityIdentifier(connection.fromHandleId, `operations[${index}].connection.fromHandleId`);
        if (connection.toHandleId !== undefined) parsedConnection.toHandleId = capabilityIdentifier(connection.toHandleId, `operations[${index}].connection.toHandleId`);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, connection: parsedConnection };
    }
    if (type === "delete_connections") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "connectionIds"]);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, connectionIds: capabilityIdentifierList(source.connectionIds, `operations[${index}].connectionIds`, 100, false) };
    }
    if (type === "set_viewport") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "viewport"]);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, viewport: capabilityViewport(source.viewport, `operations[${index}].viewport`) };
    }
    if (type === "select_nodes") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "nodeIds"]);
        return { operationId: capabilityIdentifier(source.operationId, `operations[${index}].operationId`), type, nodeIds: capabilityIdentifierList(source.nodeIds, `operations[${index}].nodeIds`, 100, true) };
    }
    throw new Error(`不受支持的画布操作: ${String(type)}`);
}

function capabilityCanvasNodeType(value: unknown, label: string): AgentCanvasNodeType {
    const result = boundedText(value, label, 64);
    switch (result) {
        case "image":
        case "text":
        case "script":
        case "skill":
        case "config":
        case "video":
        case "audio":
        case "frame":
            return result;
        default:
            throw new Error(`${label} 不受支持`);
    }
}

function capabilityCanvasNodeStatus(value: unknown, label: string): AgentCanvasNodeStatus {
    const result = boundedText(value, label, 32);
    switch (result) {
        case "idle":
        case "success":
        case "loading":
        case "error":
            return result;
        default:
            throw new Error(`${label} 不受支持`);
    }
}

function capabilityCanvasNodeMetadata(value: unknown, label: string): Record<string, unknown> {
    const metadata = object(value, label);
    if (metadata.status !== undefined) capabilityCanvasNodeStatus(metadata.status, `${label}.status`);
    return { ...metadata };
}

function capabilityIdentifier(value: unknown, label: string): string {
    const result = boundedText(value, label, 120);
    if (Array.from(result).some((character) => character.trim() === "" || character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) throw new Error(`${label} 不是合法标识符`);
    return result;
}

function capabilityResourceIdentifier(value: unknown, label: string): string {
    const result = capabilityIdentifier(value, label);
    let absolute = false;
    try {
        absolute = Boolean(new URL(result).protocol);
    } catch {
        absolute = false;
    }
    if (absolute) throw new Error(`${label} 必须是资源标识，不能是 URL`);
    return result;
}

function capabilityIdentifierList(value: unknown, label: string, limit: number, allowEmpty: boolean): string[] {
    const values = array(value, label);
    if (values.length > limit || (!allowEmpty && values.length === 0)) throw new Error(`${label} 数量无效`);
    const result = values.map((item, index) => capabilityIdentifier(item, `${label}[${index}]`));
    if (new Set(result).size !== result.length) throw new Error(`${label} 不能包含重复标识`);
    return result;
}

function capabilityResourceIdentifierList(value: unknown, label: string, limit: number, allowEmpty: boolean): string[] {
    const values = array(value, label);
    if (values.length > limit || (!allowEmpty && values.length === 0)) throw new Error(`${label} 数量无效`);
    const result = values.map((item, index) => capabilityResourceIdentifier(item, `${label}[${index}]`));
    if (new Set(result).size !== result.length) throw new Error(`${label} 不能包含重复标识`);
    return result;
}

function capabilityNumber(value: unknown, label: string): number {
    if (typeof value !== "number" || !Number.isFinite(value)) throw new Error(`${label} 必须是有限数值`);
    return value;
}

function capabilityCoordinate(value: unknown, label: string): number {
    const result = capabilityNumber(value, label);
    if (result < -1_000_000 || result > 1_000_000) throw new Error(`${label} 超出画布坐标范围`);
    return result;
}

function capabilityDimension(value: unknown, label: string): number {
    const result = capabilityNumber(value, label);
    if (result < 1 || result > 20_000) throw new Error(`${label} 超出画布尺寸范围`);
    return result;
}

function capabilityPoint(value: unknown, label: string): AgentCanvasPoint {
    const source = exactObject(value, label, ["x", "y"]);
    return { x: capabilityCoordinate(source.x, `${label}.x`), y: capabilityCoordinate(source.y, `${label}.y`) };
}

function capabilityViewport(value: unknown, label: string): AgentCanvasViewport {
    const source = exactObject(value, label, ["x", "y", "zoom"]);
    const zoom = capabilityNumber(source.zoom, `${label}.zoom`);
    if (zoom < 0.01 || zoom > 16) throw new Error(`${label}.zoom 超出范围`);
    return { x: capabilityCoordinate(source.x, `${label}.x`), y: capabilityCoordinate(source.y, `${label}.y`), zoom };
}

function validCapabilityChecksum(value: string): boolean {
    return value.length === 64 && Array.from(value).every((character) => (character >= "0" && character <= "9") || (character >= "a" && character <= "f"));
}
