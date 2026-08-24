import type { ChannelModel } from "@/services/api/wallet";

export type PricingSpecification = {
    key: string;
    label: string;
    group: "base" | "supplier-only";
    resolution?: string;
    inputVariant?: "standard" | "standard_audio" | "reference_video";
    unit?: "秒" | "张" | "万字符";
    note?: string;
};

export const imagePricingSpecifications: PricingSpecification[] = [
    { key: "1K", label: "1K", group: "base" },
    { key: "2K", label: "2K", group: "base" },
    { key: "4K", label: "4K", group: "base" },
];

export const videoPricingSpecifications: PricingSpecification[] = [
    { key: "480P", label: "480P", group: "base" },
    { key: "720P", label: "720P", group: "base" },
    { key: "768P", label: "768P", group: "base" },
    { key: "1080P", label: "1080P", group: "base" },
    { key: "2K", label: "2K", group: "base" },
    { key: "4K", label: "4K", group: "base" },
];

export const miniMaxH3SupplierSpecifications: PricingSpecification[] = [
    { key: "INPUT_IMAGE_OVERAGE", label: "输入图片（超过 5 张）", group: "supplier-only", unit: "张", note: "前 5 张免费，仅对超出部分计费" },
    { key: "INPUT_VIDEO_768P", label: "输入视频 · 生成 768P", group: "supplier-only", unit: "秒", note: "按输入视频时长计费" },
    { key: "INPUT_VIDEO_2K", label: "输入视频 · 生成 2K", group: "supplier-only", unit: "秒", note: "按输入视频时长计费" },
    { key: "REGENERATE_768P_TO_2K", label: "视频再生 768P → 2K", group: "supplier-only", unit: "秒", note: "按再生视频输出时长计费" },
    { key: "REGENERATE_INPUT_IMAGE_OVERAGE", label: "视频再生输入图片（超过 5 张）", group: "supplier-only", unit: "张", note: "前 5 张免费，仅对超出部分计费" },
    { key: "REGENERATE_INPUT_VIDEO_768P", label: "视频再生输入视频 · 768P", group: "supplier-only", unit: "秒", note: "按输入视频时长计费" },
];

export const miniMaxSpeechSupplierSpecifications: PricingSpecification[] = [{ key: "TEN_THOUSAND_CHARACTERS", label: "语音合成字符", group: "supplier-only", unit: "万字符", note: "同步与异步长文本使用相同供应商单价" }];

export function specificationsForStrategy(strategy: ChannelModel["priceStrategy"]): PricingSpecification[] {
    if (strategy === "image_resolution") return imagePricingSpecifications;
    if (strategy === "video_resolution") return videoPricingSpecifications;
    return [];
}

export function normalizedPricingTierKey(resolution: string, inputVariant = "standard") {
    return `${resolution.trim().toLowerCase()}::${inputVariant.trim().toLowerCase()}`;
}

export function specificationsForModel(model: Pick<ChannelModel, "modelKey" | "priceStrategy" | "providerCapabilities">): PricingSpecification[] {
    const base = specificationsForStrategy(model.priceStrategy);
    const modelKey = model.modelKey.trim().toLowerCase();
    const capabilities = model.providerCapabilities;
    if (model.priceStrategy === "video_resolution" && capabilities && capabilities.inputVariants.length > 0) {
        return capabilities.resolutions.flatMap((resolution) =>
            capabilities.inputVariants
                .filter((inputVariant) => {
                    if (inputVariant === "standard") return true;
                    if (inputVariant === "standard_audio") {
                        return capabilities.generatedAudioResolutions.length === 0 || capabilities.generatedAudioResolutions.includes(resolution);
                    }
                    return capabilities.referenceVideoResolutions.includes(resolution);
                })
                .map((inputVariant) => ({
                    key: `${resolution}::${inputVariant}`,
                    label: `${resolution} · ${inputVariant === "reference_video" ? "参考视频" : inputVariant === "standard_audio" ? "普通生成（有声）" : "普通生成（无声）"}`,
                    group: "base" as const,
                    resolution,
                    inputVariant,
                })),
        );
    }
    if (modelKey === "minimax-h3") return [...base, ...miniMaxH3SupplierSpecifications];
    if (modelKey === "speech-2.8-hd" || modelKey === "speech-2.8-turbo") return [...base, ...miniMaxSpeechSupplierSpecifications];
    return base;
}
