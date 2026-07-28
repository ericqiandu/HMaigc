import type { ChannelModel } from "@/services/api/wallet";

export type PricingSpecification = {
    key: string;
    label: string;
    group: "base" | "super_resolution";
};

export const imagePricingSpecifications: PricingSpecification[] = [
    { key: "1K", label: "1K", group: "base" },
    { key: "2K", label: "2K", group: "base" },
    { key: "4K", label: "4K", group: "base" },
];

export const videoPricingSpecifications: PricingSpecification[] = [
    { key: "480P", label: "480P", group: "base" },
    { key: "720P", label: "720P", group: "base" },
    { key: "1080P", label: "1080P", group: "base" },
    { key: "2K", label: "2K", group: "base" },
    { key: "4K", label: "4K", group: "base" },
    { key: "SR_720P", label: "超分 720P", group: "super_resolution" },
    { key: "SR_1080P", label: "超分 1080P", group: "super_resolution" },
    { key: "SR_2K", label: "超分 2K", group: "super_resolution" },
    { key: "SR_4K", label: "超分 4K", group: "super_resolution" },
];

export function specificationsForStrategy(
    strategy: ChannelModel["priceStrategy"],
): PricingSpecification[] {
    if (strategy === "image_resolution") return imagePricingSpecifications;
    if (strategy === "video_resolution") return videoPricingSpecifications;
    return [];
}
