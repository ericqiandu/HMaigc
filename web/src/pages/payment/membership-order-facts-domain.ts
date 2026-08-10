import { billingCycleLabel, planTotalCredits, planTotalPriceCents, publicPlanName } from "@/pages/membership/membership-formatters";
import type { MembershipOrder, MembershipPlan } from "@/services/api/membership";
import type { PaymentCheckout } from "@/services/api/payment";

import { checkoutSummary } from "./payment-checkout-domain";
import { validateMembershipOrderFacts } from "./membership-order-validation";

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

export type MembershipOrderLifecycle = { kind: "preorder" } | { facts: MembershipOrderFactsModel; kind: "frozen-ready"; orderId: string } | { error: string; kind: "frozen-invalid" };

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

function readSnapshotRecord(value: unknown): Record<string, unknown> {
    if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("订单冻结套餐快照格式无效");
    return value as Record<string, unknown>;
}

function readSnapshotString(snapshot: Record<string, unknown>, key: string): string {
    const value = snapshot[key];
    if (typeof value !== "string" || value.trim().length === 0) throw new Error(`订单冻结套餐缺少 ${key}`);
    return value;
}

function readSnapshotInteger(snapshot: Record<string, unknown>, key: string): number {
    const value = snapshot[key];
    if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) throw new Error(`订单冻结套餐 ${key} 无效`);
    return value;
}

type MembershipPlanSnapshot = Pick<MembershipPlan, "code" | "creditsPerPeriod" | "currency" | "id" | "maxSeats" | "minSeats" | "name" | "originalPriceCents" | "priceCents" | "tier"> & {
    audience: MembershipOrderFactsModel["audience"];
    billingCycle: MembershipOrderFactsModel["billingCycle"];
};

function membershipPlanSnapshotFromOrder(order: MembershipOrder): MembershipPlanSnapshot {
    if (order.planSnapshotJson.trim().length === 0) throw new Error("订单缺少冻结套餐快照");
    let parsed: unknown;
    try {
        parsed = JSON.parse(order.planSnapshotJson);
    } catch {
        throw new Error("订单冻结套餐快照不是有效 JSON");
    }
    const snapshot = readSnapshotRecord(parsed);
    const audience = readSnapshotString(snapshot, "audience");
    const billingCycle = readSnapshotString(snapshot, "billingCycle");
    const planId = readSnapshotString(snapshot, "id");
    if ((audience !== "personal" && audience !== "team") || (billingCycle !== "month" && billingCycle !== "year")) {
        throw new Error("订单冻结套餐的会员类型或计费周期无效");
    }
    if (planId !== order.planId) throw new Error("订单冻结套餐与订单套餐不一致");
    const currency = readSnapshotString(snapshot, "currency");
    if (currency !== order.currency) throw new Error("订单冻结套餐币种与订单不一致");
    return {
        audience,
        billingCycle,
        code: readSnapshotString(snapshot, "code"),
        creditsPerPeriod: readSnapshotInteger(snapshot, "creditsPerPeriod"),
        currency,
        id: planId,
        maxSeats: readSnapshotInteger(snapshot, "maxSeats"),
        minSeats: readSnapshotInteger(snapshot, "minSeats"),
        name: readSnapshotString(snapshot, "name"),
        originalPriceCents: readSnapshotInteger(snapshot, "originalPriceCents"),
        priceCents: readSnapshotInteger(snapshot, "priceCents"),
        tier: readSnapshotString(snapshot, "tier"),
    };
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

export function membershipOrderFactsFromOrder(order: MembershipOrder): MembershipOrderFactsModel {
    const snapshot = membershipPlanSnapshotFromOrder(order);
    if (snapshot.audience === "personal" && order.teamId?.trim()) throw new Error("个人会员订单不能绑定团队");
    if (snapshot.audience === "team" && !order.teamId?.trim()) throw new Error("团队会员订单缺少团队身份");
    if (snapshot.priceCents !== order.unitPriceCents) throw new Error("订单冻结金额与套餐快照不一致");
    const validated = validateMembershipOrderFacts({
        audience: snapshot.audience,
        billingCycle: snapshot.billingCycle,
        code: snapshot.code,
        creditsPerPeriod: snapshot.creditsPerPeriod,
        currency: order.currency,
        name: snapshot.name,
        orderNumber: order.orderNumber,
        originalPriceCents: checkedProduct(snapshot.originalPriceCents, order.seats, "订单冻结原价"),
        originalUnitPriceCents: snapshot.originalPriceCents,
        orderId: order.id,
        seatBounds: { maxSeats: snapshot.maxSeats, minSeats: snapshot.minSeats },
        seats: order.seats,
        source: "frozen-order",
        tier: snapshot.tier,
        totalCredits: checkedProduct(snapshot.creditsPerPeriod, order.seats, "订单冻结积分"),
        totalPriceCents: order.totalPriceCents,
        unitPriceCents: order.unitPriceCents,
    });
    return {
        audience: validated.audience,
        billingCycle: validated.billingCycle,
        creditsPerPeriod: validated.creditsPerPeriod,
        currency: validated.currency,
        orderNumber: validated.orderNumber,
        originalTotalPriceCents: validated.originalPriceCents,
        originalUnitPriceCents: validated.originalUnitPriceCents,
        seats: validated.seats,
        title: snapshot.name,
        totalCredits: validated.totalCredits,
        totalPriceCents: validated.totalPriceCents,
        unitPriceCents: validated.unitPriceCents,
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
