import { resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export function resolveAgentDefaultRequestConfig(config: AiConfig, agentDefaultModel: string) {
    if (!agentDefaultModel.trim() || !config.models.includes(agentDefaultModel)) {
        throw new Error("管理员尚未配置可用的 Agent 模型");
    }
    const resolved = resolveModelRequestConfig(config, agentDefaultModel);
    if (!resolved.model.trim() || !resolved.baseUrl.trim() || !resolved.apiKey.trim()) {
        throw new Error("管理员尚未配置可用的 Agent 模型");
    }
    return resolved;
}
