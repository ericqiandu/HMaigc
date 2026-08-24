import type { PlatformSkill } from "@/services/api/skills";
import { encodeChannelModel, isChannelModelValue } from "@/stores/use-config-store";
import { CanvasNodeType, type CanvasAgentGenerationModels, type CanvasAgentSkillSelection, type CanvasNodeData } from "@/types/canvas";
import { findMentionedSkills } from "./canvas-skill-mentions";

export function toCanvasAgentSkillSelection(skill: Pick<PlatformSkill, "dir" | "name" | "description">): CanvasAgentSkillSelection {
    return {
        dir: skill.dir,
        name: skill.name,
        description: skill.description,
    };
}

export function deriveCanvasAgentSelectionDefaults(selectedNodes: CanvasNodeData[], activatedSkills: PlatformSkill[]) {
    const selectedVideos = selectedNodes.filter((node) => node.type === CanvasNodeType.Video);
    const videoModels = Array.from(new Set(selectedVideos.map(videoModelSelection).filter(Boolean)));
    const prompt = selectedVideos
        .flatMap((node) => [node.metadata?.composerContent, node.metadata?.prompt])
        .filter((value): value is string => Boolean(value?.trim()))
        .join("\n");

    return {
        generationModels: { image: "", video: videoModels.length === 1 ? videoModels[0] : "" } satisfies CanvasAgentGenerationModels,
        skillSelections: findMentionedSkills(prompt, activatedSkills).map(toCanvasAgentSkillSelection),
    };
}

function videoModelSelection(node: CanvasNodeData) {
    const model = node.metadata?.model?.trim() || "";
    if (!model) return "";
    if (isChannelModelValue(model)) return model;
    const channelId = node.metadata?.channelId?.trim();
    return channelId ? encodeChannelModel(channelId, model) : "";
}
