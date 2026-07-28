import { isSystemProxyBaseUrl, type AiConfig } from "@/stores/use-config-store";

type RelayConfig = Pick<AiConfig, "baseUrl">;

export type ChannelRequest = {
    url: string;
    headers: Record<string, string>;
    credentials: RequestCredentials;
};

/** 所有生成请求只允许使用后台配置的系统渠道。 */
export function channelRequest(config: RelayConfig, upstreamUrl: string, headers: HeadersInit = {}): ChannelRequest {
    const normalizedHeaders = new Headers(headers);
    if (!isSystemProxyBaseUrl(config.baseUrl)) {
        throw new Error("仅支持后台配置的系统模型渠道，请联系管理员");
    }
    return { url: upstreamUrl, headers: Object.fromEntries(normalizedHeaders.entries()), credentials: "include" };
}
