import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useTeamCommands, type TeamCommandsApi, type TeamCommandsController } from "../src/pages/teams/use-team-commands";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

function Probe({ api, ready, reloadActiveTeam = async () => undefined }: { api: TeamCommandsApi; ready: (controller: TeamCommandsController) => void; reloadActiveTeam?: () => Promise<void> }) {
    const controller = useTeamCommands({ activeTeamId: "team-1", api, reloadActiveTeam, reloadWorkspace: async () => undefined });
    ready(controller);
    return createElement("output", { "data-busy": controller.busyKey, "data-error": controller.commandError });
}

describe("团队写命令协调器", () => {
    test("同一时刻只提交一个写命令并保留显式错误", async () => {
        let resolveCreate!: () => void;
        let calls = 0;
        const api: TeamCommandsApi = {
            createTeam: async () => {
                calls += 1;
                await new Promise<void>((resolve) => {
                    resolveCreate = resolve;
                });
                return { id: "team-1", ownerUserId: "owner", name: "映像组", status: "active", createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:00Z" };
            },
        };
        let commands: TeamCommandsController | null = null;
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () =>
            root?.render(
                createElement(Probe, {
                    api,
                    ready: (controller) => {
                        commands = controller;
                    },
                }),
            ),
        );
        if (!commands) throw new Error("team command controller missing");

        let first!: ReturnType<TeamCommandsController["create"]>;
        await act(async () => {
            first = commands.create("映像组", "create-key");
            await Promise.resolve();
        });
        await act(async () => {
            await expect(commands.create("映像组", "create-key")).rejects.toThrow("已有团队操作正在执行");
        });
        expect(calls).toBe(1);
        resolveCreate();
        await act(async () => first);

        const output = document.querySelector<HTMLOutputElement>("output");
        expect(output?.dataset.busy).toBe("");
        expect(output?.dataset.error).toBe("已有团队操作正在执行，请等待完成后重试");
    });

    test("写操作成功但刷新失败时仍返回一次性邀请令牌并显式提示刷新错误", async () => {
        const api: TeamCommandsApi = {
            createInvitation: async () => ({
                acceptToken: "one-time-token",
                invitation: {
                    id: "invitation-1",
                    teamId: "team-1",
                    inviterUserId: "owner",
                    email: "member@example.com",
                    role: "editor",
                    status: "pending",
                    expiresAt: "2026-08-19T00:00:00Z",
                    createdAt: "2026-08-18T00:00:00Z",
                    updatedAt: "2026-08-18T00:00:00Z",
                },
            }),
        };
        let commands: TeamCommandsController | null = null;
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () =>
            root?.render(
                createElement(Probe, {
                    api,
                    reloadActiveTeam: async () => {
                        throw new Error("network timeout");
                    },
                    ready: (controller) => {
                        commands = controller;
                    },
                }),
            ),
        );
        if (!commands) throw new Error("team command controller missing");

        let result!: Awaited<ReturnType<TeamCommandsController["invite"]>>;
        await act(async () => {
            result = await commands.invite("member@example.com", "editor");
        });

        expect(result.acceptToken).toBe("one-time-token");
        expect(document.querySelector<HTMLOutputElement>("output")?.dataset.error).toBe("操作已成功，但刷新团队数据失败：network timeout");
    });
});
