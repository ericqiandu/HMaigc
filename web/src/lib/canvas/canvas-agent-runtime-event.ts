import type { AgentRuntimeEvent } from "@/services/api/agent-runtime";

export function agentCanvasCommittedRevision(event: AgentRuntimeEvent, canvasId: string): number | undefined {
    if (event.kind !== "item.completed" || event.payload.toolName !== "canvas.commit" || event.payload.succeeded !== true) return undefined;
    const output = event.payload.output;
    if (!output || typeof output !== "object" || Array.isArray(output)) return undefined;
    if (!("canvasId" in output) || output.canvasId !== canvasId) return undefined;
    const revision = "committedRevision" in output ? output.committedRevision : undefined;
    return typeof revision === "number" && Number.isInteger(revision) && revision > 0 ? revision : undefined;
}
