import "./setup-happy-dom";

import { afterAll, afterEach, beforeEach, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import type { CanvasCollaborationState } from "../src/services/api/canvas-collaboration";
import type { TeamWorkspace } from "../src/services/api/teams";

let collaborationRequests = 0;
let teamRequests = 0;

const collaborationState: CanvasCollaborationState = {
    project: {
        id: "canvas-1",
        ownerUserId: "user-1",
        title: "测试画布",
        createdAt: "2026-08-21T00:00:00Z",
        updatedAt: "2026-08-21T00:00:00Z",
        nodes: [],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "lines",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        directorScenes: [],
        revision: 0,
        defaultTeamAccess: "editor",
    },
    access: {
        level: "manager",
        canEdit: true,
        canManage: true,
        teamSubscriptionActive: false,
    },
    collaborators: [],
    teamMembers: [],
};

const teamWorkspace: TeamWorkspace = {
    teams: [{
        team: {
            id: "team-1",
            ownerUserId: "user-1",
            name: "测试团队",
            status: "active",
            createdAt: "2026-08-21T00:00:00Z",
            updatedAt: "2026-08-21T00:00:00Z",
        },
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
        subscription: {
            planId: "team-plan",
            planName: "团队版",
            planTier: "team",
            seatLimit: 2,
            unlimitedTaskQueue: false,
            teamStorageBytes: 1_000_000,
            sharedAssetsEnabled: true,
            projectPermissionsEnabled: true,
            invoicingEnabled: false,
            commercialUseEnabled: true,
        },
        availableMicrocredits: 0,
        reservedMicrocredits: 0,
        storageUsedBytes: 0,
    }],
    incomingInvitations: [],
};

const canvasCollaborationApi = await import("../src/services/api/canvas-collaboration");
const teamsApi = await import("../src/services/api/teams");

mock.module("@/services/api/canvas-collaboration", () => ({
    ...canvasCollaborationApi,
    getCanvasCollaboration: async () => {
        collaborationRequests += 1;
        return collaborationState;
    },
    configureCanvasCollaboration: async () => collaborationState,
    updateCanvasCollaborator: async () => collaborationState,
    deleteCanvasCollaborator: async () => collaborationState,
}));

mock.module("@/services/api/teams", () => ({
    ...teamsApi,
    getTeamWorkspace: async () => {
        teamRequests += 1;
        return teamWorkspace;
    },
}));

const { CanvasCollaborationModal } = await import("../src/components/canvas/canvas-collaboration-modal");

let root: Root | null = null;

beforeEach(() => {
    collaborationRequests = 0;
    teamRequests = 0;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

afterAll(() => {
    mock.restore();
});

test("父级重渲染并更换状态回调时不会重新加载协作配置", async () => {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    await renderModal(() => undefined);
    expect(collaborationRequests).toBe(1);
    expect(teamRequests).toBe(1);

    await renderModal(() => undefined);
    expect(collaborationRequests).toBe(1);
    expect(teamRequests).toBe(1);
});

test("首次读取协作配置只初始化弹窗而不发布外部状态变更", async () => {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    let stateChanges = 0;

    await renderModal(() => {
        stateChanges += 1;
    });

    expect(collaborationRequests).toBe(1);
    expect(teamRequests).toBe(1);
    expect(stateChanges).toBe(0);
});

test("协作配置写入成功后仍向外发布一次权威状态", async () => {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    let stateChanges = 0;

    await renderModal(() => {
        stateChanges += 1;
    });
    const enableButton = Array.from(document.querySelectorAll("button"))
        .find((button) => button.textContent?.includes("启用团队协作"));
    expect(enableButton).toBeDefined();

    await act(async () => {
        enableButton?.click();
        await Promise.resolve();
    });

    expect(collaborationRequests).toBe(1);
    expect(teamRequests).toBe(1);
    expect(stateChanges).toBe(1);
});

async function renderModal(onStateChange: (state: CanvasCollaborationState) => void) {
    await act(async () => {
        root?.render(createElement(
            ConfigProvider,
            null,
            createElement(App, null, createElement(CanvasCollaborationModal, {
                projectId: "canvas-1",
                open: true,
                onClose: () => undefined,
                onStateChange,
            })),
        ));
    });
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
