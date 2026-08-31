import { array, boundedText, exactObject, flag, integer, object, text } from "./strict-contract";

export type AgentToolName = "canvas.read" | "canvas.apply_ops" | "assets.read" | "assets.publish" | "media.generate" | "skills.load";

export type AgentCanvasPoint = { x: number; y: number };
export type AgentCanvasViewport = AgentCanvasPoint & { zoom: number };
export type AgentCanvasOperation =
    | { operationId: string; type: "add_node"; node: { id: string; type: string; title: string; position: AgentCanvasPoint; width: number; height: number; metadata?: Record<string, unknown> } }
    | { operationId: string; type: "update_node"; nodeId: string; patch: Record<string, unknown> }
    | { operationId: string; type: "delete_node"; nodeId: string }
    | { operationId: string; type: "connect_nodes"; connection: { id: string; fromNodeId: string; toNodeId: string; fromHandleId?: string; toHandleId?: string } }
    | { operationId: string; type: "delete_connections"; connectionIds: string[] }
    | { operationId: string; type: "set_viewport"; viewport: AgentCanvasViewport }
    | { operationId: string; type: "select_nodes"; nodeIds: string[] };

export type AgentCapabilityArguments =
    | { canvasId: string; selectedNodeIds: string[]; includeViewport: boolean }
    | { canvasId: string; baseRevision: number; clientMutationId: string; operations: AgentCanvasOperation[] }
    | { domainProjectId: string; resourceIds: string[]; limit: number }
    | { resourceId: string; domainProjectId: string; displayName: string; clientMutationId: string }
    | { mediaKind: "image" | "video" | "audio"; modelRecordId: string; modelKey: string; parameters: Record<string, unknown>; sourceResourceIds: string[]; targetCanvasNodeId: string; clientRequestId: string }
    | { skillDir: string; version: number; checksum: string };

const toolNames = new Set<AgentToolName>(["canvas.read", "canvas.apply_ops", "assets.read", "assets.publish", "media.generate", "skills.load"]);

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
    const source = exactObject(value, "skills.load arguments", ["skillDir", "version", "checksum"]);
    const checksum = text(source.checksum, "checksum");
    if (!validCapabilityChecksum(checksum)) throw new Error("checksum 必须是 64 位小写 SHA-256");
    return {
        skillDir: boundedText(source.skillDir, "skillDir", 240),
        version: integer(source.version, "version"),
        checksum,
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
                type: boundedText(node.type, `operations[${index}].node.type`, 64),
                title: boundedText(node.title, `operations[${index}].node.title`, 240),
                position: capabilityPoint(node.position, `operations[${index}].node.position`),
                width: capabilityDimension(node.width, `operations[${index}].node.width`),
                height: capabilityDimension(node.height, `operations[${index}].node.height`),
            },
        };
        if (node.metadata !== undefined) result.node.metadata = { ...object(node.metadata, `operations[${index}].node.metadata`) };
        return result;
    }
    if (type === "update_node") {
        const source = exactObject(value, `operations[${index}]`, ["operationId", "type", "nodeId", "patch"]);
        const patch = object(source.patch, `operations[${index}].patch`);
        if (Object.keys(patch).length === 0) throw new Error(`operations[${index}].patch 不能为空`);
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
