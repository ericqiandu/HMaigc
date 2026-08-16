import { CreditSymbol, formatCredits } from "@/constant/credits";
import type { TaskBillingQuoteState } from "@/hooks/use-task-billing-quote";

export function GenerationCreditQuoteBadge({ state, className = "", color }: { state: TaskBillingQuoteState; className?: string; color?: string }) {
    if (state.status === "idle") return null;
    if (state.status === "loading") {
        return (
            <span className={`inline-flex h-6 shrink-0 items-center gap-0.5 px-1 text-[11px] font-medium ${className}`} style={{ color }} title="正在获取预计积分">
                <CreditSymbol aria-hidden="true" />
                计算中
            </span>
        );
    }
    if (state.status === "error") {
        return (
            <span className={`inline-flex h-6 shrink-0 items-center gap-0.5 px-1 text-[11px] font-medium ${className}`} style={{ color }} title={state.error}>
                <CreditSymbol aria-hidden="true" />
                报价失败
            </span>
        );
    }
    return (
        <span className={`inline-flex h-6 shrink-0 items-center gap-0.5 px-1 text-[11px] font-medium leading-4 tabular-nums ${className}`} style={{ color }} title="预计消耗积分，最终以实际结算为准">
            <CreditSymbol aria-hidden="true" />
            {formatCredits(state.quote.amountMicrocredits)}
        </span>
    );
}
