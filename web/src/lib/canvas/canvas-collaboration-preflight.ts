import type { CanvasAccess, CanvasCollaborationState } from "@/services/api/canvas-collaboration";

export async function prepareAgentCanvasRun(ensureRemoteCanvas: () => Promise<void>, flushCollaboration: () => Promise<void>): Promise<void> {
    await ensureRemoteCanvas();
    await flushCollaboration();
}

export async function requireEditableCanvasCollaboration(currentAccess: CanvasAccess | null, hasAuthoritativeBaseline: boolean, loadState: () => Promise<CanvasCollaborationState>): Promise<CanvasCollaborationState | null> {
    if (currentAccess && hasAuthoritativeBaseline) {
        if (!currentAccess.canEdit) throw new Error("当前用户没有画布编辑权限");
        return null;
    }
    const state = await loadState();
    if (!state.access.canEdit) throw new Error("当前用户没有画布编辑权限");
    return state;
}

export function requireCanvasCollaborationRevision(state: CanvasCollaborationState, expectedRevision: number): CanvasCollaborationState {
    const actualRevision = state.project.revision || 0;
    if (actualRevision < expectedRevision) {
        throw new Error(`Agent 已提交画布版本 ${expectedRevision}，但协作服务仅返回版本 ${actualRevision}`);
    }
    return state;
}
