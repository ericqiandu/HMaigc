import { useMemo } from "react";

import { useTaskBillingQuote } from "@/hooks/use-task-billing-quote";
import { systemProviderTaskConfig } from "@/lib/ai/system-provider-config";
import { buildTaskBillingQuoteRequest } from "@/lib/billing/task-billing-quote";
import { configuredModelMatchesCapability, resolveModelChannel, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export type CanvasTaskBillingQuoteUsage = {
    referenceImageCount: number;
    referenceVideoCount: number;
};

export function buildCanvasTaskBillingQuoteRequest(projectId: string, config: AiConfig, mode: "image" | "video" | "text" | "audio", operation: string, batchCount: number, usage: CanvasTaskBillingQuoteUsage) {
    if (mode !== "image" && mode !== "video") return null;
    if (!configuredModelMatchesCapability(config, config.model, mode)) return null;
    const channel = resolveModelChannel(config, config.model);
    if (channel.scope !== "system" || channel.enabled === false) return null;
    const providerConfig = systemProviderTaskConfig(resolveModelRequestConfig(config, config.model));
    return buildTaskBillingQuoteRequest({ projectId, mode, operation, batchCount, usage, config: providerConfig });
}

export function useCanvasTaskBillingQuote(projectId: string, config: AiConfig, mode: "image" | "video" | "text" | "audio", operation: string, batchCount: number, usage: CanvasTaskBillingQuoteUsage) {
    const request = useMemo(
        () => buildCanvasTaskBillingQuoteRequest(projectId, config, mode, operation, batchCount, usage),
        [
            batchCount,
            config.channels,
            config.model,
            config.models,
            config.quality,
            config.size,
            config.videoGenerateAudio,
            config.videoSeconds,
            config.videoSuperResolutionEnabled,
            config.videoSuperResolutionFps,
            config.videoSuperResolutionResolution,
            config.videoSuperResolutionVersion,
            config.vquality,
            mode,
            operation,
            projectId,
            usage.referenceImageCount,
            usage.referenceVideoCount,
        ],
    );

    return useTaskBillingQuote(request);
}
