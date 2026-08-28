import {
    parseAgentArtifactRevision,
    parseStageReviewResult,
    type AgentProductionClient,
} from "./agent-production";
import { exactObject } from "./strict-contract";

const baseURL = String(import.meta.env.VITE_CANVAS_BACKEND_URL || "/api").replace(/\/+$/, "");

async function request(path: string, init?: RequestInit): Promise<unknown> {
    const response = await fetch(`${baseURL}${path}`, {
        ...init,
        credentials: "include",
        headers: { "Content-Type": "application/json", ...init?.headers },
    });
    let payload: unknown;
    try {
        payload = await response.json();
    } catch {
        throw new Error(`Agent 生产服务返回了无法解析的响应（HTTP ${response.status}）`);
    }
    const envelope = exactObject(payload, "Agent 生产响应", ["code", "data", "msg"]);
    if (!response.ok || envelope.code !== 0) {
        const data = envelope.data && typeof envelope.data === "object" && !Array.isArray(envelope.data)
            ? envelope.data as Record<string, unknown>
            : {};
        const code = typeof data.errorCode === "string" ? data.errorCode : "agent_production_request_failed";
        const message = typeof envelope.msg === "string" ? envelope.msg : "Agent 生产请求失败";
        throw Object.assign(new Error(message), {
            name: "AgentProductionRequestError",
            status: response.status,
            code,
        });
    }
    return envelope.data;
}

export const agentProductionClient: AgentProductionClient = {
    getArtifactRevision: async (runId, artifactId, revisionId) =>
        parseAgentArtifactRevision(
            await request(`/agent/runs/${encodeURIComponent(runId)}/artifacts/${encodeURIComponent(artifactId)}/revisions/${encodeURIComponent(revisionId)}`),
        ),
    reviewStage: async (runId, stageId, input) =>
        parseStageReviewResult(
            await request(`/agent/runs/${encodeURIComponent(runId)}/stages/${encodeURIComponent(stageId)}/reviews`, {
                method: "POST",
                body: JSON.stringify(input),
            }),
        ),
};
