import type { ConfirmedTaskBillingQuote } from "@/lib/billing/task-billing-quote";
import type { TaskBillingQuote } from "@/services/api/task-center";

export type PaidCommandResult<T> = { status: "executed"; value: T } | { status: "rejected" };

export type PaidCommandRequest<T> = {
    commandKey: string;
    loadQuote: () => Promise<TaskBillingQuote>;
    confirm: (quote: TaskBillingQuote) => Promise<boolean>;
    execute: (confirmedQuote: ConfirmedTaskBillingQuote) => Promise<T>;
};

export function createPaidCommandGate() {
    const running = new Set<string>();
    return {
        async run<T>({ commandKey, loadQuote, confirm, execute }: PaidCommandRequest<T>): Promise<PaidCommandResult<T>> {
            if (running.has(commandKey)) throw new Error("同一付费操作正在执行");
            running.add(commandKey);
            try {
                const quote = await loadQuote();
                if (!(await confirm(quote))) return { status: "rejected" };
                const value = await execute({ priceVersion: quote.priceVersion, quoteFingerprint: quote.quoteFingerprint });
                return { status: "executed", value };
            } finally {
                running.delete(commandKey);
            }
        },
    };
}
