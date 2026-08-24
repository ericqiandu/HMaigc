import type { ModelPricing, ModelPricingInput } from "@/services/api/auth";
import type { ChannelModel, ChannelModelInput } from "@/services/api/wallet";

export const INPUT_IMAGE_USAGE_METRIC = "input_image";

export type InputImageUsagePricingValues = {
    includedQuantity: number | undefined;
    supplierUnitCostMicros: number | undefined;
    userUnitPriceMicrocredits: number | undefined;
};

export type InputImageUsagePricingReadResult = InputImageUsagePricingValues & {
    state: "disabled" | "complete" | "incomplete";
};

type SupplierTierInput = ModelPricingInput["tiers"][number];
type UserTierInput = ChannelModelInput["priceTiers"][number];

export type InputImageUsagePricing = {
    supplierTier: SupplierTierInput;
    userTier: UserTierInput;
};

export function buildInputImageUsagePricing(values: InputImageUsagePricingValues): InputImageUsagePricing | null {
    const fields = [values.includedQuantity, values.supplierUnitCostMicros, values.userUnitPriceMicrocredits];
    if (fields.every((value) => value === undefined)) return null;
    if (fields.some((value) => value === undefined)) {
        throw new Error("参考图附加价必须同时配置免费数量、供应商成本和用户积分");
    }
    if (!Number.isSafeInteger(values.includedQuantity) || (values.includedQuantity as number) < 0) {
        throw new Error("免费参考图数量必须是不小于 0 的整数");
    }
    if (!Number.isSafeInteger(values.supplierUnitCostMicros) || (values.supplierUnitCostMicros as number) <= 0) {
        throw new Error("参考图供应商成本必须大于 0");
    }
    if (!Number.isSafeInteger(values.userUnitPriceMicrocredits) || (values.userUnitPriceMicrocredits as number) <= 0) {
        throw new Error("参考图用户积分必须大于 0");
    }

    const includedQuantity = values.includedQuantity as number;
    return {
        supplierTier: {
            specification: INPUT_IMAGE_USAGE_METRIC,
            usageMetric: INPUT_IMAGE_USAGE_METRIC,
            includedQuantity,
            supplierCostMicros: values.supplierUnitCostMicros as number,
        },
        userTier: {
            resolution: "",
            inputVariant: "",
            usageMetric: INPUT_IMAGE_USAGE_METRIC,
            includedQuantity,
            unitPriceMicrocredits: values.userUnitPriceMicrocredits as number,
        },
    };
}

export function readInputImageUsagePricing(supplierTiers: ModelPricing["tiers"], userTiers: ChannelModel["priceTiers"]): InputImageUsagePricingReadResult {
    const supplierTier = supplierTiers.find((tier) => tier.usageMetric === INPUT_IMAGE_USAGE_METRIC);
    const userTier = userTiers.find((tier) => tier.usageMetric === INPUT_IMAGE_USAGE_METRIC);
    if (!supplierTier && !userTier) {
        return { state: "disabled", includedQuantity: undefined, supplierUnitCostMicros: undefined, userUnitPriceMicrocredits: undefined };
    }
    const includedQuantity = supplierTier?.includedQuantity ?? userTier?.includedQuantity;
    const complete = Boolean(supplierTier && userTier && supplierTier.includedQuantity === userTier.includedQuantity);
    return {
        state: complete ? "complete" : "incomplete",
        includedQuantity,
        supplierUnitCostMicros: supplierTier?.supplierCostMicros,
        userUnitPriceMicrocredits: userTier?.unitPriceMicrocredits,
    };
}
