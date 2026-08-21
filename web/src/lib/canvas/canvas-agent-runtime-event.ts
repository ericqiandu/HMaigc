import type { AgentRuntimeEvent } from "@/services/api/agent-runtime";

export function agentCanvasCommittedRevision(event: AgentRuntimeEvent, canvasId: string): number | undefined {
    if (event.kind !== "tool.result") return undefined;
    const result = event.payload.lastToolResult;
    if (!result?.succeeded || result.output.canvasId !== canvasId) return undefined;
    const revision = result.output.committedRevision;
    return typeof revision === "number" && Number.isInteger(revision) && revision > 0 ? revision : undefined;
}
