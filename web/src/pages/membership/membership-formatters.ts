import type { MembershipBillingCycle, MembershipPlan } from "@/services/api/membership";

const moneyFormatter = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 2,
    minimumFractionDigits: 0,
});

const creditFormatter = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 0,
});

export const billingCycleLabel: Record<MembershipBillingCycle, string> = {
    free: "永久免费",
    month: "按月购买",
    year: "按年购买",
};

export const billingCycleShortLabel: Record<MembershipBillingCycle, string> = {
    free: "免费",
    month: "月付",
    year: "年付",
};

const defaultPlanNames: Record<string, string> = {
    origin: "基础版",
    pro: "标准版",
    max: "高级版",
    ultra: "至尊版",
};

const legacyDefaultNames = new Set(["Origin", "Pro", "Max", "Ultra", "团队 Pro", "团队 Max", "团队 Ultra"]);

export function publicPlanName(plan: Pick<MembershipPlan, "name" | "tier">): string {
    if (!legacyDefaultNames.has(plan.name)) return plan.name;
    return defaultPlanNames[plan.tier] ?? plan.name;
}

export function formatMoney(valueCents: number): string {
    return moneyFormatter.format(valueCents / 100);
}

export function formatCredits(valueMicrocredits: number): string {
    return creditFormatter.format(valueMicrocredits / 1_000_000);
}

export function cycleMonthCount(cycle: MembershipBillingCycle): number {
    return cycle === "year" ? 12 : 1;
}

export function monthlyPriceCents(plan: MembershipPlan): number {
    return plan.priceCents / cycleMonthCount(plan.billingCycle);
}

export function monthlyCredits(plan: MembershipPlan): number {
    return plan.creditsPerPeriod / cycleMonthCount(plan.billingCycle);
}

export function discountLabel(plan: MembershipPlan): string | null {
    if (plan.priceCents <= 0 || plan.originalPriceCents <= plan.priceCents) return null;
    const discount = Math.round((plan.priceCents / plan.originalPriceCents) * 100) / 10;
    const formatted = Number.isInteger(discount) ? discount.toFixed(0) : discount.toFixed(1);
    return `限时 ${formatted} 折`;
}

export function topupDiscountLabel(basisPoints: number): string {
    if (basisPoints <= 0 || basisPoints >= 10_000) return "无折扣";
    const discount = basisPoints / 1_000;
    return `${Number.isInteger(discount) ? discount.toFixed(0) : discount.toFixed(1)} 折`;
}

export function clampSeats(plan: MembershipPlan, seats: number): number {
    if (plan.audience !== "team") return 1;
    return Math.min(plan.maxSeats, Math.max(plan.minSeats, seats));
}

export function planTotalPriceCents(plan: MembershipPlan, seats: number): number {
    return plan.priceCents * clampSeats(plan, seats);
}

export function planTotalCredits(plan: MembershipPlan, seats: number): number {
    return plan.creditsPerPeriod * clampSeats(plan, seats);
}
