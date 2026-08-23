import type { AgentRuntimeEvent } from "../../services/api/agent-runtime";

export type AgentConversationItem = {
    id: string;
    text: string;
    status: "in_progress" | "completed" | "failed";
};

export type AgentConversationState = {
    items: AgentConversationItem[];
    lastSequence: number;
    protocolError?: "agent_message_delta_conflict";
};

export function initialAgentConversationState(): AgentConversationState {
    return { items: [], lastSequence: 0 };
}

export function reduceAgentConversation(state: AgentConversationState, event: AgentRuntimeEvent): AgentConversationState {
    if (event.sequence <= state.lastSequence || !event.itemId) return state;
    const next: AgentConversationState = { ...state, lastSequence: event.sequence, items: state.items.map((item) => ({ ...item })) };
    if (event.kind !== "item.started" && event.kind !== "item.delta" && event.kind !== "item.completed" && event.kind !== "item.failed") return next;

    const payload = event.payload;
    const delta = typeof payload.delta === "string" && payload.userVisible === true ? payload.delta : "";
    const finalMessage = typeof payload.message === "string" ? payload.message : "";
    let item = next.items.find((candidate) => candidate.id === event.itemId);
    if (!item && (delta || finalMessage)) {
        item = { id: event.itemId, text: "", status: "in_progress" };
        next.items.push(item);
    }
    if (!item) return next;
    if (event.kind === "item.started") {
        item.text = "";
        item.status = "in_progress";
    }
    if (delta) item.text += delta;
    if (event.kind === "item.completed" && finalMessage) {
        if (!finalMessage.startsWith(item.text)) return { ...next, protocolError: "agent_message_delta_conflict" };
        item.text = finalMessage;
        item.status = "completed";
    } else if (event.kind === "item.failed") {
        item.status = "failed";
    }
    return next;
}

export function foldAgentConversation(events: AgentRuntimeEvent[]): AgentConversationState {
    return events.reduce(reduceAgentConversation, initialAgentConversationState());
}
