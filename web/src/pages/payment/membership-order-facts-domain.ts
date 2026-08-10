import { billingCycleLabel, planTotalCredits, planTotalPriceCents, publicPlanName } from "@/pages/membership/membership-formatters";
import type { MembershipPlan } from "@/services/api/membership";
import type { PaymentCheckout } from "@/services/api/payment";

import { checkoutSummary } from "./payment-checkout-domain";

export type MembershipOrderFactsModel = {
    audience: "personal" | "team";
    billingCycle: "month" | "year";
    creditsPerPeriod: number;
    currency: string;
    orderNumber: string;
    originalTotalPriceCents: number;
    originalUnitPriceCents: number;
    seats: number;
    title: string;
    totalCredits: number;
    totalPriceCents: number;
    unitPriceCents: number;
};

function checkedProduct(left: number, right: number, field: string): number {
    if (!Number.isSafeInteger(left) || !Number.isSafeInteger(right)) throw new Error(`${field}必须使用安全整数计算`);
    const result = left * right;
    if (!Number.isSafeInteger(result)) throw new Error(`${field}超出安全整数范围`);
    return result;
}

function checkedPlanTotal(plan: MembershipPlan, seats: number): Pick<MembershipOrderFactsModel, "totalCredits" | "totalPriceCents"> {
    const totalCredits = checkedProduct(plan.creditsPerPeriod, seats, "会员积分合计");
    const totalPriceCents = checkedProduct(plan.priceCents, seats, "会员总价");
    if (planTotalCredits(plan, seats) !== totalCredits || planTotalPriceCents(plan, seats) !== totalPriceCents) {
        throw new Error("会员套餐总额与席位计算不一致");
    }
    return { totalCredits, totalPriceCents };
}

export function membershipOrderFactsFromPlan(plan: MembershipPlan, seats: number): MembershipOrderFactsModel {
    if (plan.billingCycle !== "month" && plan.billingCycle !== "year") throw new Error("会员套餐计费周期无效");
    const normalizedSeats = plan.audience === "team" ? Math.min(plan.maxSeats, Math.max(plan.minSeats, seats)) : 1;
    const totals = checkedPlanTotal(plan, normalizedSeats);
    const originalTotalPriceCents = checkedProduct(plan.originalPriceCents, normalizedSeats, "会员原价合计");

    return {
        audience: plan.audience,
        billingCycle: plan.billingCycle,
        creditsPerPeriod: plan.creditsPerPeriod,
        currency: plan.currency,
        orderNumber: "",
        originalTotalPriceCents,
        originalUnitPriceCents: plan.originalPriceCents,
        seats: normalizedSeats,
        title: publicPlanName(plan),
        totalCredits: totals.totalCredits,
        totalPriceCents: totals.totalPriceCents,
        unitPriceCents: plan.priceCents,
    };
}

export function membershipOrderFactsFromCheckout(checkout: PaymentCheckout): MembershipOrderFactsModel {
    const summary = checkoutSummary(checkout);
    if (summary.kind !== "membership") throw new Error("积分充值订单不能映射为会员订单事实");

    return {
        audience: summary.audience,
        billingCycle: summary.billingCycle,
        creditsPerPeriod: summary.creditsPerPeriod,
        currency: checkout.currency,
        orderNumber: checkout.orderNumber,
        originalTotalPriceCents: summary.originalPriceCents,
        originalUnitPriceCents: summary.originalUnitPriceCents,
        seats: summary.seats,
        title: summary.title,
        totalCredits: summary.totalCredits,
        totalPriceCents: summary.actualPriceCents,
        unitPriceCents: summary.unitPriceCents,
    };
}

export function membershipBillingCycleLabel(facts: MembershipOrderFactsModel): string {
    return billingCycleLabel[facts.billingCycle];
}
