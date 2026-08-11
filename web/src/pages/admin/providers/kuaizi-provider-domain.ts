import { providerCredentialSecretRequest, type AdminProviderAccount, type AdminProviderCredential, type ProviderAdapterDescriptor } from "@/services/api/provider-accounts";

export type ProviderFamilyView = {
    adapter: ProviderAdapterDescriptor;
    credential?: AdminProviderCredential;
};

export function credentialSecretRequest(value: string): { key: string } | null {
    return providerCredentialSecretRequest(value);
}

export function formatKuaiziBalance(value: string): string {
    if (!/^\d+$/.test(value)) return "尚未验证";
    const grouped = value.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    return `${grouped} 筷子点数`;
}

export function providerFamilyViews(account: AdminProviderAccount): ProviderFamilyView[] {
    const credentials = new Map(account.credentials.map((credential) => [credential.family, credential]));
    return account.adapters.map((adapter) => ({ adapter, credential: credentials.get(adapter.family) }));
}

export function endpointDraftChanged(account: AdminProviderAccount, value: string): boolean {
    const baseline = account.endpointCandidate?.baseUrl ?? account.endpoint?.baseUrl ?? "";
    return value.trim() !== baseline;
}
