import { expect, test } from "bun:test";

import {
    parseAgentArtifactRevision,
    parseAgentProductionTimelineContent,
    parseStageReviewResult,
} from "../src/services/api/agent-production";
import { agentProductionClient } from "../src/services/api/agent-production-client";
import { parseAgentRuntimeEvent } from "../src/services/api/agent-runtime";

const artifactRevision = {
    artifactId: "artifact-script",
    revisionId: "revision-script-1",
    artifactKey: "script-main",
    revision: 1,
    kind: "script_bundle",
    schemaVersion: 1,
    payload: {
        title: "纸飞机",
        logline: "一个孩子用纸飞机找回父亲的记忆。",
        script: "第一场：屋顶，黄昏。",
        characters: [{ key: "child", name: "小雨", description: "十岁，安静但坚定" }],
        scenes: [{ key: "roof", name: "老屋屋顶", description: "黄昏逆光" }],
        props: [{ key: "plane", name: "纸飞机", description: "折痕明显" }],
        voiceRoles: [],
    },
    upstreamRevisions: [],
    skillVersions: [],
    lifecycleStatus: "awaiting_review",
    createdAt: "2026-08-28T00:00:00Z",
};

test("产物版本按 schema 严格解析且拒绝短期地址和未知字段", () => {
    expect(parseAgentArtifactRevision(artifactRevision)).toEqual(artifactRevision);
    expect(() => parseAgentArtifactRevision({ ...artifactRevision, payload: { ...artifactRevision.payload, debug: true } })).toThrow("未知字段");
    expect(() => parseAgentArtifactRevision({ ...artifactRevision, resourceId: "resource-1", signedUrl: "https://example.test/temporary" })).toThrow("短期媒体地址");
});

test("所有生产产物 schema 都拒绝嵌套字段类型损坏", () => {
    const malformedPayloads = [
        ["asset_binding", 1, { bindingKey: "binding-1", scriptRevision: null, confirmed: true, entries: [] }],
        ["visual_evidence", 1, { sourceRevision: null, characters: [], identityEvidence: [], scene: {}, props: [], spatialRelations: [], shot: {}, actionState: "静止", ocrText: [], uncertainties: [], conflicts: [], confidenceBasisPoints: 9000, visionModelRecordId: "model-1", requestIdentity: "request-1" }],
        ["character_visual_bible", 1, { scriptRevision: null, assetBindingRevision: null, visualEvidenceRevisions: [], referenceAssetRevisions: [], characters: [] }],
        ["storyboard_plan", 1, { scriptRevision: null, assetBindingRevision: null, characterBibleRevision: null, visualEvidenceRevisions: [], targetDurationMs: "6000", shots: [] }],
        ["camera_tree", 1, { storyboardRevision: null, visualEvidenceRevisions: [], shotKeys: [], cameras: [], relations: [], missingViews: [] }],
        ["first_motion_last_frame", 1, { firstFrame: null, motion: "推进", lastFrame: null, inputRevisions: [], continuityConditions: [] }],
        ["media_candidate_selection", 1, { stageId: "stage-1", reviewRevision: null, selectedCandidateRevision: null, approvedByUserId: "user-1", clientRequestId: "request-1" }],
        ["video_plan", 1, { planKey: "video-1", inputRevisions: [], audioMode: "native", segments: [{ segmentKey: "segment-1" }] }],
        ["audio_plan", 1, { planKey: "audio-1", inputRevisions: [], clips: [{ clipKey: "clip-1", startMs: "0" }] }],
        ["assembly_plan", 1, { planKey: "assembly-1", audioMode: "none", videoRevisions: null, audioRevisions: [], outputArtifactKey: "output-1" }],
    ] as const;

    for (const [kind, schemaVersion, payload] of malformedPayloads) {
        expect(() => parseAgentArtifactRevision({ ...artifactRevision, kind, schemaVersion, payload })).toThrow();
    }
});

test("审核、决议与资产入库事件保留精确的耐久身份", () => {
    expect(
        parseAgentProductionTimelineContent({
            contentType: "artifact_review",
            stageId: "stage-script",
            stageVersion: 3,
            artifactId: "artifact-script",
            revisionId: "revision-script-1",
            artifactSchema: "script_bundle.v1",
            summary: "剧本初稿待确认",
        }),
    ).toEqual(expect.objectContaining({ contentType: "artifact_review", stageVersion: 3, revisionId: "revision-script-1" }));
    expect(
        parseAgentProductionTimelineContent({
            contentType: "asset_publication",
            publicationId: "publication-1",
            artifactRevisionId: "revision-image-1",
            resourceId: "resource-image-1",
            assetId: "asset-1",
            assetVersionId: "asset-version-1",
            projectAssetLinkId: "asset-link-1",
            representationId: "representation-1",
            publicationPurpose: "character-library",
            targetCategory: "character",
            targetBindingKey: "hero",
        }),
    ).toEqual(expect.objectContaining({ contentType: "asset_publication", resourceId: "resource-image-1" }));
});

test("最终装配计划与任务时间线只接受显式 v2 事实", () => {
    const plan = {
        ...artifactRevision,
        artifactId: "artifact-assembly",
        revisionId: "revision-assembly-2",
        artifactKey: "assembly-plan",
        kind: "assembly_plan",
        schemaVersion: 2,
        payload: {
            planKey: "assembly-plan",
            audioMode: "none",
            clips: [{
                clipKey: "clip-1",
                sourceRevision: { artifactId: "video-1", revisionId: "video-r1" },
                trimStartMs: 0,
                trimEndMs: 5_000,
                nativeAudioGainMilliDb: null,
                transitionToNext: { kind: "cut", durationMs: 0 },
            }],
            audioTracks: [],
            output: {
                artifactKey: "final-video",
                container: "mp4",
                videoCodec: "h264",
                audioCodec: "none",
                width: 1_920,
                height: 1_080,
                frameRate: 24,
            },
        },
    };
    expect(parseAgentArtifactRevision(plan)).toEqual(plan);

    const timeline = {
        contentType: "media_assembly",
        toolCallId: "assemble-final",
        actionVersion: 1,
        taskId: "assembly-task",
        taskStatus: "succeeded",
        stage: "装配完成",
        clipCount: 1,
        audioMode: "none",
        output: plan.payload.output,
        planRevision: { artifactId: "artifact-assembly", revisionId: "revision-assembly-2" },
        final: {
            artifactRevision: { artifactId: "final-video", revisionId: "final-video-r1" },
            resourceId: "final-resource",
            adopted: true,
        },
    };
    expect(parseAgentProductionTimelineContent(timeline)).toEqual(timeline);
    expect(() => parseAgentProductionTimelineContent({ ...timeline, progress: 100 })).toThrow("未知字段");
    expect(() => parseAgentProductionTimelineContent({ ...timeline, final: { ...timeline.final, url: "https://temporary.invalid/video.mp4" } })).toThrow("短期媒体地址");
    expect(() => parseAgentProductionTimelineContent({ ...timeline, reasoning: "内部思考" })).toThrow("未知字段");
});

test("最终装配生命周期以 tool_call 事件进入统一 Agent 时间线", () => {
    const payload = {
        contentType: "media_assembly",
        toolCallId: "assemble-final",
        actionVersion: 1,
        taskId: "assembly-task",
        taskStatus: "running",
        stage: "正在装配",
        clipCount: 1,
        audioMode: "none",
        output: {
            artifactKey: "final-video",
            container: "mp4",
            videoCodec: "h264",
            audioCodec: "none",
            width: 1_920,
            height: 1_080,
            frameRate: 24,
        },
        planRevision: { artifactId: "artifact-assembly", revisionId: "revision-assembly-2" },
    };
    const event = {
        protocolVersion: 4,
        threadId: "thread-1",
        runId: "run-1",
        sequence: 7,
        kind: "item.delta",
        itemId: "assembly-call-1",
        itemKind: "tool_call",
        payload,
        createdAt: "2026-08-28T00:00:02Z",
    };

    expect(parseAgentRuntimeEvent(event)).toEqual(event);
    expect(() => parseAgentRuntimeEvent({ ...event, itemKind: "artifact" })).toThrow("item kind 不一致");
});

test("阶段结果使用生产状态全集并拒绝非 required 审核策略", () => {
    const planned = {
        stage: {
            id: "stage-script",
            stageKey: "script",
            specialistKey: "narrative",
            reviewPolicy: "required",
            costPolicy: "none",
            status: "planned",
            version: 1,
            updatedAt: "2026-08-28T00:00:00Z",
        },
        artifactRevisionIds: [],
    };
    expect(parseStageReviewResult(planned).stage.status).toBe("planned");
    expect(parseStageReviewResult({ ...planned, stage: { ...planned.stage, status: "stale" } }).stage.status).toBe("stale");
    expect(() => parseStageReviewResult({ ...planned, stage: { ...planned.stage, reviewPolicy: "automatic" } })).toThrow("审核策略无效");
});

test("产物审核客户端编码精确路径并原样提交候选入库意图", async () => {
    const originalFetch = globalThis.fetch;
    const requests: Array<{ url: string; method?: string; body?: unknown }> = [];
    globalThis.fetch = (async (input, init) => {
        requests.push({ url: String(input), method: init?.method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
        const data = init?.method === "POST"
            ? {
                  stage: {
                      id: "stage-visual",
                      stageKey: "visual-review",
                      specialistKey: "visual",
                      reviewPolicy: "required",
                      costPolicy: "none",
                      status: "approved",
                      version: 4,
                      reviewRevisionId: "revision-review-1",
                      updatedAt: "2026-08-28T00:00:01Z",
                  },
                  artifactRevisionIds: ["revision-selection-1"],
                  selectedCandidateRevisionId: "revision-image-1",
              }
            : artifactRevision;
        return new Response(JSON.stringify({ code: 0, data, msg: "ok" }), { status: 200, headers: { "Content-Type": "application/json" } });
    }) as typeof fetch;
    try {
        await agentProductionClient.getArtifactRevision("run /1", "artifact /1", "revision /1");
        await agentProductionClient.reviewStage("run /1", "stage /1", {
            stageVersion: 3,
            revisionId: "revision-review-1",
            decision: "approved",
            selectedCandidateRevisionId: "revision-image-1",
            clientRequestId: "approval-1",
            comment: "",
            publicationIntent: { publicationPurpose: "character-library", targetCategory: "character", targetBindingKey: "hero" },
        });
        expect(requests[0]?.url).toEndWith("/agent/runs/run%20%2F1/artifacts/artifact%20%2F1/revisions/revision%20%2F1");
        expect(requests[1]).toEqual({
            url: expect.stringContaining("/agent/runs/run%20%2F1/stages/stage%20%2F1/reviews"),
            method: "POST",
            body: {
                stageVersion: 3,
                revisionId: "revision-review-1",
                decision: "approved",
                selectedCandidateRevisionId: "revision-image-1",
                clientRequestId: "approval-1",
                comment: "",
                publicationIntent: { publicationPurpose: "character-library", targetCategory: "character", targetBindingKey: "hero" },
            },
        });
    } finally {
        globalThis.fetch = originalFetch;
    }
});

test("阶段审核响应拒绝非契约资产发布字段", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
        new Response(
            JSON.stringify({
                code: 0,
                msg: "ok",
                data: {
                    stage: {
                        id: "stage-visual",
                        stageKey: "visual-review",
                        specialistKey: "visual",
                        reviewPolicy: "required",
                        costPolicy: "none",
                        status: "approved",
                        version: 4,
                        reviewRevisionId: "revision-review-1",
                        updatedAt: "2026-08-28T00:00:01Z",
                    },
                    artifactRevisionIds: ["revision-selection-1"],
                    selectedCandidateRevisionId: "revision-image-1",
                    publication: {
                        id: "publication-1",
                        artifactRevisionId: "revision-image-1",
                        assetId: "asset-1",
                        assetVersionId: "asset-version-1",
                        projectAssetLinkId: "asset-link-1",
                        representationId: "representation-1",
                        status: "succeeded",
                        replayed: false,
                        debug: true,
                    },
                },
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
        )) as typeof fetch;
    try {
        await expect(
            agentProductionClient.reviewStage("run-1", "stage-visual", {
                stageVersion: 3,
                revisionId: "revision-review-1",
                decision: "approved",
                selectedCandidateRevisionId: "revision-image-1",
                clientRequestId: "approval-1",
                comment: "",
                publicationIntent: { publicationPurpose: "character-library", targetCategory: "character", targetBindingKey: "hero" },
            }),
        ).rejects.toThrow("未知字段");
    } finally {
        globalThis.fetch = originalFetch;
    }
});
