export const STOREFRONT_EXIT_DESTINATION = "/";

export function shouldExitStorefront(key: string, dismissalOwnedByOverlay: boolean): boolean {
    return key === "Escape" && !dismissalOwnedByOverlay;
}
