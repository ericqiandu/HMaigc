import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { UserDataRequestError, type RemoteCanvasDeletion } from "@/services/api/user-data";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";

import { isRemoteCanvasDeletedError, mergeCanvasProjects } from "./canvas-sync-state";

function project(id: string, updatedAt: string): CanvasProject {
    return {
        id,
        title: id,
        createdAt: "2026-08-25T00:00:00Z",
        updatedAt,
        nodes: [],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "lines",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        directorScenes: [],
    };
}

describe("画布跨浏览器同步状态", () => {
    test("服务端删除事实优先于更新时间更晚的本地副本", () => {
        const local = [
            project("deleted-canvas", "2026-08-25T03:00:00Z"),
            project("local-only", "2026-08-25T02:00:00Z"),
        ];
        const remote = [project("remote-canvas", "2026-08-25T01:00:00Z")];
        const deletions: RemoteCanvasDeletion[] = [
            { id: "deleted-canvas", deletedAt: "2026-08-25T04:00:00Z" },
        ];

        assert.deepEqual(
            mergeCanvasProjects(local, remote, deletions).map((item) => item.id),
            ["local-only", "remote-canvas"],
        );
    });

    test("410 是唯一会把远端创建失败判定为权威删除的错误", () => {
        assert.equal(isRemoteCanvasDeletedError(new UserDataRequestError("已删除", 410)), true);
        assert.equal(isRemoteCanvasDeletedError(new UserDataRequestError("冲突", 409)), false);
        assert.equal(isRemoteCanvasDeletedError(new Error("网络错误")), false);
        assert.equal(isRemoteCanvasDeletedError({ status: 410 }), false);
    });
});
