import type { PlatformSkill } from "@/services/api/skills";
import type { CanvasAgentSkillSelection } from "@/types/canvas";

export function toCanvasAgentSkillSelection(skill: Pick<PlatformSkill, "dir" | "name" | "description">): CanvasAgentSkillSelection {
    return {
        dir: skill.dir,
        name: skill.name,
        description: skill.description,
    };
}
