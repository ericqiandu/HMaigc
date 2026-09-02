import { describe, expect, test } from "bun:test";
import { initialAgentConversationState, reduceAgentConversation } from "../src/components/canvas/agent-conversation-reducer";
import type { AgentRuntimeEvent, AgentTimelineItemKind } from "../src/services/api/agent-runtime";

const event = (sequence: number, kind: "item.started" | "item.delta" | "item.completed", payload: Record<string, unknown>, itemKind: AgentTimelineItemKind = "agent_message"): AgentRuntimeEvent => ({
    protocolVersion: 5,
    threadId: "thread",
    runId: "run",
    sequence,
    createdAt: "2026-08-23T00:00:00Z",
    kind,
    itemId: "message",
    itemKind,
    payload,
});

describe("Agent conversation reducer", () => {
    test("grows one assistant item and ignores replayed sequence", () => {
        let state = reduceAgentConversation(initialAgentConversationState(), event(1, "item.started", { itemId: "message", delta: "你", userVisible: true, started: true }));
        state = reduceAgentConversation(state, event(2, "item.delta", { itemId: "message", delta: "好", userVisible: true }));
        state = reduceAgentConversation(state, event(2, "item.delta", { itemId: "message", delta: "重复", userVisible: true }));
        expect(state.items).toEqual([{ id: "message", text: "你好", status: "in_progress" }]);
        expect(state.lastSequence).toBe(2);
    });

    test("rejects a completed message that conflicts with streamed prefix", () => {
        let state = reduceAgentConversation(initialAgentConversationState(), event(1, "item.started", { delta: "真实前缀", userVisible: true, started: true }));
        state = reduceAgentConversation(state, event(2, "item.completed", { message: "冲突正文" }));
        expect(state.protocolError).toBe("agent_message_delta_conflict");
        expect(state.items[0]?.text).toBe("真实前缀");
    });

    test("replaces an interrupted prefix when the same message stream restarts", () => {
        let state = reduceAgentConversation(initialAgentConversationState(), event(1, "item.started", { itemId: "message", delta: "旧的半截", userVisible: true, started: true }));
        state = reduceAgentConversation(state, event(2, "item.started", { itemId: "message", delta: "重新开始", userVisible: true, started: true }));
        expect(state.items).toEqual([{ id: "message", text: "重新开始", status: "in_progress" }]);
        expect(state.lastSequence).toBe(2);
    });

    test("does not render a completed user message as an assistant item", () => {
        const state = reduceAgentConversation(initialAgentConversationState(), event(1, "item.completed", { message: "用户输入" }, "user_message"));
        expect(state.items).toEqual([]);
        expect(state.lastSequence).toBe(1);
    });
});
