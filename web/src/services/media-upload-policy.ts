const GUEST_SCOPE = "guest";

export function persistMediaByUserScope<T>(scope: string, persistRemote: () => Promise<T>, persistLocal: () => Promise<T>) {
    return scope === GUEST_SCOPE ? persistLocal() : persistRemote();
}
