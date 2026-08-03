export const MODEL_BRAND_KEYS = ["generic", "openai", "google", "anthropic", "deepseek", "xai", "zhipu", "minimax", "seedance", "kling", "qwen", "alibaba", "volcengine", "runway", "luma", "pika", "flux", "stability", "ideogram", "recraft"] as const;

export type ModelBrandKey = (typeof MODEL_BRAND_KEYS)[number];

export type ModelBrandDefinition = {
    key: ModelBrandKey;
    label: string;
    mark: string;
    asset?: string;
};

const definitions: Record<ModelBrandKey, ModelBrandDefinition> = {
    generic: { key: "generic", label: "通用模型", mark: "AI" },
    openai: { key: "openai", label: "OpenAI", mark: "O", asset: "/icons/openai.svg" },
    google: { key: "google", label: "Google", mark: "G", asset: "/icons/gemini.svg" },
    anthropic: { key: "anthropic", label: "Anthropic", mark: "A", asset: "/icons/claude.svg" },
    deepseek: { key: "deepseek", label: "DeepSeek", mark: "D", asset: "/icons/deepseek.svg" },
    xai: { key: "xai", label: "xAI", mark: "X", asset: "/icons/grok.svg" },
    zhipu: { key: "zhipu", label: "智谱 AI", mark: "Z", asset: "/icons/glm.svg" },
    minimax: { key: "minimax", label: "MiniMax", mark: "M", asset: "/icons/minimax.svg" },
    seedance: { key: "seedance", label: "Seedance", mark: "S", asset: "/icons/seedance.svg" },
    kling: { key: "kling", label: "可灵", mark: "K", asset: "/icons/kling.svg" },
    qwen: { key: "qwen", label: "通义千问", mark: "Q", asset: "/icons/qwen.svg" },
    alibaba: { key: "alibaba", label: "阿里云", mark: "A", asset: "/icons/alibaba.svg" },
    volcengine: { key: "volcengine", label: "火山引擎", mark: "V", asset: "/icons/volcengine.svg" },
    runway: { key: "runway", label: "Runway", mark: "R", asset: "/icons/runway.svg" },
    luma: { key: "luma", label: "Luma", mark: "L", asset: "/icons/luma.svg" },
    pika: { key: "pika", label: "Pika", mark: "P" },
    flux: { key: "flux", label: "FLUX", mark: "F" },
    stability: { key: "stability", label: "Stability AI", mark: "S", asset: "/icons/stability.svg" },
    ideogram: { key: "ideogram", label: "Ideogram", mark: "I", asset: "/icons/ideogram.svg" },
    recraft: { key: "recraft", label: "Recraft", mark: "R", asset: "/icons/recraft.svg" },
};

export const modelBrandOptions = MODEL_BRAND_KEYS.map((key) => definitions[key]);

export function modelBrandDefinition(key: ModelBrandKey): ModelBrandDefinition {
    return definitions[key];
}
