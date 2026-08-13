import { describe, expect, test } from "bun:test";

import { defaultConfig, resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";

import { systemProviderTaskConfig } from "@/lib/ai/system-provider-config";

function systemConfig(): AiConfig {
    return {
        ...defaultConfig,
        model: "text-model",
        channels: [
            {
                id: "channel-1",
                name: "系统文本渠道",
                baseUrl: "/api/ai/system/channel-1",
                apiKey: "system",
                apiFormat: "openai",
                interfaceType: "chat-completion",
                models: ["text-model"],
                scope: "system",
                enabled: true,
                hasApiKey: true,
            },
        ],
    };
}

describe("systemProviderTaskConfig", () => {
    test("only sends the system channel selector and generation parameters", () => {
        const config = resolveModelRequestConfig(systemConfig(), "text-model");
        const result = systemProviderTaskConfig(config);

        expect(result.channelId).toBe("channel-1");
        expect(result.model).toBe("text-model");
        expect("apiKey" in result).toBe(false);
        expect("baseUrl" in result).toBe(false);
        expect("apiFormat" in result).toBe(false);
        expect("interfaceType" in result).toBe(false);
        expect("systemPrompt" in result).toBe(false);
        expect("watermark" in result).toBe(false);
        expect("videoWatermark" in result).toBe(false);
    });

    test("rejects models that are not bound to a system channel", () => {
        const config = resolveModelRequestConfig(defaultConfig, "unbound-model");
        expect(() => systemProviderTaskConfig(config)).toThrow("当前模型未绑定后台系统渠道");
    });
});
