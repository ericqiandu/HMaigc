import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement, useState } from "react";
import { createRoot, type Root } from "react-dom/client";

import { useTeamWorkspaceController, type TeamWorkspaceControllerApi } from "../src/pages/teams/use-team-workspace-controller";
import type { TeamDetail, TeamSummary, TeamWorkspace } from "../src/services/api/teams";

type Deferred<T> = { promise: Promise<T>; resolve: (value: T) => void };

function deferred<T>(): Deferred<T> {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((resolvePromise) => {
        resolve = resolvePromise;
    });
    return { promise, resolve };
}

function summary(id: string, name: string): TeamSummary {
    return {
        team: { id, ownerUserId: "owner", name, status: "active", createdAt: "2026-08-18T00:00:00Z", updatedAt: "2026-08-18T00:00:00Z" },
        currentRole: "owner",
        capabilities: {
            canRenameTeam: true,
            canManageSubscription: true,
            canInviteMembers: true,
            inviteRoles: ["admin", "member"],
            canManageMemberRoles: true,
            canManageMemberCreditLimits: true,
            canRemoveMembers: true,
            canLeaveTeam: false,
            canManageProjects: true,
            canUploadSharedAssets: true,
            canViewAudit: true,
        },
        seatUsed: 1,
        invitationSeatReserved: 0,
        availableMicrocredits: 0,
        reservedMicrocredits: 0,
        storageUsedBytes: 0,
    };
}

function detail(value: TeamSummary): TeamDetail {
    return { summary: value, members: [], invitations: [], auditEvents: [] };
}

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

function Probe({ api }: { api: TeamWorkspaceControllerApi }) {
    const [teamId, setTeamId] = useState("team-a");
    const controller = useTeamWorkspaceController({ api, teamId, setTeamId });
    return createElement(
        "div",
        {},
        createElement("button", { type: "button", onClick: () => controller.selectTeam("team-b") }, "select-b"),
        createElement("output", { "data-team": controller.detail?.summary.team.id ?? "", "data-workspace": controller.workspaceStatus }, controller.detail?.summary.team.name ?? ""),
    );
}

describe("团队工作区读取控制器", () => {
    test("空数组进入 ready 空态且旧详情迟到不会覆盖当前团队", async () => {
        const teamA = summary("team-a", "团队 A");
        const teamB = summary("team-b", "团队 B");
        const detailA = deferred<TeamDetail>();
        const detailB = deferred<TeamDetail>();
        const workspace: TeamWorkspace = { teams: [teamA, teamB], incomingInvitations: [] };
        const api: TeamWorkspaceControllerApi = {
            getWorkspace: async () => workspace,
            getDetail: (teamId) => (teamId === "team-a" ? detailA.promise : detailB.promise),
        };
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);
        await act(async () => root?.render(createElement(Probe, { api })));
        await act(async () => Promise.resolve());

        const button = document.querySelector<HTMLButtonElement>("button");
        if (!button) throw new Error("team selector missing");
        await act(async () => button.click());
        detailB.resolve(detail(teamB));
        await act(async () => detailB.promise);
        detailA.resolve(detail(teamA));
        await act(async () => detailA.promise);

        const output = document.querySelector<HTMLOutputElement>("output");
        expect(output?.dataset.team).toBe("team-b");
        expect(output?.textContent).toBe("团队 B");
        expect(output?.dataset.workspace).toBe("ready");
    });
});
