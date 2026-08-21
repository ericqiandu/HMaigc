import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { App } from "antd";
import { nanoid } from "nanoid";

import type { CanvasBackgroundMode } from "@/lib/canvas-theme";
import {
    applyCanvasMutationPatch,
    diffCanvasCollaborationDocument,
    isEmptyCanvasMutationPatch,
    type CanvasCollaborationDocument,
} from "@/lib/canvas/canvas-collaboration-document";
import {
    canvasCollaborationSocketURL,
    getCanvasCollaboration,
    type CanvasAccess,
    type CanvasCollaborationState,
    type CanvasMutationPatch,
    type CanvasMutationResult,
    type CanvasPresence,
    type CanvasRealtimeEnvelope,
} from "@/services/api/canvas-collaboration";
import { useCanvasStore, type CanvasProject } from "@/stores/canvas/use-canvas-store";
import { removeRetiredCanvasNodes } from "@/lib/canvas/canvas-retired-content-migration";
import type { CanvasConnection, CanvasNodeData, Position } from "@/types/canvas";
import type { DirectorScene } from "@/types/director";
import { canvasUsesRevisionedMutations } from "@/lib/canvas/canvas-persistence-policy";
import { requireCanvasCollaborationRevision, requireEditableCanvasCollaboration } from "@/lib/canvas/canvas-collaboration-preflight";

export type CanvasCollaborationConnectionStatus = "personal" | "connecting" | "online" | "reconnecting" | "readonly" | "error";

type UseCanvasCollaborationOptions = {
    projectId: string;
    projectLoaded: boolean;
    project?: CanvasProject;
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
    backgroundMode: CanvasBackgroundMode;
    showImageInfo: boolean;
    selectedNodeIds: Set<string>;
    setNodes: Dispatch<SetStateAction<CanvasNodeData[]>>;
    setConnections: Dispatch<SetStateAction<CanvasConnection[]>>;
    setBackgroundMode: Dispatch<SetStateAction<CanvasBackgroundMode>>;
    setShowImageInfo: Dispatch<SetStateAction<boolean>>;
};

type InFlightMutation = {
    id: string;
    patch: CanvasMutationPatch;
    target: CanvasCollaborationDocument;
};

const MUTATION_DEBOUNCE_MS = 450;
const PRESENCE_DEBOUNCE_MS = 50;
const MAX_RECONNECT_DELAY_MS = 30_000;

export function useCanvasCollaboration({
    projectId,
    projectLoaded,
    project,
    nodes,
    connections,
    backgroundMode,
    showImageInfo,
    selectedNodeIds,
    setNodes,
    setConnections,
    setBackgroundMode,
    setShowImageInfo,
}: UseCanvasCollaborationOptions) {
    const { message } = App.useApp();
    const updateProject = useCanvasStore((state) => state.updateProject);
    const enabled = canvasUsesRevisionedMutations(projectLoaded, project?.id);
    const [access, setAccess] = useState<CanvasAccess | null>(null);
    const [status, setStatus] = useState<CanvasCollaborationConnectionStatus>(enabled ? "connecting" : "personal");
    const [presenceByConnection, setPresenceByConnection] = useState<Record<string, CanvasPresence>>({});
    const [managementState, setManagementState] = useState<CanvasCollaborationState | null>(null);
    const socketRef = useRef<WebSocket | null>(null);
    const reconnectTimerRef = useRef<number | null>(null);
    const reconnectAttemptRef = useRef(0);
    const disposedRef = useRef(false);
    const baseDocumentRef = useRef<CanvasCollaborationDocument | null>(null);
    const currentDocumentRef = useRef<CanvasCollaborationDocument | null>(null);
    const revisionRef = useRef(0);
    const inFlightRef = useRef<InFlightMutation | null>(null);
    const mutationTimerRef = useRef<number | null>(null);
    const presenceTimerRef = useRef<number | null>(null);
    const connectionIDRef = useRef("");
    const accessRef = useRef<CanvasAccess | null>(null);
    const cursorRef = useRef<Position | undefined>(undefined);
    const selectedNodeIdsRef = useRef(selectedNodeIds);

    const currentDocument = useMemo<CanvasCollaborationDocument | null>(() => {
        if (!project) return null;
        return {
            title: project.title,
            nodes,
            connections,
            backgroundMode,
            showImageInfo,
            directorScenes: project.directorScenes || [],
        };
    }, [backgroundMode, connections, nodes, project, showImageInfo]);

    useEffect(() => {
        currentDocumentRef.current = currentDocument;
    }, [currentDocument]);

    useEffect(() => {
        accessRef.current = access;
    }, [access]);

    useEffect(() => {
        selectedNodeIdsRef.current = selectedNodeIds;
    }, [selectedNodeIds]);

    const applyDocument = useCallback((document: CanvasCollaborationDocument, metadata?: Partial<CanvasProject>) => {
        const migratedGraph = removeRetiredCanvasNodes(document);
        const migratedDocument = { ...document, ...migratedGraph };
        setNodes(migratedDocument.nodes);
        setConnections(migratedDocument.connections);
        setBackgroundMode(document.backgroundMode);
        setShowImageInfo(document.showImageInfo);
        updateProject(projectId, {
            title: document.title,
            nodes: migratedDocument.nodes,
            connections: migratedDocument.connections,
            backgroundMode: document.backgroundMode,
            showImageInfo: document.showImageInfo,
            directorScenes: document.directorScenes,
            ...metadata,
        });
        currentDocumentRef.current = migratedDocument;
    }, [projectId, setBackgroundMode, setConnections, setNodes, setShowImageInfo, updateProject]);

    const applySnapshot = useCallback((state: CanvasCollaborationState) => {
        const remote = collaborationDocumentFromProject(state.project);
        const previousBase = baseDocumentRef.current;
        const desired = currentDocumentRef.current;
        const pending = previousBase && desired ? diffCanvasCollaborationDocument(previousBase, desired) : {};
        const canPreservePending = state.access.canEdit && !isEmptyCanvasMutationPatch(pending);
        const nextDocument = canPreservePending ? applyCanvasMutationPatch(remote, pending) : remote;
        baseDocumentRef.current = remote;
        revisionRef.current = state.project.revision || 0;
        inFlightRef.current = null;
        accessRef.current = state.access;
        setAccess(state.access);
        setManagementState(state);
        setPresenceByConnection({});
        setStatus(state.access.canEdit ? "online" : "readonly");
        applyDocument(nextDocument, collaborationMetadata(state));
        if (canPreservePending) {
            message.warning("协作连接已恢复，已保留本地未提交修改");
        } else if (!state.access.canEdit && !isEmptyCanvasMutationPatch(pending)) {
            message.warning("当前团队画布已变为只读，本地未提交修改未保存");
        }
    }, [applyDocument, message]);

    const resync = useCallback(async (reason: string) => {
        const base = baseDocumentRef.current;
        const desired = currentDocumentRef.current;
        const pending = base && desired ? diffCanvasCollaborationDocument(base, desired) : {};
        try {
            const state = await getCanvasCollaboration(projectId);
            const remote = collaborationDocumentFromProject(state.project);
            const merged = isEmptyCanvasMutationPatch(pending) ? remote : applyCanvasMutationPatch(remote, pending);
            baseDocumentRef.current = remote;
            revisionRef.current = state.project.revision || 0;
            inFlightRef.current = null;
            accessRef.current = state.access;
            setAccess(state.access);
            setManagementState(state);
            setStatus(state.access.canEdit ? "online" : "readonly");
            applyDocument(merged, collaborationMetadata(state));
            if (!isEmptyCanvasMutationPatch(pending) && state.access.canEdit) {
                message.warning(`${reason}，已同步最新版本并保留本地未提交修改`);
            }
        } catch (error) {
            setStatus("error");
            message.error(error instanceof Error ? `画布协作恢复失败：${error.message}` : "画布协作恢复失败");
        }
    }, [applyDocument, message, projectId]);

    const sendPendingMutation = useCallback(() => {
        const socket = socketRef.current;
        const base = baseDocumentRef.current;
        const current = currentDocumentRef.current;
        if (!socket || socket.readyState !== WebSocket.OPEN || !base || !current || inFlightRef.current || !accessRef.current?.canEdit) return;
        const patch = diffCanvasCollaborationDocument(base, current);
        if (isEmptyCanvasMutationPatch(patch)) return;
        const id = nanoid();
        const target = applyCanvasMutationPatch(base, patch);
        inFlightRef.current = { id, patch, target };
        socket.send(JSON.stringify({
            type: "mutation",
            mutation: { baseRevision: revisionRef.current, clientMutationId: id, patch },
        }));
    }, []);

    const scheduleMutation = useCallback(() => {
        if (mutationTimerRef.current) window.clearTimeout(mutationTimerRef.current);
        mutationTimerRef.current = window.setTimeout(() => {
            mutationTimerRef.current = null;
            sendPendingMutation();
        }, MUTATION_DEBOUNCE_MS);
    }, [sendPendingMutation]);

    const applyMutation = useCallback((mutation: CanvasMutationResult) => {
        const base = baseDocumentRef.current;
        const current = currentDocumentRef.current;
        if (!base || !current || mutation.revision <= revisionRef.current) return;
        if (mutation.revision !== revisionRef.current + 1) {
            void resync("检测到协作版本缺口");
            return;
        }
        const inFlight = inFlightRef.current;
        const pending = inFlight?.id === mutation.clientMutationId
            ? diffCanvasCollaborationDocument(inFlight.target, current)
            : diffCanvasCollaborationDocument(base, current);
        const remote = applyCanvasMutationPatch(base, mutation.patch);
        const merged = isEmptyCanvasMutationPatch(pending) ? remote : applyCanvasMutationPatch(remote, pending);
        baseDocumentRef.current = remote;
        revisionRef.current = mutation.revision;
        if (inFlight?.id === mutation.clientMutationId) inFlightRef.current = null;
        applyDocument(merged, { revision: mutation.revision });
        if (!isEmptyCanvasMutationPatch(pending)) scheduleMutation();
    }, [applyDocument, resync, scheduleMutation]);

    const handleEnvelope = useCallback((value: unknown) => {
        if (!isCanvasRealtimeEnvelope(value)) {
            setStatus("error");
            message.error("协作服务返回了无法识别的数据");
            return;
        }
        const envelope = value;
        if (envelope.type === "snapshot") {
            connectionIDRef.current = envelope.connectionId;
            reconnectAttemptRef.current = 0;
            applySnapshot(envelope.state);
            return;
        }
        if (envelope.type === "mutation") {
            applyMutation(envelope.mutation);
            return;
        }
        if (envelope.type === "presence") {
            setPresenceByConnection((current) => {
                if (!envelope.presence.active) {
                    const next = { ...current };
                    delete next[envelope.presence.connectionId];
                    return next;
                }
                return { ...current, [envelope.presence.connectionId]: envelope.presence };
            });
            return;
        }
        if (envelope.error.status === 409) {
            void resync("画布发生并发修改");
            return;
        }
        inFlightRef.current = null;
        message.error(envelope.error.message);
        if (envelope.error.status === 402 || envelope.error.status === 403) {
            setAccess((current) => {
                const next = current ? { ...current, canEdit: false } : current;
                accessRef.current = next;
                return next;
            });
            setStatus("readonly");
        }
    }, [applyMutation, applySnapshot, message, resync]);

    useEffect(() => {
        disposedRef.current = false;
        if (!enabled) {
            setStatus("personal");
            setAccess(null);
            setManagementState(null);
            setPresenceByConnection({});
            baseDocumentRef.current = null;
            revisionRef.current = 0;
            return;
        }

        const connect = () => {
            if (disposedRef.current) return;
            if (socketRef.current) socketRef.current.close();
            setStatus(reconnectAttemptRef.current ? "reconnecting" : "connecting");
            const socket = new WebSocket(canvasCollaborationSocketURL(projectId));
            socketRef.current = socket;
            socket.addEventListener("message", (event) => {
                try {
                    handleEnvelope(JSON.parse(String(event.data)) as unknown);
                } catch {
                    setStatus("error");
                    message.error("协作服务消息解析失败");
                }
            });
            socket.addEventListener("close", () => {
                if (socketRef.current !== socket) return;
                socketRef.current = null;
                if (disposedRef.current) return;
                setStatus("reconnecting");
                reconnectAttemptRef.current += 1;
                const delay = Math.min(1000 * 2 ** Math.min(reconnectAttemptRef.current - 1, 5), MAX_RECONNECT_DELAY_MS);
                reconnectTimerRef.current = window.setTimeout(connect, delay);
            });
            socket.addEventListener("error", () => {
                if (socket.readyState === WebSocket.OPEN) setStatus("error");
            });
        };
        connect();
        return () => {
            disposedRef.current = true;
            if (reconnectTimerRef.current) window.clearTimeout(reconnectTimerRef.current);
            if (mutationTimerRef.current) window.clearTimeout(mutationTimerRef.current);
            if (presenceTimerRef.current) window.clearTimeout(presenceTimerRef.current);
            reconnectTimerRef.current = null;
            mutationTimerRef.current = null;
            presenceTimerRef.current = null;
            const socket = socketRef.current;
            socketRef.current = null;
            if (socket) socket.close(1000, "canvas changed");
            setPresenceByConnection({});
        };
    }, [enabled, handleEnvelope, message, projectId]);

    useEffect(() => {
        if (!enabled || !currentDocument || !baseDocumentRef.current || !access?.canEdit) return;
        scheduleMutation();
    }, [access?.canEdit, currentDocument, enabled, scheduleMutation]);

    useEffect(() => {
        const base = baseDocumentRef.current;
        if (!enabled || !base || !currentDocument || access?.canEdit !== false) return;
        if (isEmptyCanvasMutationPatch(diffCanvasCollaborationDocument(base, currentDocument))) return;
        applyDocument(base, { revision: revisionRef.current });
        message.warning("当前团队画布只有查看权限，修改未被保存");
    }, [access?.canEdit, applyDocument, currentDocument, enabled, message]);

    const schedulePresence = useCallback(() => {
        const socket = socketRef.current;
        if (!enabled || !socket || socket.readyState !== WebSocket.OPEN || !connectionIDRef.current) return;
        if (presenceTimerRef.current) window.clearTimeout(presenceTimerRef.current);
        presenceTimerRef.current = window.setTimeout(() => {
            presenceTimerRef.current = null;
            if (socket.readyState !== WebSocket.OPEN) return;
            socket.send(JSON.stringify({
                type: "presence",
                presence: {
                    cursor: cursorRef.current,
                    selectedNodeIds: Array.from(selectedNodeIdsRef.current).slice(0, 100),
                },
            }));
        }, PRESENCE_DEBOUNCE_MS);
    }, [enabled]);

    useEffect(() => {
        schedulePresence();
    }, [schedulePresence, selectedNodeIds]);

    const updateCursor = useCallback((next: Position | undefined) => {
        cursorRef.current = next;
        schedulePresence();
    }, [schedulePresence]);

    const refreshManagementState = useCallback(async () => {
        const state = await getCanvasCollaboration(projectId);
        setManagementState(state);
        accessRef.current = state.access;
        setAccess(state.access);
        updateProject(projectId, collaborationMetadata(state));
        return state;
    }, [projectId, updateProject]);

    const refreshRemoteState = useCallback(async (expectedRevision?: number) => {
        const loadedState = await getCanvasCollaboration(projectId);
        const state = expectedRevision === undefined ? loadedState : requireCanvasCollaborationRevision(loadedState, expectedRevision);
        if ((state.project.revision || 0) < revisionRef.current) return;
        applySnapshot(state);
    }, [applySnapshot, projectId]);

    const adoptAuthoritativeBaseline = useCallback((state: CanvasCollaborationState) => {
        baseDocumentRef.current = collaborationDocumentFromProject(state.project);
        revisionRef.current = state.project.revision || 0;
        inFlightRef.current = null;
        accessRef.current = state.access;
        setAccess(state.access);
        setManagementState(state);
        setStatus(state.access.canEdit ? "online" : "readonly");
        updateProject(projectId, collaborationMetadata(state));
    }, [projectId, updateProject]);

    const flushPendingChanges = useCallback(async () => {
        if (!enabled) return;
        const loadedState = await requireEditableCanvasCollaboration(accessRef.current, Boolean(baseDocumentRef.current), () => getCanvasCollaboration(projectId));
        if (loadedState) adoptAuthoritativeBaseline(loadedState);
        if (mutationTimerRef.current) {
            window.clearTimeout(mutationTimerRef.current);
            mutationTimerRef.current = null;
        }
        sendPendingMutation();
        const startedAt = performance.now();
        while (performance.now() - startedAt < 10_000) {
            const base = baseDocumentRef.current;
            const current = currentDocumentRef.current;
            if (base && current && !inFlightRef.current && isEmptyCanvasMutationPatch(diffCanvasCollaborationDocument(base, current))) {
                return;
            }
            if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
                throw new Error("实时协作连接不可用，无法确认画布已保存");
            }
            await new Promise<void>((resolve) => window.setTimeout(resolve, 25));
            sendPendingMutation();
        }
        throw new Error("等待团队画布保存超时，请确认协作连接后重试");
    }, [adoptAuthoritativeBaseline, enabled, projectId, sendPendingMutation]);

    return {
        access,
        flushPendingChanges,
        managementState,
        presence: Object.values(presenceByConnection).filter((item) => item.connectionId !== connectionIDRef.current),
        refreshManagementState,
        refreshRemoteState,
        status,
        updateCursor,
    };
}

function collaborationDocumentFromProject(project: CanvasProject): CanvasCollaborationDocument {
    return {
        title: project.title,
        nodes: project.nodes || [],
        connections: project.connections || [],
        backgroundMode: project.backgroundMode,
        showImageInfo: project.showImageInfo || false,
        directorScenes: project.directorScenes || [],
    };
}

function collaborationMetadata(state: CanvasCollaborationState): Partial<CanvasProject> {
    return {
        ownerUserId: state.project.ownerUserId,
        teamId: state.access.teamId,
        revision: state.project.revision,
        defaultTeamAccess: state.project.defaultTeamAccess,
        accessLevel: state.access.level,
        canEdit: state.access.canEdit,
        canManage: state.access.canManage,
        teamSubscriptionActive: state.access.teamSubscriptionActive,
    };
}

function isCanvasRealtimeEnvelope(value: unknown): value is CanvasRealtimeEnvelope {
    if (!isRecord(value) || typeof value.type !== "string" || typeof value.canvasId !== "string") return false;
    if (value.type === "snapshot") return typeof value.connectionId === "string" && isRecord(value.state);
    if (value.type === "mutation") return isRecord(value.mutation) && typeof value.mutation.revision === "number";
    if (value.type === "presence") return isRecord(value.presence) && typeof value.presence.connectionId === "string";
    if (value.type === "error") return isRecord(value.error) && typeof value.error.status === "number" && typeof value.error.message === "string";
    return false;
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
