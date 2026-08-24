import "./setup-happy-dom";

import { App } from "antd";
import { afterEach, beforeAll, expect, test } from "bun:test";
import { act, createElement } from "react";
import type { Root } from "react-dom/client";

import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandle, AgentRuntimeHandleStorage, AgentRuntimeView, AgentThreadHistoryItem } from "../src/services/api/agent-runtime";
import type { PlatformSkill } from "../src/services/api/skills";
import { defaultConfig, encodeChannelModel, useConfigStore, type ModelChannel } from "../src/stores/use-config-store";
import { CanvasNodeType, type CanvasNodeData } from "../src/types/canvas";

let Panel: typeof import("../src/components/canvas/canvas-assistant-panel").CanvasAssistantPanel;
let createRoot: (container: Element | DocumentFragment) => Root;
let root: Root | null = null;

beforeAll(async () => {
    ({ createRoot } = await import("react-dom/client"));
    ({ CanvasAssistantPanel: Panel } = await import("../src/components/canvas/canvas-assistant-panel"));
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
    window.localStorage.clear();
    useConfigStore.setState({ config: defaultConfig, agentDefaultModel: "" });
});

test("单一运行内核保留原有紧凑 Agent 对话框视觉外壳", async () => {
    await mount(runtimeClient());

    const panel = document.querySelector(".canvas-agent-shell");
    const composer = document.querySelector(".canvas-agent-composer");
    const textarea = document.querySelector<HTMLTextAreaElement>("textarea");

    expect(panel?.classList.contains("canvas-agent-shell")).toBe(true);
    expect(document.body.textContent).toContain("Agent 画布助手");
    expect(document.body.textContent).toContain("搭建短剧工作流");
    expect(document.body.textContent).not.toContain("从目标开始");
    expect(composer).not.toBeNull();
    expect(textarea?.placeholder).toBe("描述你想让 Agent 如何操作画布");
    expect(document.querySelector(".canvas-agent-runtime-subtitle")).toBeNull();
    expect(button("选择模型")).not.toBeNull();
    expect(button("Skills")).not.toBeNull();
    expect(button("生成模式")).not.toBeNull();
    expect(button("添加图片")).not.toBeNull();
    expect(button("发送").classList.contains("canvas-submit-button")).toBe(true);
});

test("生成模式菜单使用紧凑且符合真实审批边界的说明", async () => {
    await mount(runtimeClient());

    await act(async () => button("生成模式").click());
    await settle();

    const modeMenu = document.querySelector<HTMLElement>('section[aria-label="生成模式"]');
    if (!modeMenu) throw new Error("未找到生成模式菜单");
    expect(modeMenu.textContent).toContain("手动模式");
    expect(modeMenu.textContent).toContain("每次生成前询问");
    expect(modeMenu.textContent).toContain("自动模式");
    expect(modeMenu.textContent).toContain("推理按 Token 计费，媒体生成前确认");
    expect(modeMenu.textContent).not.toContain("完全自动生成");
    const modeButtons = modeMenu.querySelectorAll<HTMLButtonElement>("button");
    expect(modeButtons).toHaveLength(2);
    expect(modeButtons[0]?.querySelector(".lucide-hand")).not.toBeNull();
    expect(modeButtons[1]?.querySelector(".lucide-cpu")).not.toBeNull();
    expect(modeButtons[1]?.querySelector(".lucide-refresh-cw")).toBeNull();
});

test("启动运行前先等待当前画布提交完成", async () => {
    const calls: string[] = [];
    const client = runtimeClient({
        startRun: async () => {
            calls.push("start-run");
            return runtimeView("running");
        },
    });
    await mount(client, undefined, {
        onBeforeRun: async () => {
            calls.push("flush-canvas");
        },
    });
    await setPrompt("整理当前画布");
    await act(async () => button("发送").click());
    await settle();
    expect(calls).toEqual(["flush-canvas", "start-run"]);
});

test("运行事件逐条交给当前画布刷新链路", async () => {
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const received: AgentRuntimeEvent[] = [];
    const running = runtimeView("running", { stateVersion: 3, stepNumber: 2 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 2 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });
    await mount(client, storage, { onRuntimeEvent: (event: AgentRuntimeEvent) => received.push(event) });
    const event: AgentRuntimeEvent = {
        protocolVersion: 2,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 3,
        kind: "item.completed",
        itemId: "tool-result-1",
        itemKind: "tool_call",
        payload: {
            toolCallId: "canvas-commit-1",
            toolName: "canvas.commit",
            actionVersion: 1,
            succeeded: true,
            output: { canvasId: "canvas-1", committedRevision: 8 },
        },
        createdAt: "2026-08-19T00:00:00Z",
    };
    if (!handlers) throw new Error("Agent SSE 未建立订阅");
    await act(async () => handlers?.onEvent(event));
    await settle();
    expect(received).toEqual([event]);
});

test("Agent 工作区提供受控宽度的语义分隔条", async () => {
    await mount(runtimeClient(), undefined, { width: 432, onResizeStart: () => undefined });

    const separator = document.querySelector<HTMLElement>('[role="separator"][aria-orientation="vertical"]');
    expect(separator).not.toBeNull();
    expect(separator?.getAttribute("aria-valuemin")).toBe("320");
    expect(separator?.getAttribute("aria-valuemax")).toBe("560");
    expect(separator?.getAttribute("aria-valuenow")).toBe("432");
    expect(separator?.getAttribute("aria-label")).toBe("调整 Agent 面板宽度");
    expect(separator?.tabIndex).toBe(0);
});

test("输入框选择的动态生成模型随本次 Agent 运行提交", async () => {
    const imageModel = encodeChannelModel("channel-image", "gpt-image-2");
    useConfigStore.setState({
        config: { ...defaultConfig, channels: [imageChannel()], models: [imageModel], imageModels: [imageModel], imageModel },
    });
    let submittedConfiguration: Parameters<AgentRuntimeClient["startRun"]>[1]["configuration"] | null = null;
    const client = runtimeClient({
        startRun: async (_threadId, input) => {
            submittedConfiguration = input.configuration;
            return runtimeView("running", { userMessage: input.userMessage, configuration: { generationModels: input.configuration.generationModels, skills: [], attachments: [], executionMode: input.configuration.executionMode } });
        },
    });
    await mount(client);
    await act(async () => button("选择模型").click());
    await settle();
    await act(async () => button("GPT Image 2").click());
    await setPrompt("生成一张分镜图");
    await act(async () => button("发送").click());
    await settle();
    expect(submittedConfiguration).toEqual({
        generationModels: { image: { channelId: "channel-image", model: "gpt-image-2" } },
        skillDirs: [],
        attachments: [],
        executionMode: "guided",
    });
});

test("选中视频节点后默认提交该节点的真实模型和明确提及的 Skills", async () => {
    const videoModel = encodeChannelModel("channel-video", "video-model-mini");
    useConfigStore.setState({
        config: { ...defaultConfig, channels: [videoChannel()], models: [videoModel], videoModels: [videoModel], videoModel },
    });
    const selectedVideo: CanvasNodeData = {
        id: "video-node",
        type: CanvasNodeType.Video,
        title: "Agent 视频",
        position: { x: 0, y: 0 },
        width: 420,
        height: 236,
        metadata: {
            channelId: "channel-video",
            model: "video-model-mini",
            composerContent: "使用 @视频提示词 优化镜头运动",
        },
    };
    let submittedConfiguration: Parameters<AgentRuntimeClient["startRun"]>[1]["configuration"] | null = null;
    const client = runtimeClient({
        startRun: async (_threadId, input) => {
            submittedConfiguration = input.configuration;
            return runtimeView("running", {
                userMessage: input.userMessage,
                configuration: {
                    generationModels: input.configuration.generationModels,
                    skills: [runtimeSkill()],
                    attachments: [],
                    executionMode: input.configuration.executionMode,
                },
            });
        },
    });

    await mount(client, undefined, {
        selectedNodeIds: new Set([selectedVideo.id]),
        getSelectedNodes: () => [selectedVideo],
        activatedSkills: [platformSkill()],
    });
    await setPrompt("继续生成这个视频");
    await act(async () => button("发送").click());
    await settle();

    expect(submittedConfiguration).toEqual({
        generationModels: { video: { channelId: "channel-video", model: "video-model-mini" } },
        skillDirs: ["skills/video-prompt"],
        attachments: [],
        executionMode: "guided",
    });
});

test("多个选中视频节点的模型不一致时不猜测默认模型", async () => {
    const firstVideo: CanvasNodeData = {
        id: "video-a",
        type: CanvasNodeType.Video,
        title: "视频 A",
        position: { x: 0, y: 0 },
        width: 420,
        height: 236,
        metadata: { channelId: "channel-video", model: "video-model-mini" },
    };
    const secondVideo: CanvasNodeData = {
        ...firstVideo,
        id: "video-b",
        title: "视频 B",
        metadata: { channelId: "channel-video", model: "video-model-pro" },
    };
    let submittedConfiguration: Parameters<AgentRuntimeClient["startRun"]>[1]["configuration"] | null = null;
    const client = runtimeClient({
        startRun: async (_threadId, input) => {
            submittedConfiguration = input.configuration;
            return runtimeView("running", { userMessage: input.userMessage });
        },
    });

    await mount(client, undefined, {
        selectedNodeIds: new Set([firstVideo.id, secondVideo.id]),
        getSelectedNodes: () => [firstVideo, secondVideo],
    });
    await setPrompt("检查这两个视频");
    await act(async () => button("发送").click());
    await settle();

    expect(submittedConfiguration?.generationModels).toEqual({});
});

test("用户消息展示本轮服务端冻结的模型和 Skills 而不是只显示文本", async () => {
    const videoModel = encodeChannelModel("channel-video", "video-model-mini");
    useConfigStore.setState({
        config: { ...defaultConfig, channels: [videoChannel()], models: [videoModel], videoModels: [videoModel], videoModel },
    });
    const completed = runtimeView("succeeded", {
        userMessage: "生成五秒视频",
        finalMessage: "视频已经生成。",
        verification: { status: "satisfied", rationale: "ok" },
        configuration: {
            generationModels: { video: { channelId: "channel-video", model: "video-model-mini" } },
            skills: [runtimeSkill()],
            attachments: [],
            executionMode: "guided",
        },
    });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-1", completed, "2026-08-23T00:00:00Z")] }),
        getRun: async () => completed,
    });

    await mount(client);

    const userMessage = document.querySelector<HTMLElement>('.canvas-agent-runtime-user-message[aria-label="你的消息"]');
    expect(userMessage?.textContent).toContain("生成五秒视频");
    expect(userMessage?.textContent).toContain("Seedance Mini");
    expect(userMessage?.textContent).toContain("视频提示词");
    expect(userMessage?.querySelector('[aria-label^="移除"]')).toBeNull();
    expect(userMessage?.querySelector('[aria-label="本轮已提交配置"]')).not.toBeNull();
});

test("单一运行链等待付费审批并展示验收后的最终消息", async () => {
    const calls: string[] = [];
    const videoModel = encodeChannelModel("channel-video", "video-model-mini");
    useConfigStore.setState({
        config: { ...defaultConfig, channels: [videoChannel()], models: [videoModel], videoModels: [videoModel], videoModel },
    });
    const approvalView = runtimeView("waiting_approval", {
        stateVersion: 3,
        pendingToolCall: {
            toolCallId: "render-1",
            toolName: "production.render",
            actionVersion: 2,
            arguments: {
                planKey: "plan-1",
                planVersion: 1,
                artifactId: "artifact-1",
                generationModel: { channelId: "channel-video", model: "video-model-mini" },
                videoInputMode: "text_to_video",
                videoConfig: { durationSeconds: 5, aspectRatio: "16:9", quality: "720p", generateAudio: false },
                amountMicrocredits: 1_000_000,
                quantity: 1,
            },
            expectedDelivery: answerDelivery(),
        },
    });
    const completedView = runtimeView("succeeded", {
        stateVersion: 4,
        stepNumber: 2,
        pendingToolCall: undefined,
        finalMessage: "画布整理已经完成。",
        verification: { status: "satisfied", rationale: "delivery evidence satisfies every criterion" },
    });
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => {
            calls.push("create-thread");
            return { id: "thread-1", canvasId: "canvas-1", status: "active" };
        },
        startRun: async (_threadId, input) => {
            calls.push(`start:${input.userMessage}`);
            return approvalView;
        },
        getRun: async () => approvalView,
        submitApproval: async (_runId, input) => {
            calls.push(`approval:${input.decision}:${input.toolCallId}:${input.actionVersion}`);
            return completedView;
        },
        submitClarificationResponse: async () => completedView,
        subscribe: () => () => undefined,
    };
    await mount(client);
    const textarea = document.querySelector("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    const valueSetter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
    if (!valueSetter) throw new Error("测试 DOM 缺少 textarea value setter");
    await act(async () => {
        valueSetter.call(textarea, "整理当前画布");
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(textarea.value).toBe("整理当前画布");
    expect(button("选择模型").disabled).toBe(false);
    expect(button("发送").disabled).toBe(false);
    await act(async () => button("发送").click());
    await settle();
    expect(calls).toEqual(["create-thread", "start:整理当前画布"]);
    expect(document.body.textContent).toContain("等待确认");
    expect(document.body.textContent).toContain("production.render");
    const approvalSummary = document.querySelector<HTMLElement>('[aria-label="冻结生成费用"]');
    expect(approvalSummary?.textContent).toContain("预计扣除");
    expect(approvalSummary?.textContent).toContain("1 积分");
    expect(approvalSummary?.textContent).toContain("Seedance Mini");
    expect(approvalSummary?.textContent).toContain("文生视频 · 720P · 16:9 · 5s · 1 个 · 无声");
    await act(async () => button("批准执行").click());
    await settle();
    expect(calls.at(-1)).toBe("approval:approved:render-1:2");
    expect(document.body.textContent).toContain("画布整理已经完成。");
    expect(document.body.textContent).toContain("交付已验收");
});

test("刷新时从持久句柄恢复运行并重放耐久事件", async () => {
    const calls: string[] = [];
    const runningView = runtimeView("running", { stateVersion: 3, stepNumber: 2 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 9 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async () => runningView,
        getRun: async (runId) => {
            calls.push(`resume:${runId}`);
            return runningView;
        },
        submitApproval: async () => runningView,
        submitClarificationResponse: async () => runningView,
        subscribe: (_runId, afterSequence) => {
            calls.push(`subscribe:${afterSequence}`);
            return () => undefined;
        },
    };
    await mount(client, storage);
    expect(calls).toEqual(["resume:run-1", "subscribe:0"]);
    expect(document.body.textContent).toContain("已执行 2 步 · 上限 8");
});

test("活动 Agent 运行使用发送按钮原位停止并提交状态版本 CAS", async () => {
    const calls: string[] = [];
    const running = runtimeView("running", { stateVersion: 6, stepNumber: 3 });
    const cancelled = runtimeView("cancelled", { stateVersion: 7, stepNumber: 3 });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-1", running, "2026-08-23T13:39:07Z")] }),
        getRun: async () => running,
        interrupt: async (runId, input) => {
            calls.push(`${runId}:${input.expectedStateVersion}`);
            return cancelled;
        },
    });

    await mount(client);

    const stop = button("停止 Agent");
    expect(stop.disabled).toBe(false);
    expect(stop.classList.contains("canvas-submit-button")).toBe(true);
    await act(async () => stop.click());
    await settle();
    expect(calls).toEqual(["run-1:6"]);
    expect(button("发送")).not.toBeNull();
});

test("运行刚建立且尚无回复时立即显示思考中", async () => {
    const queued = runtimeView("queued", { stateVersion: 1, stepNumber: 0 });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-1", queued, "2026-08-15T04:00:00Z")] }),
        getRun: async () => queued,
    });

    await mount(client);

    const thinking = document.querySelector<HTMLElement>('[aria-label="Agent 思考过程"]');
    expect(thinking?.querySelector(".canvas-agent-thinking-toggle")?.textContent).toContain("思考中");
    expect(thinking?.textContent).toContain("等待模型任务");
    expect(thinking?.textContent).not.toContain("准备中");
});

test("真实回复增量到达前显示思考中并在首个增量后切换为同一流式回复", async () => {
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    const running = runtimeView("running", { stateVersion: 3, stepNumber: 1 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 0 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        getRun: async () => running,
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });

    await mount(client, storage);

    const thinking = document.querySelector<HTMLElement>('[aria-label="Agent 思考过程"]');
    const thinkingToggle = thinking?.querySelector<HTMLButtonElement>(".canvas-agent-thinking-toggle");
    expect(thinkingToggle?.textContent).toContain("思考中");
    expect(thinkingToggle?.getAttribute("aria-expanded")).toBe("true");
    expect(thinking?.querySelector(".canvas-agent-thinking-disclosure")?.getAttribute("aria-hidden")).toBe("false");
    expect(thinking?.querySelector(".canvas-agent-thinking-icon")).not.toBeNull();
    expect(thinking?.querySelector(".canvas-agent-thinking-label[data-active='true']")).not.toBeNull();
    expect(thinking?.querySelector(".canvas-agent-thinking-spinner")).not.toBeNull();
    expect(document.querySelector('[aria-label="Agent 回复"]')).toBeNull();

    if (!handlers) throw new Error("Agent SSE 未建立订阅");
    await act(async () =>
        handlers?.onEvent({
            protocolVersion: 2,
            threadId: "thread-1",
            runId: "run-1",
            sequence: 2,
            kind: "item.delta",
            itemId: "message-1",
            itemKind: "agent_message",
            payload: { delta: "第一句", userVisible: true },
            createdAt: "2026-08-23T00:00:00Z",
        }),
    );
    await settle();

    expect(thinkingToggle?.textContent).toContain("已思考");
    expect(thinkingToggle?.getAttribute("aria-expanded")).toBe("false");
    expect(thinking?.querySelector(".canvas-agent-thinking-disclosure")?.getAttribute("aria-hidden")).toBe("true");
    const response = document.querySelector<HTMLElement>('[aria-label="Agent 回复"]');
    expect(response?.textContent).toContain("第一句");
    expect(response?.querySelector(".canvas-agent-runtime-final-heading")).toBeNull();
    expect(response?.querySelector(".canvas-agent-runtime-streaming-caret")).toBeNull();
});

test("新一轮运行不继承上一轮手动折叠并默认展开思考轨迹", async () => {
    const completed = runtimeView("succeeded", { finalMessage: "上一轮完成", verification: { status: "satisfied", rationale: "ok" } });
    const running = runtimeView("running", { userMessage: "新一轮" }, { id: "run-2", clientRequestId: "request-2" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 0 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        getRun: async () => completed,
        startRun: async () => running,
    });

    await mount(client, storage);
    const previousToggle = document.querySelector<HTMLButtonElement>(".canvas-agent-thinking-toggle");
    if (!previousToggle) throw new Error("未找到上一轮思考轨迹");
    await act(async () => previousToggle.click());
    await act(async () => previousToggle.click());
    expect(previousToggle.getAttribute("aria-expanded")).toBe("false");

    await setPrompt("新一轮");
    await act(async () => button("发送").click());
    await settle();

    const currentToggle = document.querySelector<HTMLButtonElement>(".canvas-agent-thinking-toggle");
    expect(currentToggle?.textContent).toContain("思考中");
    expect(currentToggle?.getAttribute("aria-expanded")).toBe("true");
});

test("运行已终止时即使完成事件稍晚到达也不继续显示流式光标", async () => {
    let handlers: Parameters<AgentRuntimeClient["subscribe"]>[2] | null = null;
    let runReads = 0;
    const running = runtimeView("running", { stateVersion: 3 });
    const completed = runtimeView("succeeded", { stateVersion: 4, finalMessage: "第一句", verification: { status: "satisfied", rationale: "ok" } });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 0 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        getRun: async () => (runReads++ === 0 ? running : completed),
        subscribe: (_runId, _afterSequence, nextHandlers) => {
            handlers = nextHandlers;
            return () => undefined;
        },
    });

    await mount(client, storage);
    if (!handlers) throw new Error("Agent SSE 未建立订阅");
    await act(async () =>
        handlers?.onEvent({
            protocolVersion: 2,
            threadId: "thread-1",
            runId: "run-1",
            sequence: 2,
            kind: "item.delta",
            itemId: "message-1",
            itemKind: "agent_message",
            payload: { delta: "第一句", userVisible: true },
            createdAt: "2026-08-23T00:00:00Z",
        }),
    );
    await act(async () => new Promise((resolve) => setTimeout(resolve, 80)));

    expect(document.querySelector('[aria-label="Agent 思考过程"]')?.textContent).toContain("已思考");
    expect(document.querySelector('[aria-label="Agent 回复"] .canvas-agent-runtime-streaming-caret')).toBeNull();
});

test("启动响应丢失后复用同一 clientRequestId 收敛运行", async () => {
    const calls: string[] = [];
    const runningView = runtimeView("running", { stateVersion: 2, stepNumber: 1, userMessage: "生成三镜头短片" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", lastSequence: 0, pendingRun: { clientRequestId: "request-stable", userMessage: "生成三镜头短片", configuration: emptyConfiguration() } }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (threadId, input) => {
            calls.push(`${threadId}:${input.clientRequestId}:${input.userMessage}`);
            return runningView;
        },
        getRun: async () => runningView,
        submitApproval: async () => runningView,
        submitClarificationResponse: async () => runningView,
        subscribe: () => () => undefined,
    };
    await mount(client, storage);
    expect(calls).toEqual(["thread-1:request-stable:生成三镜头短片"]);
    expect(document.body.textContent).toContain("生成三镜头短片");
});

test("启动恢复再次失败时还原原指令并允许复用同一请求身份重试", async () => {
    const calls: string[] = [];
    const completedView = runtimeView("succeeded", {
        userMessage: "生成三镜头短片",
        finalMessage: "已完成",
        verification: { status: "satisfied", rationale: "ok" },
    });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", lastSequence: 0, pendingRun: { clientRequestId: "request-stable", userMessage: "生成三镜头短片", configuration: emptyConfiguration() } }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (_threadId, input) => {
            calls.push(input.clientRequestId);
            if (calls.length === 1) throw new Error("启动结果仍不可确认");
            return completedView;
        },
        getRun: async () => completedView,
        submitApproval: async () => completedView,
        submitClarificationResponse: async () => completedView,
        subscribe: () => () => undefined,
    };
    await mount(client, storage);
    const textarea = document.querySelector<HTMLTextAreaElement>("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    expect(textarea.value).toBe("生成三镜头短片");
    expect(document.body.textContent).toContain("启动结果仍不可确认");
    await act(async () => button("发送").click());
    await settle();
    expect(calls).toEqual(["request-stable", "request-stable"]);
    expect(document.body.textContent).toContain("已完成");
});

test("切换画布时先清空上一画布的运行事实", async () => {
    const oldView = runtimeView("running", { userMessage: "只属于画布一的任务" });
    const storage: AgentRuntimeHandleStorage = {
        load: async (canvasId) => (canvasId === "canvas-1" ? { threadId: "thread-1", activeRunId: "run-1", lastSequence: 3 } : null),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: `thread-${canvasId}`, canvasId, status: "active" }),
        startRun: async () => oldView,
        getRun: async () => oldView,
        submitApproval: async () => oldView,
        submitClarificationResponse: async () => oldView,
        subscribe: () => () => undefined,
    };
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const render = (projectId: string) =>
        createElement(
            App,
            null,
            createElement(Panel, {
                projectId,
                canvasRevision: 7,
                selectedNodeIds: new Set<string>(),
                getSelectedNodes: () => [],
                activatedSkills: [],
                closing: false,
                width: 400,
                onResizeStart: () => undefined,
                onResizeKeyDown: () => undefined,
                onCollapse: () => undefined,
                runtimeClient: client,
                runtimeStorage: storage,
            }),
        );
    await act(async () => root?.render(render("canvas-1")));
    await settle();
    expect(document.body.textContent).toContain("只属于画布一的任务");
    await act(async () => root?.render(render("canvas-2")));
    await settle();
    expect(document.body.textContent).not.toContain("只属于画布一的任务");
    expect(document.body.textContent).toContain("等待任务");
});

test("首页已确认的创作请求只提交一次并在成功后消费", async () => {
    const calls: string[] = [];
    const completedView = runtimeView("succeeded", { userMessage: "生成东方幻想短片", finalMessage: "已完成", verification: { status: "satisfied", rationale: "ok" } });
    const client: AgentRuntimeClient = {
        listThreads: async () => ({ items: [] }),
        createThread: async () => ({ id: "thread-1", canvasId: "canvas-1", status: "active" }),
        startRun: async (_threadId, input) => {
            calls.push(`start:${input.userMessage}`);
            return completedView;
        },
        getRun: async () => completedView,
        submitApproval: async () => completedView,
        submitClarificationResponse: async () => completedView,
        subscribe: () => () => undefined,
    };
    await mount(client, undefined, {
        agentLaunchRequest: {
            id: "launch-1",
            source: "home",
            prompt: "生成东方幻想短片",
            attachments: [],
            generationModels: { image: "", video: "" },
            skillDirs: [],
            executionMode: "guided",
            createdAt: "2026-08-15T00:00:00Z",
        },
        onAgentLaunchHandled: (id: string) => calls.push(`handled:${id}`),
    });
    expect(calls).toEqual(["start:生成东方幻想短片", "handled:launch-1"]);
});

test("没有本地句柄时采用服务端最近运行并保存恢复身份", async () => {
    const saved: AgentRuntimeHandle[] = [];
    const running = runtimeView("running", { userMessage: "正在生成" }, { id: "run-active", threadId: "thread-active", lastEventSequence: 6 });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => null,
        save: async (_canvasId, handle) => saved.push(handle),
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-active", running, "2026-08-15T04:00:00Z")] }),
        getRun: async () => running,
        subscribe: (_runId, afterSequence) => {
            expect(afterSequence).toBe(0);
            return () => undefined;
        },
    });
    await mount(client, storage);
    expect(document.body.textContent).toContain("正在生成");
    expect(saved.at(-1)).toMatchObject({ threadId: "thread-active", activeRunId: "run-active", lastSequence: 6 });
});

test("历史对话可切换到旧终态运行并保存所选 Thread", async () => {
    const saved: AgentRuntimeHandle[] = [];
    const current = runtimeView("succeeded", { userMessage: "当前对话", finalMessage: "当前结果", verification: { status: "satisfied", rationale: "ok" } }, { id: "run-current", threadId: "thread-current" });
    const old = runtimeView("succeeded", { userMessage: "旧对话", finalMessage: "旧结果", verification: { status: "satisfied", rationale: "ok" } }, { id: "run-old", threadId: "thread-old" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => null,
        save: async (_canvasId, handle) => saved.push(handle),
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-current", current, "2026-08-15T04:00:00Z"), historyItem("thread-old", old, "2026-08-15T03:00:00Z")] }),
        getRun: async (runId) => (runId === old.run.id ? old : current),
    });
    await mount(client, storage);
    await act(async () => button("历史对话").click());
    await act(async () => button("旧对话").click());
    await settle();
    expect(document.body.textContent).toContain("旧结果");
    expect(saved.at(-1)).toMatchObject({ threadId: "thread-old" });
});

test("历史接口失败独立显示但不阻断本地运行恢复", async () => {
    const running = runtimeView("running", { userMessage: "本地恢复中的任务" });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => ({ threadId: "thread-1", activeRunId: "run-1", lastSequence: 3 }),
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({
        listThreads: async () => {
            throw new Error("历史服务暂不可用");
        },
        getRun: async () => running,
    });
    await mount(client, storage);
    expect(document.body.textContent).toContain("本地恢复中的任务");
    await act(async () => button("历史对话").click());
    expect(document.body.textContent).toContain("历史服务暂不可用");
});

test("余额不足时向用户显示可理解文案而不是内部错误码", async () => {
    const failed = runtimeView("failed", { failureCode: "insufficient_credits" });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-1", failed, "2026-08-15T04:00:00Z")] }),
        getRun: async () => failed,
    });

    await mount(client);

    expect(document.querySelector(".canvas-agent-runtime-failure")?.textContent).toBe("余额不足");
    expect(document.body.textContent).not.toContain("insufficient_credits");
});

test("上一生成费用待确认时在失败卡和工具卡隐藏内部错误码", async () => {
    const failed = runtimeView("failed", {
        failureCode: "production_previous_billing_unresolved",
        lastToolResult: {
            toolCallId: "tool_retry_video_clip_002",
            actionVersion: 1,
            succeeded: false,
            output: {},
            errorCode: "production_previous_billing_unresolved",
        },
    });
    const client = runtimeClient({
        listThreads: async () => ({ items: [historyItem("thread-1", failed, "2026-08-15T04:00:00Z")] }),
        getRun: async () => failed,
    });

    await mount(client);

    const message = "上一次生成费用仍待确认，请先处理后再重试";
    expect(document.querySelector(".canvas-agent-runtime-failure")?.textContent).toBe(message);
    expect(document.querySelector(".canvas-agent-runtime-tool-result")?.textContent).toContain(message);
    expect(document.body.textContent).not.toContain("production_previous_billing_unresolved");
});

test("服务端历史恢复成功也不会吞掉本地句柄读取错误", async () => {
    const completed = runtimeView("succeeded", { userMessage: "服务端恢复的对话", finalMessage: "服务端结果", verification: { status: "satisfied", rationale: "ok" } });
    const storage: AgentRuntimeHandleStorage = {
        load: async () => {
            throw new Error("本地句柄损坏");
        },
        save: async () => undefined,
        clear: async () => undefined,
    };
    const client = runtimeClient({ listThreads: async () => ({ items: [historyItem("thread-1", completed, "2026-08-15T04:00:00Z")] }), getRun: async () => completed });
    await mount(client, storage);
    expect(document.body.textContent).toContain("服务端结果");
    expect(document.body.textContent).toContain("本地句柄损坏");
});

test("新运行启动后刷新历史且刷新失败不污染运行状态", async () => {
    let historyCalls = 0;
    const running = runtimeView("running", { userMessage: "启动后的任务" });
    const client = runtimeClient({
        listThreads: async () => {
            historyCalls += 1;
            if (historyCalls > 1) throw new Error("历史刷新失败");
            return { items: [] };
        },
        startRun: async () => running,
    });
    await mount(client);
    await setPrompt("启动后的任务");
    await act(async () => button("发送").click());
    await settle();
    expect(historyCalls).toBe(2);
    expect(document.body.textContent).toContain("启动后的任务");
    expect(document.body.textContent).not.toContain("运行失败");
    await act(async () => button("历史对话").click());
    expect(document.body.textContent).toContain("历史刷新失败");
});

test("询问状态在输入框上方显示结构化卡片并保留运行配置入口", async () => {
    const waiting = runtimeView("waiting_input", {
        stateVersion: 4,
        expectedDelivery: answerDelivery(),
        pendingClarification: {
            request: {
                requestId: "clarification-1",
                expectedDelivery: answerDelivery(),
                questions: [
                    {
                        id: "question-1",
                        prompt: "广告时长大概多长？",
                        type: "single_choice",
                        options: [
                            { id: "15s", label: "15 秒" },
                            { id: "30s", label: "30 秒" },
                        ],
                        allowCustomAnswer: false,
                    },
                ],
            },
            answers: [],
        },
    });
    const client = runtimeClient({ listThreads: async () => ({ items: [historyItem("thread-1", waiting, "2026-08-15T04:00:00Z")] }), getRun: async () => waiting });
    await mount(client);

    expect(document.body.textContent).toContain("询问中");
    expect(document.body.textContent).not.toContain("正在执行");
    expect(document.body.textContent).toContain("广告时长大概多长？");
    expect(document.querySelector(".agent-clarification-section")).not.toBeNull();
    expect(document.querySelector(".canvas-agent-runtime-interaction > .agent-clarification-section")).not.toBeNull();
    expect(document.querySelector(".canvas-agent-runtime-content .agent-clarification-section")).toBeNull();
    expect(document.querySelector(".canvas-agent-runtime-content > .agent-clarification-live")).not.toBeNull();
    expect(document.querySelector(".canvas-agent-runtime-interaction .agent-clarification-live")).toBeNull();
    expect(button("选择模型")).not.toBeNull();
    expect(button("Skills")).not.toBeNull();
    expect(button("生成模式")).not.toBeNull();
    expect(document.querySelector<HTMLTextAreaElement>(".canvas-agent-composer-textarea")?.disabled).toBe(true);
});

async function mount(client: AgentRuntimeClient, storage?: AgentRuntimeHandleStorage, extra: Record<string, unknown> = {}) {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    await act(async () =>
        root?.render(
            createElement(
                App,
                null,
                createElement(Panel, {
                    projectId: "canvas-1",
                    canvasRevision: 7,
                    selectedNodeIds: new Set(["node-a", "node-b"]),
                    getSelectedNodes: () => [],
                    activatedSkills: [],
                    closing: false,
                    width: 400,
                    onResizeStart: () => undefined,
                    onResizeKeyDown: () => undefined,
                    onCollapse: () => undefined,
                    runtimeClient: client,
                    runtimeStorage: storage,
                    ...extra,
                }),
            ),
        ),
    );
    await settle();
}

function runtimeView(status: AgentRuntimeView["state"]["status"], patch: Partial<AgentRuntimeView["state"]> = {}, runPatch: Partial<AgentRuntimeView["run"]> = {}): AgentRuntimeView {
    const state = {
        stateVersion: 2,
        stepNumber: 1,
        maxSteps: 8,
        status,
        clarificationHistory: [],
        userMessage: "整理当前画布",
        configuration: { generationModels: {}, skills: [], attachments: [], executionMode: "guided" },
        ...(status === "succeeded"
            ? {
                  expectedDelivery: { kind: "answer" as const, completionCriteria: [{ fact: "final_message" }] },
              }
            : {}),
        ...patch,
    } satisfies AgentRuntimeView["state"];
    return {
        run: {
            id: "run-1",
            threadId: "thread-1",
            actorUserId: "user-1",
            clientRequestId: "request-1",
            status,
            lastEventSequence: 1,
            stateVersion: state.stateVersion,
            stepNumber: state.stepNumber,
            maxSteps: state.maxSteps,
            modelRecordId: "model-1",
            modelKey: "agent-model",
            toolSchemaVersion: 1,
            createdAt: "2026-08-15T00:00:00Z",
            updatedAt: "2026-08-15T00:00:01Z",
            ...runPatch,
        },
        state,
    };
}

function answerDelivery(): NonNullable<AgentRuntimeView["state"]["expectedDelivery"]> {
    return { kind: "answer", completionCriteria: [{ fact: "final_message" }] };
}

function emptyConfiguration() {
    return { generationModels: {}, skillDirs: [], attachments: [], executionMode: "guided" as const };
}

function imageChannel(): ModelChannel {
    return {
        id: "channel-image",
        name: "图片模型渠道",
        baseUrl: "/api/ai/system/channel-image",
        apiKey: "system",
        apiFormat: "openai",
        interfaceType: "openai-image",
        models: ["gpt-image-2"],
        scope: "system",
        enabled: true,
        hasApiKey: true,
        modelCosts: [
            {
                model: "gpt-image-2",
                displayName: "GPT Image 2",
                marketingCopy: "分镜图生成",
                promotionBadge: "",
                estimatedDurationSeconds: 20,
                brandKey: "openai",
                accessPolicy: "authenticated",
                accessible: true,
                capability: "image",
                watermarkCapability: "not_applicable",
                billingMode: "fixed_request",
                priceStrategy: "flat",
                unitPriceMicrocredits: 1,
                priceTiers: [],
            },
        ],
    };
}

function videoChannel(): ModelChannel {
    return {
        id: "channel-video",
        name: "视频模型渠道",
        baseUrl: "/api/ai/system/channel-video",
        apiKey: "system",
        apiFormat: "openai",
        interfaceType: "ai-open-platform-video-volcengine",
        models: ["video-model-mini"],
        scope: "system",
        enabled: true,
        hasApiKey: true,
        modelCosts: [
            {
                model: "video-model-mini",
                displayName: "Seedance Mini",
                marketingCopy: "视频生成",
                promotionBadge: "",
                estimatedDurationSeconds: 30,
                brandKey: "seedance",
                accessPolicy: "authenticated",
                accessible: true,
                capability: "video",
                watermarkCapability: "controlled",
                billingMode: "per_second",
                priceStrategy: "video_resolution",
                unitPriceMicrocredits: 0,
                priceTiers: [{ resolution: "720p", inputVariant: "standard", unitPriceMicrocredits: 200_000 }],
            },
        ],
    };
}

function platformSkill(): PlatformSkill {
    return {
        dir: "skills/video-prompt",
        name: "视频提示词",
        description: "优化视频镜头运动",
        icon: "",
        cover_url: "",
        detail_text: "",
        categories: ["video"],
        version: 1,
        checksum: "skill-checksum",
        status: "published",
        source_kind: "original",
        source_license: "proprietary",
        published_at: "2026-08-23T00:00:00Z",
        uploader_name: "HMaigc",
        liked: false,
        activated: true,
    };
}

function runtimeSkill(): AgentRuntimeView["state"]["configuration"]["skills"][number] {
    return {
        dir: "skills/video-prompt",
        name: "视频提示词",
        description: "优化视频镜头运动",
        instructions: "",
        version: 1,
        checksum: "skill-checksum",
    };
}

function runtimeClient(patch: Partial<AgentRuntimeClient> = {}): AgentRuntimeClient {
    const running = runtimeView("running");
    return {
        listThreads: async () => ({ items: [] }),
        createThread: async (canvasId) => ({ id: "thread-1", canvasId, status: "active" }),
        startRun: async () => running,
        getRun: async () => running,
        steer: async () => running,
        interrupt: async () => running,
        submitApproval: async () => running,
        submitClarificationResponse: async () => running,
        subscribe: () => () => undefined,
        ...patch,
    };
}

function historyItem(threadId: string, latestRun: AgentRuntimeView | null, activityAt: string): AgentThreadHistoryItem {
    const turns = latestRun
        ? [
              {
                  run: {
                      id: latestRun.run.id,
                      threadId,
                      status: latestRun.run.status,
                      lastEventSequence: latestRun.run.lastEventSequence,
                      stateVersion: latestRun.run.stateVersion,
                      stepNumber: latestRun.run.stepNumber,
                      maxSteps: latestRun.run.maxSteps,
                      modelKey: latestRun.run.modelKey,
                      toolSchemaVersion: latestRun.run.toolSchemaVersion,
                      runtimeVersion: 1,
                      policyVersion: 1,
                      createdAt: latestRun.run.createdAt,
                      updatedAt: latestRun.run.updatedAt,
                      completedAt: latestRun.run.completedAt,
                  },
                  items: [
                      {
                          id: `${latestRun.run.id}-user-message`,
                          runId: latestRun.run.id,
                          kind: "user_message" as const,
                          status: "completed" as const,
                          ordinal: 1,
                          sourceEventSequence: 1,
                          content: { message: latestRun.state.userMessage },
                          startedAt: latestRun.run.createdAt,
                          completedAt: latestRun.run.createdAt,
                          createdAt: latestRun.run.createdAt,
                          updatedAt: latestRun.run.createdAt,
                      },
                  ],
              },
          ]
        : [];
    return {
        thread: { id: threadId, canvasId: "canvas-1", status: "active", createdAt: "2026-08-15T00:00:00Z", updatedAt: activityAt },
        activityAt,
        turns,
    };
}

async function setPrompt(value: string) {
    const textarea = document.querySelector("textarea");
    if (!textarea) throw new Error("未找到 Agent 输入框");
    const valueSetter = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(textarea), "value")?.set;
    if (!valueSetter) throw new Error("测试 DOM 缺少 textarea value setter");
    await act(async () => {
        valueSetter.call(textarea, value);
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
    });
}

function button(label: string) {
    const compactLabel = label.replace(/\s+/g, "");
    const match = [...document.querySelectorAll("button")].find((item) => item.textContent?.replace(/\s+/g, "").includes(compactLabel) || item.getAttribute("aria-label")?.replace(/\s+/g, "").includes(compactLabel));
    if (!(match instanceof HTMLButtonElement)) throw new Error(`未找到按钮：${label}`);
    return match;
}

async function settle() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
