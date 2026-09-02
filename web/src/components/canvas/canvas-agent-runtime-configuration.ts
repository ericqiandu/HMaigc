import type { AgentRuntimeStartConfiguration } from "@/services/api/agent-runtime";
import { decodeChannelModel } from "@/stores/use-config-store";
import type { CanvasAgentGenerationModels, CanvasAgentSkillSelection } from "@/types/canvas";

export const AGENT_RUNTIME_DECISION_BUDGET = 24;

export function buildAgentStartConfiguration(
    models: CanvasAgentGenerationModels,
    skills: CanvasAgentSkillSelection[],
    attachments: Array<{ resourceId?: string; name: string }>,
    executionMode: AgentRuntimeStartConfiguration["executionMode"],
): AgentRuntimeStartConfiguration {
    const configuration: AgentRuntimeStartConfiguration = { generationModels: {}, skillDirs: [], attachments: [], executionMode };
    if (models.image) configuration.generationModels.image = requiredModelSelection(models.image, "图片模型");
    if (models.video) configuration.generationModels.video = requiredModelSelection(models.video, "视频模型");
    configuration.skillDirs = skills
        .map((skill) => skill.dir.trim())
        .filter(Boolean)
        .sort();
    configuration.attachments = attachments.map((attachment) => {
        const resourceId = attachment.resourceId?.trim();
        if (!resourceId) throw new Error(`参考图片“${attachment.name}”缺少账号资源事实`);
        return { resourceId, name: attachment.name.trim() || "参考图片" };
    });
    return configuration;
}

export function encodeAgentModelSelection(selection: { channelId: string; model: string } | undefined): string {
    return selection ? `${selection.channelId}::${selection.model}` : "";
}

function requiredModelSelection(value: string, label: string) {
    const selected = decodeChannelModel(value);
    if (!selected?.channelId.trim() || !selected.model.trim()) throw new Error(`${label}配置无效，请重新选择`);
    return selected;
}
