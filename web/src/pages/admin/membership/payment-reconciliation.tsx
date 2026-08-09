import { Button } from "antd";

import type { AdminPaymentReconciliationResult, PaymentProviderState, PaymentTransaction, PaymentWebhookEvent } from "@/services/api/payment";

type PaymentTransactionReconciliationActionProps = {
    transaction: PaymentTransaction;
    loading: boolean;
    onRequest: (transaction: PaymentTransaction) => void;
};

type PaymentReconciliationExecutionOptions = {
    transaction: PaymentTransaction;
    inFlightIds: Set<string>;
    reconcile: (transactionId: string) => Promise<AdminPaymentReconciliationResult>;
    replaceTransaction: (transaction: PaymentTransaction) => void;
    refreshTransactions: () => Promise<string | null>;
    refreshWebhooks: () => Promise<string | null>;
    notifySuccess: (result: AdminPaymentReconciliationResult) => void;
    notifyError: (description: string) => void;
    notifyRefreshError: (description: string) => void;
    setBusy: (transactionId: string, busy: boolean) => void;
};

const errorDescription = (error: unknown) => (error instanceof Error && error.message ? error.message : "未知错误");

const formatMoney = (amountCents: number, currency: string) => new Intl.NumberFormat("zh-CN", { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(amountCents / 100);

async function refreshPaymentFacts({ refreshTransactions, refreshWebhooks, notifyRefreshError }: Pick<PaymentReconciliationExecutionOptions, "refreshTransactions" | "refreshWebhooks" | "notifyRefreshError">) {
    const capture = async (label: string, refresh: () => Promise<string | null>) => {
        try {
            const description = await refresh();
            return description ? `${label}：${description}` : null;
        } catch (error) {
            return `${label}：${errorDescription(error)}`;
        }
    };
    const failures = (await Promise.all([capture("支付交易", refreshTransactions), capture("回调审计", refreshWebhooks)])).filter((failure): failure is string => Boolean(failure));
    if (failures.length > 0) notifyRefreshError(failures.join("；"));
}

export function PaymentTransactionReconciliationAction({ transaction, loading, onRequest }: PaymentTransactionReconciliationActionProps) {
    if (transaction.status !== "review_required") return null;
    return (
        <Button className="admin-payment-reconciliation-button" type="text" size="small" loading={loading} aria-label={`对账交易 ${transaction.merchantOrderNo}`} onClick={() => onRequest(transaction)}>
            渠道对账
        </Button>
    );
}

export function PaymentReconciliationConfirmation({ transaction }: { transaction: PaymentTransaction }) {
    const provider = transaction.provider === "wechat" ? "微信支付" : "支付宝";
    return (
        <div className="admin-payment-reconciliation-confirmation">
            <p className="admin-payment-reconciliation-facts">
                <strong className="admin-payment-reconciliation-order">{transaction.merchantOrderNo}</strong> · {provider} · {formatMoney(transaction.amountCents, transaction.currency)}（{transaction.currency}）
            </p>
            <p className="admin-payment-reconciliation-impact">渠道确认到账才会履约；确认未支付且远端关单成功才会关闭；结果不确定仍保持待对账。</p>
        </div>
    );
}

export function paymentReconciliationOutcomeLabel(state: PaymentProviderState) {
    const labels: Record<PaymentProviderState, string> = {
        paid: "渠道已确认到账并完成履约",
        unpaid: "渠道已确认未到账并完成关单",
        not_found: "渠道未找到付款并完成关单",
        unknown: "渠道状态仍不确定，交易保持待对账",
    };
    return labels[state];
}

export function paymentStatusLabel(status: PaymentTransaction["status"]) {
    const labels: Record<PaymentTransaction["status"], string> = {
        created: "已创建",
        pending: "待支付",
        review_required: "待对账",
        paid: "已支付",
        closed: "已关闭",
        failed: "失败",
        refunded: "已退款",
    };
    return labels[status];
}

export function webhookStatusLabel(status: PaymentWebhookEvent["status"]) {
    const labels: Record<PaymentWebhookEvent["status"], string> = {
        received: "已接收",
        processed: "已处理",
        rejected: "已拒绝",
        review_required: "待复核",
    };
    return labels[status];
}

export async function executePaymentReconciliation({
    transaction,
    inFlightIds,
    reconcile,
    replaceTransaction,
    refreshTransactions,
    refreshWebhooks,
    notifySuccess,
    notifyError,
    notifyRefreshError,
    setBusy,
}: PaymentReconciliationExecutionOptions): Promise<boolean> {
    if (inFlightIds.has(transaction.id)) return false;
    inFlightIds.add(transaction.id);
    setBusy(transaction.id, true);
    try {
        let result: AdminPaymentReconciliationResult;
        try {
            result = await reconcile(transaction.id);
        } catch (error) {
            notifyError(errorDescription(error));
            await refreshPaymentFacts({ refreshTransactions, refreshWebhooks, notifyRefreshError });
            return false;
        }
        replaceTransaction(result.transaction);
        notifySuccess(result);
        await refreshPaymentFacts({ refreshTransactions, refreshWebhooks, notifyRefreshError });
        return true;
    } finally {
        inFlightIds.delete(transaction.id);
        setBusy(transaction.id, false);
    }
}
