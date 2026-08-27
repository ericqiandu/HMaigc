import { describe, expect, test } from "bun:test";

import { canvasAgentProjectTitle, cinematicAgentProgress, createCanvasAgentLaunchRequest, handoffCanvasAgentLaunch, hasCanvasAgentLaunchRecord, hasPendingCinematicAgentWork } from "../src/lib/canvas/canvas-agent-launch";
import type { AgentSessionDetail } from "../src/services/api/task-center";
import { CanvasNodeType, type CanvasAssistantSession } from "../src/types/canvas";
import { previewCanvasAgentOps } from "../src/lib/canvas/canvas-agent-ops";

describe("canvas agent launch", () => {
    test("首页先完成画布路由交接，不等待远端创建 Promise", async () => {
        const calls: string[] = [];
        let rejectRemoteCreation: ((error: Error) => void) | undefined;
        const remoteReady = new Promise<void>((_resolve, reject) => {
            rejectRemoteCreation = reject;
        });

        handoffCanvasAgentLaunch(
            { id: "canvas-new", remoteReady },
            (canvasId) => calls.push(`open:${canvasId}`),
            () => calls.push("remote-error"),
        );

        expect(calls).toEqual(["open:canvas-new"]);
        rejectRemoteCreation?.(new Error("remote unavailable"));
        await Promise.resolve();
        expect(calls).toEqual(["open:canvas-new", "remote-error"]);
    });

    test("normalizes the prompt and creates a privacy-safe persisted launch request", () => {
        const request = createCanvasAgentLaunchRequest({
            id: "launch-1",
            createdAt: "2026-07-29T00:00:00.000Z",
            draft: {
                prompt: "  月下少女走进发光竹林  ",
                attachments: [{ id: "attachment-1", name: "竹林.png", url: "/api/resources/resource-1/file", resourceId: "resource-1" }],
                generationModels: { image: "channel-image::gpt-image-2", video: "" },
                skillSelections: [{ dir: "skills/storyboard", name: "分镜", description: "" }],
                executionMode: "automatic",
            },
        });

        expect(request).toEqual({
            id: "launch-1",
            source: "home",
            prompt: "月下少女走进发光竹林",
            attachments: [{ resourceId: "resource-1", name: "竹林.png" }],
            generationModels: { image: "channel-image::gpt-image-2", video: "" },
            skillDirs: ["skills/storyboard"],
            executionMode: "automatic",
            createdAt: "2026-07-29T00:00:00.000Z",
        });
        expect(() =>
            createCanvasAgentLaunchRequest({
                id: "launch-2",
                createdAt: request.createdAt,
                draft: { ...requestToDraft(request), prompt: "   " },
            }),
        ).toThrow("创作描述不能为空");
        expect(() =>
            createCanvasAgentLaunchRequest({
                id: "launch-3",
                createdAt: request.createdAt,
                draft: { ...requestToDraft(request), attachments: [{ id: "local", name: "本地.png", url: "blob:local" }] },
            }),
        ).toThrow("参考图片尚未保存到账号资源");
    });

    test("derives a compact project title without putting the prompt in the URL", () => {
        expect(canvasAgentProjectTitle("  东方幻想   短片  ")).toBe("东方幻想 短片");
        expect(canvasAgentProjectTitle("一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十")).toBe("一二三四五六七八九十一二三四五六七八九十一二三四…");
    });

    test("recognizes a launch already persisted as a pending backend session or proposal", () => {
        const baseSession: CanvasAssistantSession = {
            id: "chat-1",
            title: "创作",
            messages: [],
            createdAt: "2026-07-29T00:00:00.000Z",
            updatedAt: "2026-07-29T00:00:00.000Z",
        };
        expect(
            hasCanvasAgentLaunchRecord(
                [
                    {
                        ...baseSession,
                        pendingBackendSession: {
                            id: "backend-1",
                            kind: "cinematic",
                            messageId: "message-1",
                            status: "pending",
                            executionMode: "guided",
                            launchRequestId: "launch-1",
                            startedAt: "2026-07-29T00:00:00.000Z",
                        },
                    },
                ],
                "launch-1",
            ),
        ).toBe(true);
        expect(
            hasCanvasAgentLaunchRecord(
                [
                    {
                        ...baseSession,
                        messages: [
                            {
                                id: "message-2",
                                role: "tool",
                                text: "等待确认",
                                detail: { kind: "cinematic-proposal", launchRequestId: "launch-2", status: "pending" },
                            },
                        ],
                    },
                ],
                "launch-2",
            ),
        ).toBe(true);
        expect(hasCanvasAgentLaunchRecord([baseSession], "launch-3")).toBe(false);
        expect(
            hasPendingCinematicAgentWork([
                {
                    ...baseSession,
                    pendingBackendSession: {
                        id: "backend-2",
                        kind: "cinematic",
                        messageId: "message-3",
                        status: "pending",
                        executionMode: "automatic",
                        startedAt: "2026-07-29T00:00:00.000Z",
                    },
                },
            ]),
        ).toBe(true);
        expect(
            hasPendingCinematicAgentWork([
                {
                    ...baseSession,
                    messages: [
                        {
                            id: "message-4",
                            role: "tool",
                            text: "等待确认",
                            detail: { kind: "cinematic-proposal", status: "pending" },
                        },
                    ],
                },
            ]),
        ).toBe(true);
        expect(hasPendingCinematicAgentWork([baseSession])).toBe(false);
    });

    test("reports only backend task facts in progress copy", () => {
        const detail = {
            session: {
                id: "session-1",
                status: "active",
                prompt: "测试",
                createdAt: "2026-07-29T00:00:00.000Z",
                updatedAt: "2026-07-29T00:00:00.000Z",
            },
            messages: [],
            results: [],
            tasks: [
                {
                    id: "task-1",
                    type: "agent_storyboard",
                    status: "succeeded",
                    prompt: "测试",
                    attempts: 1,
                    createdAt: "2026-07-29T00:00:00.000Z",
                    updatedAt: "2026-07-29T00:00:00.000Z",
                },
                {
                    id: "task-2",
                    type: "canvas_image",
                    status: "running",
                    progress: 42,
                    stage: "生成分镜图",
                    prompt: "测试",
                    attempts: 1,
                    createdAt: "2026-07-29T00:00:00.000Z",
                    updatedAt: "2026-07-29T00:00:00.000Z",
                },
            ],
        } satisfies AgentSessionDetail;

        expect(cinematicAgentProgress(detail)).toEqual({
            progress: 42,
            stage: "生成分镜图",
            taskCount: 2,
            completedTaskCount: 1,
            text: "影视 Agent 正在处理：生成分镜图 42%（已完成 1/2 个任务）",
        });
    });

    test("uses planned node titles instead of internal ids in approval copy", () => {
        const impact = previewCanvasAgentOps([
            { type: "add_node", id: "agent-internal-script", nodeType: CanvasNodeType.Text, title: "短片剧本" },
            { type: "add_node", id: "agent-internal-shot", nodeType: CanvasNodeType.Config, title: "镜头 1" },
            { type: "connect_nodes", fromNodeId: "agent-internal-script", toNodeId: "agent-internal-shot" },
        ]);

        expect(impact.items).toContain("连接「短片剧本」到「镜头 1」");
        expect(impact.items.join(" ")).not.toContain("agent-internal");
    });
});

function requestToDraft(request: ReturnType<typeof createCanvasAgentLaunchRequest>) {
    return {
        prompt: request.prompt,
        attachments: request.attachments.map((attachment) => ({ id: attachment.resourceId, name: attachment.name, url: `/api/resources/${attachment.resourceId}/file`, resourceId: attachment.resourceId })),
        generationModels: request.generationModels,
        skillSelections: request.skillDirs.map((dir) => ({ dir, name: dir, description: "" })),
        executionMode: request.executionMode,
    };
}
