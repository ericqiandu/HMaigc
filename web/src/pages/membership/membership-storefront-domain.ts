import type { MembershipPlan } from "@/services/api/membership";

import { cycleMonthCount, formatMoney, monthlyCredits, monthlyPriceCents } from "./membership-formatters";

export type CountdownPart = { key: "days" | "hours" | "minutes" | "seconds"; label: string; value: string };

export function countdownParts(endsAt: string, serverNow: string, clientStartedAtMilliseconds: number, clientNowMilliseconds = Date.now()): CountdownPart[] {
    const deadline = Date.parse(endsAt);
    const serverTimestamp = Date.parse(serverNow);
    const elapsed = Math.max(0, clientNowMilliseconds - clientStartedAtMilliseconds);
    const remainingSeconds = Number.isFinite(deadline) && Number.isFinite(serverTimestamp)
        ? Math.max(0, Math.floor((deadline - serverTimestamp - elapsed) / 1000))
        : 0;
    const days = Math.floor(remainingSeconds / 86_400);
    const hours = Math.floor((remainingSeconds % 86_400) / 3_600);
    const minutes = Math.floor((remainingSeconds % 3_600) / 60);
    const seconds = remainingSeconds % 60;
    return [
        { key: "days", label: "天", value: String(days).padStart(2, "0") },
        { key: "hours", label: "时", value: String(hours).padStart(2, "0") },
        { key: "minutes", label: "分", value: String(minutes).padStart(2, "0") },
        { key: "seconds", label: "秒", value: String(seconds).padStart(2, "0") },
    ];
}

export function planCreditValueLabel(plan: MembershipPlan, seats: number): string {
    const credits = monthlyCredits(plan) * seats;
    if (credits <= 0) return "免费额度";
    const yuanPerCredit = monthlyPriceCents(plan) * seats / 100 / (credits / 1_000_000);
    return `1 积分≈${yuanPerCredit.toLocaleString("zh-CN", { maximumFractionDigits: 3 })} 元`;
}

export function planPeriodDescription(plan: MembershipPlan, seats: number): string {
    const total = plan.priceCents * seats;
    if (total <= 0) return "无需付款，登录后即可使用";
    const period = plan.billingCycle === "year" ? "年" : "月";
    return `本期 ${period}付 ¥${formatMoney(total)}，支付成功后生效`;
}

export function planCycleSavingsLabel(plan: MembershipPlan, allPlans: MembershipPlan[]): string {
    if (plan.billingCycle === "year") {
        const original = plan.originalPriceCents;
        if (original > plan.priceCents && original > 0) {
            const savedPercent = Math.round((1 - plan.priceCents / original) * 100);
            return `当前年付立省 ${savedPercent}%`;
        }
        return "按年配置会员权益";
    }
    const annual = allPlans.find((candidate) => candidate.audience === plan.audience && candidate.tier === plan.tier && candidate.billingCycle === "year");
    const monthlyAnnualized = plan.priceCents * 12;
    if (annual && monthlyAnnualized > annual.priceCents) {
        const savedPercent = Math.round((1 - annual.priceCents / monthlyAnnualized) * 100);
        return `买年卡立省 ${savedPercent}%`;
    }
    return "可按需切换计费周期";
}

export function planPriceLabel(plan: MembershipPlan, seats: number): string {
    return formatMoney(monthlyPriceCents(plan) * seats);
}

export function planOriginalMonthlyPriceLabel(plan: MembershipPlan, seats: number): string | null {
    if (plan.originalPriceCents <= plan.priceCents) return null;
    return formatMoney((plan.originalPriceCents / cycleMonthCount(plan.billingCycle)) * seats);
}

export function planStorageLabel(plan: MembershipPlan): string {
    if (plan.teamStorageBytes <= 0) return "个人项目与素材空间";
    const gibibytes = plan.teamStorageBytes / (1 << 30);
    if (gibibytes >= 1024) return `云端存储空间 ${(gibibytes / 1024).toLocaleString("zh-CN", { maximumFractionDigits: 1 })} TB`;
    return `云端存储空间 ${gibibytes.toLocaleString("zh-CN", { maximumFractionDigits: 1 })} GB`;
}
