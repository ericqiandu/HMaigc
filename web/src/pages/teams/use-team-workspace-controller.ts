import { useCallback, useEffect, useRef, useState } from "react";

import { getTeamDetail, getTeamWorkspace, type TeamDetail, type TeamWorkspace } from "@/services/api/teams";

export type TeamWorkspaceControllerApi = {
    getWorkspace: () => Promise<TeamWorkspace>;
    getDetail: (teamId: string) => Promise<TeamDetail>;
};

export type TeamWorkspaceController = {
    workspace: TeamWorkspace | null;
    detail: TeamDetail | null;
    activeTeamId: string;
    workspaceStatus: "loading" | "ready" | "error";
    detailStatus: "idle" | "loading" | "ready" | "error";
    workspaceError: string;
    detailError: string;
    selectTeam: (teamId: string) => void;
    reloadWorkspace: (preferredTeamId?: string) => Promise<void>;
    reloadDetail: (teamId?: string) => Promise<void>;
};

type UseTeamWorkspaceControllerInput = {
    api?: TeamWorkspaceControllerApi;
    teamId: string;
    setTeamId: (teamId: string) => void;
};

const defaultApi: TeamWorkspaceControllerApi = {
    getWorkspace: getTeamWorkspace,
    getDetail: getTeamDetail,
};

function errorMessage(error: unknown, fallback: string) {
    return error instanceof Error && error.message ? error.message : fallback;
}

export function useTeamWorkspaceController({ api = defaultApi, teamId, setTeamId }: UseTeamWorkspaceControllerInput): TeamWorkspaceController {
    const [workspace, setWorkspace] = useState<TeamWorkspace | null>(null);
    const [detail, setDetail] = useState<TeamDetail | null>(null);
    const [workspaceStatus, setWorkspaceStatus] = useState<TeamWorkspaceController["workspaceStatus"]>("loading");
    const [detailStatus, setDetailStatus] = useState<TeamWorkspaceController["detailStatus"]>(teamId ? "loading" : "idle");
    const [workspaceError, setWorkspaceError] = useState("");
    const [detailError, setDetailError] = useState("");
    const workspaceRequest = useRef(0);
    const detailRequest = useRef(0);
    const teamIdRef = useRef(teamId);
    teamIdRef.current = teamId;

    const reloadWorkspace = useCallback(
        async (preferredTeamId?: string) => {
            const request = ++workspaceRequest.current;
            setWorkspaceStatus("loading");
            setWorkspaceError("");
            try {
                const next = await api.getWorkspace();
                if (request !== workspaceRequest.current) return;
                setWorkspace(next);
                setWorkspaceStatus("ready");
                const currentTeamId = teamIdRef.current;
                const candidate = preferredTeamId ?? currentTeamId;
                if (candidate && next.teams.some((item) => item.team.id === candidate)) {
                    if (candidate !== currentTeamId) setTeamId(candidate);
                    return;
                }
                const firstTeamId = next.teams[0]?.team.id ?? "";
                if (firstTeamId !== currentTeamId) setTeamId(firstTeamId);
            } catch (error) {
                if (request !== workspaceRequest.current) return;
                setWorkspaceStatus("error");
                setWorkspaceError(errorMessage(error, "读取团队空间失败"));
            }
        },
        [api, setTeamId],
    );

    const reloadDetail = useCallback(
        async (requestedTeamId = teamIdRef.current) => {
            const request = ++detailRequest.current;
            if (!requestedTeamId) {
                setDetail(null);
                setDetailStatus("idle");
                setDetailError("");
                return;
            }
            setDetailStatus("loading");
            setDetailError("");
            setDetail((current) => (current?.summary.team.id === requestedTeamId ? current : null));
            try {
                const next = await api.getDetail(requestedTeamId);
                if (request !== detailRequest.current) return;
                setDetail(next);
                setDetailStatus("ready");
            } catch (error) {
                if (request !== detailRequest.current) return;
                setDetail(null);
                setDetailStatus("error");
                setDetailError(errorMessage(error, "读取团队详情失败"));
            }
        },
        [api],
    );

    const selectTeam = useCallback(
        (nextTeamId: string) => {
            if (nextTeamId !== teamId) setTeamId(nextTeamId);
        },
        [setTeamId, teamId],
    );

    useEffect(() => {
        void reloadWorkspace();
    }, [reloadWorkspace]);

    useEffect(() => {
        void reloadDetail(teamId);
    }, [reloadDetail, teamId]);

    return {
        workspace,
        detail,
        activeTeamId: teamId,
        workspaceStatus,
        detailStatus,
        workspaceError,
        detailError,
        selectTeam,
        reloadWorkspace,
        reloadDetail,
    };
}
