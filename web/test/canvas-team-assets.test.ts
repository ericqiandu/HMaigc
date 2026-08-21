import { describe, expect, test } from "bun:test";

import { loadReadyTeamResourceItems, teamResourceToInsertPayload } from "../src/components/canvas/canvas-project-asset-modal";
import type { TeamResource } from "../src/services/api/teams";

function resource(id: string, status: TeamResource["status"], kind = "image"): TeamResource {
    return {
        id,
        userId: "user-1",
        teamId: "team-1",
        kind,
        status,
        provider: "oss",
        mimeType: kind === "video" ? "video/mp4" : "image/png",
        size: 128,
        width: 1280,
        height: 720,
        durationMs: kind === "video" ? 6000 : 0,
        error: status === "failed" ? "upload failed" : "",
        createdAt: "2026-08-18T00:00:00Z",
        updatedAt: "2026-08-18T00:00:00Z",
    };
}

describe("团队共享素材进入项目画布", () => {
    test("只接收 ready 的可视媒体资源，并使用鉴权文件地址", async () => {
        const items = await loadReadyTeamResourceItems("team-1", {
            listResources: async () => ({
                resources: [resource("ready-image", "ready"), resource("failed-image", "failed"), resource("ready-reference", "ready", "reference")],
            }),
            fileURL: (teamId, resourceId) => `/api/teams/${teamId}/resources/${resourceId}/file`,
        });

        expect(items).toHaveLength(1);
        expect(items[0]?.id).toBe("team-resource:ready-image");
        expect(items[0]?.teamResource.fileURL).toBe("/api/teams/team-1/resources/ready-image/file");
    });

    test("插入载荷保留团队资源稳定身份，不伪装成个人 OSS storageKey", () => {
        const payload = teamResourceToInsertPayload({
            id: "team-resource:ready-video",
            category: "team-shared",
            teamResource: {
                resource: resource("ready-video", "ready", "video"),
                fileURL: "/api/teams/team-1/resources/ready-video/file",
                title: "团队视频 · ready-vi",
            },
        });

        expect(payload).toEqual({
            kind: "video",
            url: "/api/teams/team-1/resources/ready-video/file",
            title: "团队视频 · ready-vi",
            width: 1280,
            height: 720,
            durationMs: 6000,
            bytes: 128,
            mimeType: "video/mp4",
            teamResource: { teamId: "team-1", resourceId: "ready-video" },
        });
        expect("storageKey" in payload).toBe(false);
    });
});
