export type MembershipStorefrontExitIntent = "back" | "home";

export function membershipStorefrontExitIntent(historyState: unknown): MembershipStorefrontExitIntent {
    if (typeof historyState !== "object" || historyState === null || !("idx" in historyState)) return "home";
    const historyIndex = historyState.idx;
    return typeof historyIndex === "number" && Number.isFinite(historyIndex) && historyIndex > 0 ? "back" : "home";
}

export function shouldExitMembershipStorefront(key: string, paymentDialogOpen: boolean): boolean {
    return key === "Escape" && !paymentDialogOpen;
}
