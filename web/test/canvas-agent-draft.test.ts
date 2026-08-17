import { expect, test } from "bun:test";

import { createEmptyCanvasAgentDraft, removeLastCanvasAgentDraftSelection } from "../src/lib/canvas/canvas-agent-draft";

const skillA = { dir: "skills/storyboard", name: "分镜", description: "", detailText: "" };
const skillB = { dir: "skills/video", name: "视频", description: "", detailText: "" };

test("共享 Agent 草稿不隐式选择模型且默认采用手动确认模式", () => {
    expect(createEmptyCanvasAgentDraft()).toEqual({
        prompt: "",
        attachments: [],
        generationModels: { image: "", video: "" },
        skillSelections: [],
        executionMode: "guided",
    });
});

test("空输入退格按可见顺序移除最后一个 Skill、视频模型和图片模型", () => {
    const draft = {
        ...createEmptyCanvasAgentDraft(),
        generationModels: { image: "channel-image::image-model", video: "channel-video::video-model" },
        skillSelections: [skillA, skillB],
    };

    const withoutSkillB = removeLastCanvasAgentDraftSelection(draft);
    expect(withoutSkillB?.skillSelections).toEqual([skillA]);
    const withoutSkillA = withoutSkillB && removeLastCanvasAgentDraftSelection(withoutSkillB);
    expect(withoutSkillA?.skillSelections).toEqual([]);
    const withoutVideo = withoutSkillA && removeLastCanvasAgentDraftSelection(withoutSkillA);
    expect(withoutVideo?.generationModels).toEqual({ image: "channel-image::image-model", video: "" });
    const withoutImage = withoutVideo && removeLastCanvasAgentDraftSelection(withoutVideo);
    expect(withoutImage?.generationModels).toEqual({ image: "", video: "" });
    expect(withoutImage && removeLastCanvasAgentDraftSelection(withoutImage)).toBeNull();
});

test("选择删除不会改写提示词、附件或执行模式", () => {
    const draft = {
        ...createEmptyCanvasAgentDraft(),
        prompt: "制作可乐广告",
        attachments: [{ id: "attachment-1", name: "cola.png", url: "blob:cola", resourceId: "resource-1" }],
        skillSelections: [skillA],
        executionMode: "automatic" as const,
    };
    const next = removeLastCanvasAgentDraftSelection(draft);
    expect(next && { prompt: next.prompt, attachments: next.attachments, executionMode: next.executionMode }).toEqual({
        prompt: "制作可乐广告",
        attachments: draft.attachments,
        executionMode: "automatic",
    });
});
