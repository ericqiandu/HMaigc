import assert from "node:assert/strict";
import { test } from "node:test";

import type { LocalAgentEvent, LocalAgentFinalDecisionEvent } from "@/services/local-agent/local-agent-contracts";

import { visibleLocalAgentFinalMessages } from "./local-agent-final-message";

const delivery: LocalAgentFinalDecisionEvent["expectedDelivery"] = {
    kind: "answer",
    completionCriteria: [{ fact: "final_message" }],
};

test("后端审计失败时不展示本机 Codex 自报的成功文案", () => {
    const events: LocalAgentEvent[] = [{ kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "已成功生成短片", expectedDelivery: delivery }];

    assert.deepEqual(visibleLocalAgentFinalMessages(events, "failed"), []);
});

test("后端审计成功时只展示最后一次通过交付验收的本机文案", () => {
    const events: LocalAgentEvent[] = [
        { kind: "final_decision", threadId: "thread-1", turnId: "turn-1", message: "第一次尝试完成", expectedDelivery: delivery },
        { kind: "final_decision", threadId: "thread-1", turnId: "turn-2", message: "纠偏后完成", expectedDelivery: delivery },
    ];

    assert.deepEqual(
        visibleLocalAgentFinalMessages(events, "succeeded").map((event) => event.message),
        ["纠偏后完成"],
    );
});
