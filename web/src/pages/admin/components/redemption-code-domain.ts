import type { AdminRedeemCode, RedeemBatch } from "@/services/api/wallet";

export type RedeemFormValues = {
    amount: number;
    count: number;
    note?: string;
    expiresAt?: string;
};

export function redeemBatchRequest(values: RedeemFormValues) {
    const note = values.note?.trim();
    return {
        amountMicrocredits: Math.round(values.amount * 1_000_000),
        count: values.count,
        note: note || undefined,
        expiresAt: values.expiresAt ? new Date(values.expiresAt).toISOString() : undefined,
    };
}

export function redeemBatchDisableDescription(batch: RedeemBatch) {
    return `将禁用该批次当前 ${batch.availableCount} 个可用兑换码；已核销、已过期和已禁用记录不会变更。`;
}

export function redeemCodeDisableDescription(code: Pick<AdminRedeemCode, "codeSuffix">) {
    return `兑换码尾号 ····${code.codeSuffix} 将立即失效，且无法恢复。`;
}
