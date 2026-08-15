import { useEffect, useRef, useState } from "react";

import { requestTaskBillingQuote, type TaskBillingQuote, type TaskBillingQuoteRequest } from "@/services/api/task-center";
import { invalidateTaskBillingQuotes, subscribeTaskBillingQuoteInvalidation } from "@/lib/billing/task-billing-quote-events";

const billingQuoteDebounceMilliseconds = 250;
export { invalidateTaskBillingQuotes } from "@/lib/billing/task-billing-quote-events";

export type TaskBillingQuoteState = { status: "idle"; quote: null; error: null } | { status: "loading"; quote: null; error: null } | { status: "ready"; quote: TaskBillingQuote; error: null } | { status: "error"; quote: null; error: string };

type TaskBillingQuoteLoader = (request: TaskBillingQuoteRequest, signal: AbortSignal) => Promise<TaskBillingQuote>;

const initialTaskBillingQuoteState: TaskBillingQuoteState = { status: "idle", quote: null, error: null };

export function useTaskBillingQuote(request: TaskBillingQuoteRequest | null, load: TaskBillingQuoteLoader = requestTaskBillingQuote): TaskBillingQuoteState {
    const [state, setState] = useState<TaskBillingQuoteState>(initialTaskBillingQuoteState);
    const [revision, setRevision] = useState(0);
    const requestSequence = useRef(0);
    const quoteFingerprint = useRef<string | undefined>(undefined);
    quoteFingerprint.current = state.status === "ready" ? state.quote.quoteFingerprint : undefined;

    useEffect(() => {
        const invalidate = (fingerprint?: string) => {
            if (!fingerprint || quoteFingerprint.current === fingerprint) setRevision((current) => current + 1);
        };
        return subscribeTaskBillingQuoteInvalidation(invalidate);
    }, []);

    useEffect(() => {
        const sequence = ++requestSequence.current;
        if (!request) {
            setState(initialTaskBillingQuoteState);
            return;
        }

        setState({ status: "loading", quote: null, error: null });
        let controller: AbortController | null = null;
        const timer = window.setTimeout(() => {
            controller = new AbortController();
            void load(request, controller.signal).then(
                (quote) => {
                    if (requestSequence.current !== sequence || controller?.signal.aborted) return;
                    setState({ status: "ready", quote, error: null });
                },
                (error: unknown) => {
                    if (requestSequence.current !== sequence || controller?.signal.aborted) return;
                    setState({ status: "error", quote: null, error: error instanceof Error ? error.message : "预计积分获取失败" });
                },
            );
        }, billingQuoteDebounceMilliseconds);

        return () => {
            window.clearTimeout(timer);
            controller?.abort();
        };
    }, [load, request, revision]);

    return state;
}
