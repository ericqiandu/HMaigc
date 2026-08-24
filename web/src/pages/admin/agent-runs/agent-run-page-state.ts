import type { AdminAgentRun, AdminAgentRunPage } from "@/services/api/admin-agent-runs";

export type AgentRunPageState = {
    data: AdminAgentRunPage | null;
    loading: boolean;
    refreshing: boolean;
    error: string;
};

export type AgentRunInterruptDraft = {
    run: AdminAgentRun;
    reason: string;
    confirmation: string;
    submitting: boolean;
    error: string;
};

export function startAgentRunPageLoad(state: AgentRunPageState): AgentRunPageState {
    return {
        ...state,
        loading: state.data === null,
        refreshing: state.data !== null,
        error: "",
    };
}

export function succeedAgentRunPageLoad(state: AgentRunPageState, data: AdminAgentRunPage): AgentRunPageState {
    return { ...state, data, loading: false, refreshing: false, error: "" };
}

export function failAgentRunPageLoad(state: AgentRunPageState, error: string): AgentRunPageState {
    return { ...state, loading: false, refreshing: false, error };
}

export function applyAgentRunConflict(
    draft: AgentRunInterruptDraft,
    latestRun: AdminAgentRun,
    error: string,
): AgentRunInterruptDraft {
    return {
        ...draft,
        run: latestRun,
        confirmation: "",
        submitting: false,
        error,
    };
}
