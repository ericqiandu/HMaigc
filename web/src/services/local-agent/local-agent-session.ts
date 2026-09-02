export type LocalAgentConnection = { baseUrl: string; token: string };
export type LocalAgentSessionStore = {
    load: () => LocalAgentConnection | null;
    save: (connection: LocalAgentConnection) => void;
    clear: () => void;
};

const storageKey = "hmaigc-local-agent-connection:v1";

export function createLocalAgentSessionStore(storage: Pick<Storage, "getItem" | "setItem" | "removeItem">): LocalAgentSessionStore {
    return {
        load: () => {
            const encoded = storage.getItem(storageKey);
            if (!encoded) return null;
            return parseConnection(JSON.parse(encoded) as unknown);
        },
        save: (connection) => storage.setItem(storageKey, JSON.stringify(parseConnection(connection))),
        clear: () => storage.removeItem(storageKey),
    };
}

export function validateLocalAgentBaseUrl(value: string): string {
    const url = new URL(value);
    if (url.protocol !== "http:" || url.hostname !== "127.0.0.1" || url.username || url.password || url.search || url.hash || url.pathname !== "/") {
        throw new Error("本机 Agent 地址必须是无认证信息、查询参数与路径的 HTTP loopback URL");
    }
    if (!url.port) throw new Error("本机 Agent 地址必须包含显式端口");
    return url.origin;
}

export function validateLocalAgentToken(value: string): string {
    if (!value || value.length > 512) throw new Error("本机 Agent 令牌格式无效");
    let byteLength = 0;
    if (/^[0-9a-fA-F]+$/.test(value) && value.length % 2 === 0) {
        byteLength = value.length / 2;
    } else if (/^[A-Za-z0-9_-]+$/.test(value)) {
        const remainder = value.length % 4;
        if (remainder === 1) throw new Error("本机 Agent 令牌格式无效");
        byteLength = Math.floor(value.length / 4) * 3 + (remainder === 2 ? 1 : remainder === 3 ? 2 : 0);
    } else {
        throw new Error("本机 Agent 令牌格式无效");
    }
    if (byteLength < 32) throw new Error("本机 Agent 令牌强度不足");
    return value;
}

function parseConnection(value: unknown): LocalAgentConnection {
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("本机 Agent 会话配置无效");
    const source = value as Record<string, unknown>;
    if (Object.keys(source).some((key) => key !== "baseUrl" && key !== "token")) throw new Error("本机 Agent 会话配置包含未知字段");
    if (typeof source.baseUrl !== "string") throw new Error("本机 Agent 地址无效");
    if (typeof source.token !== "string") throw new Error("本机 Agent 令牌格式无效");
    return { baseUrl: validateLocalAgentBaseUrl(source.baseUrl), token: validateLocalAgentToken(source.token) };
}
