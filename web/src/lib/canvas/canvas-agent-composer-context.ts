import { renderSkillPrompt } from "@/lib/canvas/canvas-skill-mentions";
import type { UpdreamSkill } from "@/services/api/skills";
import { modelOptionName } from "@/stores/use-config-store";
import type {
    CanvasAgentGenerationModels,
    CanvasAgentSkillSelection,
} from "@/types/canvas";

export function toCanvasAgentSkillSelection(
    skill: Pick<UpdreamSkill, "dir" | "name" | "description" | "detail_text">,
): CanvasAgentSkillSelection {
    return {
        dir: skill.dir,
        name: skill.name,
        description: skill.description,
        detailText: skill.detail_text,
    };
}

export function buildCanvasAgentRequestText(
    text: string,
    models: CanvasAgentGenerationModels,
    skills: CanvasAgentSkillSelection[],
) {
    const explicitModels = [
        models.image ? `图片模型：${modelOptionName(models.image)}` : "",
        models.video ? `视频模型：${modelOptionName(models.video)}` : "",
    ].filter(Boolean);
    const skillInstructions = skills.map((skill) =>
        renderSkillPrompt({
            name: skill.name,
            description: skill.description,
            detail_text: skill.detailText,
        }),
    );

    return [
        explicitModels.length ? `用户在输入框中显式选择的生成模型：\n${explicitModels.join("\n")}` : "",
        skillInstructions.length ? `用户在输入框中显式选择的 Skills：\n${skillInstructions.join("\n\n")}` : "",
        `用户需求：${text}`,
    ].filter(Boolean).join("\n\n");
}
