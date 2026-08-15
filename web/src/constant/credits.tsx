import type { ComponentProps } from "react";
import { Coins } from "lucide-react";

export function CreditSymbol({ className, ...props }: ComponentProps<"span">) {
    return (
        <span {...props} className={`inline-flex items-center justify-center ${className || ""}`}>
            <Coins className="size-[1em]" strokeWidth={2.2} />
        </span>
    );
}

export type ModelCreditCost = {
    model: string;
    billingMode: "fixed_request" | "per_second" | "token_usage";
    priceStrategy: "flat" | "image_resolution" | "video_resolution" | "token";
    unitPriceMicrocredits: number;
    priceTiers: Array<{
        resolution: string;
        inputVariant?: "standard" | "reference_video";
        unitPriceMicrocredits: number;
    }>;
};

function modelCreditCost(modelCosts: ModelCreditCost[] | undefined, model: string) {
    return modelCosts?.find((item) => item.model === model) || null;
}

export function formatCredits(value: number, maximumFractionDigits = 6) {
    return (value / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits });
}

function imagePricingResolution(value: string | undefined): "1K" | "2K" | "4K" | null {
    switch (value?.trim().toLowerCase()) {
        case "low":
        case "1k":
            return "1K";
        case "medium":
        case "2k":
            return "2K";
        case "high":
        case "4k":
            return "4K";
        default:
            return null;
    }
}

function videoPricingResolution(resolution: string | undefined) {
    const value = resolution?.trim().toLowerCase();
    const normalized =
        value === "480" || value === "480p"
            ? "480P"
            : value === "768" || value === "768p"
              ? "768P"
              : value === "720" || value === "720p"
                ? "720P"
                : value === "1080" || value === "1080p"
                  ? "1080P"
                  : value === "2k"
                    ? "2K"
                    : value === "4k"
                      ? "4K"
                      : value === "8k"
                        ? "8K"
                        : null;
    return normalized;
}

export function requestCreditCost(options: { channelMode: string; modelCosts?: ModelCreditCost[]; model: string; count?: string | number; seconds?: string | number; quality?: string; resolution?: string; referenceVideoCount?: number }) {
    if (options.channelMode !== "remote") return null;
    const cost = modelCreditCost(options.modelCosts, options.model);
    if (!cost) return null;
    if (cost.priceStrategy !== "flat" && !Array.isArray(cost.priceTiers)) return null;
    const pricingResolution = cost.priceStrategy === "image_resolution" ? imagePricingResolution(options.resolution || options.quality) : videoPricingResolution(options.resolution);
    const inputVariant = Number(options.referenceVideoCount) > 0 ? "reference_video" : "standard";
    const unitPriceMicrocredits = cost.priceStrategy === "flat" ? cost.unitPriceMicrocredits : cost.priceTiers.find((tier) => tier.resolution === pricingResolution && (tier.inputVariant || "standard") === inputVariant)?.unitPriceMicrocredits;
    if (!Number.isFinite(unitPriceMicrocredits) || Number(unitPriceMicrocredits) <= 0) return null;
    const count = Math.max(1, Math.floor(Math.abs(Number(options.count)) || 1));
    const quantity = cost.billingMode === "per_second" ? Math.max(1, Math.floor(Math.abs(Number(options.seconds)) || 1)) * count : count;
    return (Number(unitPriceMicrocredits) / 1_000_000) * quantity;
}
