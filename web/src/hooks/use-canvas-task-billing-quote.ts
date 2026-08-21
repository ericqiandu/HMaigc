import { useMemo } from "react";

import { useTaskBillingQuote } from "@/hooks/use-task-billing-quote";
import { systemProviderTaskConfig } from "@/lib/ai/system-provider-config";
import { buildTaskBillingQuoteRequest } from "@/lib/billing/task-billing-quote";
import { resolveModelChannel, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export type CanvasTaskBillingQuoteUsage = {
    referenceImageCount: number;
    referenceVideoCount: number;
};

export function useCanvasTaskBillingQuote(projectId: string, config: AiConfig, mode: "image" | "video" | "text" | "audio", operation: string, batchCount: number, usage: CanvasTaskBillingQuoteUsage) {
    const request = useMemo(() => {
        if (mode !== "image" && mode !== "video") return null;
        const channel = resolveModelChannel(config, config.model);
        if (channel.scope !== "system" || channel.enabled === false) return null;
        const providerConfig = systemProviderTaskConfig(resolveModelRequestConfig(config, config.model));
        return buildTaskBillingQuoteRequest({ projectId, mode, operation, batchCount, usage, config: providerConfig });
    }, [
        batchCount,
        config.channels,
        config.model,
        config.quality,
        config.size,
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
    ]);

    return useTaskBillingQuote(request);
}
