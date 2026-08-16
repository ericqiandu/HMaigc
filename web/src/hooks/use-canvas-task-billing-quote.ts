import { useMemo } from "react";

import { useTaskBillingQuote } from "@/hooks/use-task-billing-quote";
import { systemProviderTaskConfig } from "@/lib/ai/system-provider-config";
import { buildTaskBillingQuoteRequest } from "@/lib/billing/task-billing-quote";
import { resolveModelChannel, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

export function useCanvasTaskBillingQuote(config: AiConfig, mode: "image" | "video" | "text" | "audio", operation: string, batchCount: number, referenceVideoCount: number) {
    const request = useMemo(() => {
        if (mode !== "image" && mode !== "video") return null;
        const channel = resolveModelChannel(config, config.model);
        if (channel.scope !== "system" || channel.enabled === false) return null;
        const providerConfig = systemProviderTaskConfig(resolveModelRequestConfig(config, config.model));
        return buildTaskBillingQuoteRequest({ mode, operation, batchCount, referenceVideoCount, config: providerConfig });
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
        referenceVideoCount,
    ]);

    return useTaskBillingQuote(request);
}
