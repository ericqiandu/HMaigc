import type { CanvasAgentExecutionMode, CanvasAgentGenerationModels, CanvasAgentSkillSelection } from "@/types/canvas";

export type CanvasAgentDraftAttachment = {
    id: string;
    name: string;
    url: string;
    resourceId?: string;
};

export type CanvasAgentDraft = {
    prompt: string;
    attachments: CanvasAgentDraftAttachment[];
    generationModels: CanvasAgentGenerationModels;
    skillSelections: CanvasAgentSkillSelection[];
    executionMode: CanvasAgentExecutionMode;
};

export function createEmptyCanvasAgentDraft(): CanvasAgentDraft {
    return {
        prompt: "",
        attachments: [],
        generationModels: { image: "", video: "" },
        skillSelections: [],
        executionMode: "guided",
    };
}

export function removeLastCanvasAgentDraftSelection(draft: CanvasAgentDraft): CanvasAgentDraft | null {
    if (draft.skillSelections.length) {
        return { ...draft, skillSelections: draft.skillSelections.slice(0, -1) };
    }
    if (draft.generationModels.video) {
        return { ...draft, generationModels: { ...draft.generationModels, video: "" } };
    }
    if (draft.generationModels.image) {
        return { ...draft, generationModels: { ...draft.generationModels, image: "" } };
    }
    return null;
}
