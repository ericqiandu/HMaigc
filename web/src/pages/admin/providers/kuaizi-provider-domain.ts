import { providerCredentialSecretRequest, type AdminProviderAccount, type AdminProviderCredential, type ProviderAdapterDescriptor } from "@/services/api/provider-accounts";

export type ProviderFamilyView = {
    adapter: ProviderAdapterDescriptor;
    credential?: AdminProviderCredential;
};

export function credentialSecretRequest(value: string): { key: string } | null {
    return providerCredentialSecretRequest(value);
}

export function formatKuaiziBalance(value: string): string {
    if (value === "") return "尚未验证";
    if (!/^(?:0|[1-9]\d*)$/.test(value)) throw new Error("余额分值必须是规范化非负十进制整数");
    const padded = value.padStart(3, "0");
    const integer = padded.slice(0, -2).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return `${integer}.${padded.slice(-2)} 筷子点数`;
}

export function providerFamilyViews(account: AdminProviderAccount): ProviderFamilyView[] {
    const credentials = new Map(account.credentials.map((credential) => [credential.family, credential]));
    return account.adapters.map((adapter) => ({ adapter, credential: credentials.get(adapter.family) }));
}

export function endpointDraftChanged(account: AdminProviderAccount, value: string): boolean {
    const baseline = account.endpointCandidate?.baseUrl ?? account.endpoint?.baseUrl ?? "";
    return value.trim() !== baseline;
}
