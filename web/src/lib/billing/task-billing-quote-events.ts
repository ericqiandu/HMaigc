const billingQuoteInvalidatedEvent = "task-billing-quote:invalidated";

export function subscribeTaskBillingQuoteInvalidation(listener: (quoteFingerprint?: string) => void) {
    const receive = (event: Event) => {
        const detail: unknown = event instanceof window.CustomEvent ? event.detail : undefined;
        listener(isRecord(detail) && typeof detail.quoteFingerprint === "string" ? detail.quoteFingerprint : undefined);
    };
    window.addEventListener(billingQuoteInvalidatedEvent, receive);
    return () => window.removeEventListener(billingQuoteInvalidatedEvent, receive);
}

export function invalidateTaskBillingQuotes(quoteFingerprint?: string) {
    window.dispatchEvent(new window.CustomEvent(billingQuoteInvalidatedEvent, { detail: quoteFingerprint ? { quoteFingerprint } : undefined }));
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
