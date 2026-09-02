import type { AgentRuntimeView } from "@/services/api/agent-runtime";
import type { LocalAgentEvent } from "@/services/local-agent/local-agent-contracts";

export type LocalAgentFinalDecisionEvent = Extract<LocalAgentEvent, { kind: "final_decision" }>;

export function visibleLocalAgentFinalMessages(events: LocalAgentEvent[], runtimeStatus: AgentRuntimeView["state"]["status"] | undefined): LocalAgentFinalDecisionEvent[] {
    if (runtimeStatus !== "succeeded") return [];
    for (let index = events.length - 1; index >= 0; index -= 1) {
        const event = events[index];
        if (event?.kind === "final_decision") return [event];
    }
    return [];
}
