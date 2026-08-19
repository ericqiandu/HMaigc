import "./setup-happy-dom";

import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentCompletedClarification, AgentPendingClarification } from "../src/services/api/agent-runtime";

let Panel: typeof import("../src/components/canvas/agent-clarification-panel").AgentClarificationPanel;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ AgentClarificationPanel: Panel } = await import("../src/components/canvas/agent-clarification-panel"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

test("默认定位第一道未回答题并提交完整批次", async () => {
    const submitted: Array<Record<string, unknown>> = [];
    await mount({
        pending: pending([{ questionId: "q1", selectedOptionIds: ["sedan"], customText: "", skipped: false }]),
        onRespond: async (input) => submitted.push(input),
    });

    expect(document.body.textContent).toContain("2/2");
    expect(document.body.textContent).toContain("广告时长大概多长？");
    expect(document.querySelector(".agent-clarification-card-header")).not.toBeNull();
    expect(document.querySelector(".agent-clarification-card-footer")).not.toBeNull();
    expect(button("上一题").disabled).toBe(false);
    await act(async () => button("30 秒").click());
    await act(async () => button("提交").click());

    expect(submitted).toEqual([{ requestId: "request-1", questionId: "q2", answer: { selectedOptionIds: ["30s"], customText: "", skipped: false }, complete: true }]);
});

test("询问卡使用底部紧凑布局而不是嵌套表单卡片", async () => {
    await mount({ pending: pending() });

    const card = document.querySelector(".agent-clarification-card");
    expect(card?.querySelector(".agent-clarification-card-header .agent-clarification-question")?.textContent).toContain("想要哪种车型？");
    expect(card?.querySelector("fieldset.agent-clarification-fieldset legend")?.textContent).toContain("想要哪种车型？");
    expect(card?.querySelectorAll(".agent-clarification-option").length).toBe(2);
    expect(card?.querySelector(".agent-clarification-card-footer .agent-clarification-submit")).not.toBeNull();
});

test("自定义答案作为最后一个编号选项呈现且选中态不增加尾部图标", async () => {
    const customPending: AgentPendingClarification = {
        request: {
            ...pending().request,
            questions: [{ id: "q1", prompt: "广告的核心风格和主题方向是什么？", type: "single_choice", options: [{ id: "luxury", label: "豪华感" }, { id: "performance", label: "性能激情" }], allowCustomAnswer: true }],
        },
        answers: [],
    };
    await mount({ pending: customPending });

    const rows = document.querySelectorAll(".agent-clarification-option");
    expect(rows.length).toBe(3);
    expect(rows[2]?.classList.contains("agent-clarification-custom-option")).toBe(true);
    expect(rows[2]?.querySelector(".agent-clarification-option-index")?.textContent).toBe("3");
    expect(rows[2]?.querySelector<HTMLTextAreaElement>("textarea")?.placeholder).toBe("其他答案，请描述...");

    await setTextarea("其他方向");
    await act(async () => button("豪华感").click());
    expect(rows[0]?.classList.contains("is-selected")).toBe(true);
    expect(rows[0]?.querySelector(".agent-clarification-option-check")).toBeNull();
    expect(rows[2]?.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe("");
});

test("未保存当前答案时禁止跳到下一题", async () => {
    await mount({ pending: pending() });

    expect(document.body.textContent).toContain("想要哪种车型？");
    expect(button("下一题").disabled).toBe(true);
    await act(async () => button("轿车").click());
    expect(button("下一题").disabled).toBe(true);
});

test("多选与自定义答案保持独立结构并可显式忽略", async () => {
    const submitted: Array<Record<string, unknown>> = [];
    const customPending: AgentPendingClarification = {
        request: {
            ...pending().request,
            questions: [{ id: "q1", prompt: "选择核心卖点", type: "multi_choice", options: [{ id: "power", label: "动力" }, { id: "comfort", label: "舒适" }], allowCustomAnswer: true }],
        },
        answers: [],
    };
    await mount({ pending: customPending, onRespond: async (input) => submitted.push(input) });

    await act(async () => button("动力").click());
    await setTextarea("低能耗");
    await act(async () => button("提交").click());
    expect(submitted[0]).toEqual({ requestId: "request-1", questionId: "q1", answer: { selectedOptionIds: ["power"], customText: "低能耗", skipped: false }, complete: true });

    await act(async () => button("忽略").click());
    expect(submitted[1]).toEqual({ requestId: "request-1", questionId: "q1", answer: { selectedOptionIds: [], customText: "", skipped: true }, complete: true });
});

test("提交失败后保留自由文本草稿并暴露无障碍错误", async () => {
    const freeTextPending: AgentPendingClarification = {
        request: { ...pending().request, questions: [{ id: "q1", prompt: "请描述品牌语气", type: "free_text", options: [], allowCustomAnswer: false }] },
        answers: [],
    };
    await mount({ pending: freeTextPending, error: "网络连接失败", onRespond: async () => { throw new Error("网络连接失败"); } });
    await setTextarea("年轻、克制、有力量");
    await act(async () => button("提交").click());

    expect(document.querySelector<HTMLTextAreaElement>("textarea")?.value).toBe("年轻、克制、有力量");
    expect(document.querySelector('[role="alert"]')?.textContent).toContain("网络连接失败");
    expect(button("提交").disabled).toBe(false);
});

test("已询问历史只读展示问题与答案", async () => {
    await mount({ history: [history()] });
    await act(async () => button("已询问").click());

    expect(document.body.textContent).toContain("想要哪种车型？");
    expect(document.body.textContent).toContain("轿车");
    expect(document.querySelector(".agent-clarification-history")?.querySelector("input, textarea")).toBeNull();
});

async function mount(patch: Partial<Parameters<typeof Panel>[0]>) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () => root?.render(createElement(Panel, { pending: pending(), history: [], busy: false, error: "", onRespond: async () => undefined, ...patch })));
}

function pending(answers: AgentPendingClarification["answers"] = []): AgentPendingClarification {
    return {
        request: {
            requestId: "request-1",
            expectedDelivery: { kind: "answer", completionCriteria: [{ fact: "final_message" }] },
            questions: [
                { id: "q1", prompt: "想要哪种车型？", type: "single_choice", options: [{ id: "sedan", label: "轿车" }, { id: "suv", label: "SUV" }], allowCustomAnswer: false },
                { id: "q2", prompt: "广告时长大概多长？", type: "single_choice", options: [{ id: "15s", label: "15 秒" }, { id: "30s", label: "30 秒" }], allowCustomAnswer: false },
            ],
        },
        answers,
    };
}

function history(): AgentCompletedClarification {
    const source = pending([{ questionId: "q1", selectedOptionIds: ["sedan"], customText: "", skipped: false }, { questionId: "q2", selectedOptionIds: ["30s"], customText: "", skipped: false }]);
    return { request: source.request, answers: source.answers, completionQuestionId: "q2", completionExpectedStateVersion: 4 };
}

async function setTextarea(value: string) {
    const textarea = document.querySelector<HTMLTextAreaElement>("textarea");
    if (!textarea) throw new Error("未找到自定义输入框");
    const setter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
    if (!setter) throw new Error("测试 DOM 缺少 textarea value setter");
    await act(async () => {
        setter.call(textarea, value);
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
    });
}

function button(label: string) {
    const match = [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "").includes(label.replace(/\s+/g, "")) || item.getAttribute("aria-label") === label);
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}`);
    return match;
}
