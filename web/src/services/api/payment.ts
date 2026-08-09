import axios from "axios";

const api = axios.create({
    baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api",
    withCredentials: true,
});

type BackendEnvelope<T> = {
    code: number;
    data: T;
    msg: string;
};

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) {
            throw new Error(error.response?.data?.msg || error.message || "请求失败");
        }
        throw error;
    }
}

export type AdminPaymentChannelSetting = {
    enabled: boolean;
    appId: string;
    merchantId: string;
    merchantSerialNo: string;
    notifyUrl: string;
    gatewayUrl: string;
    hasMerchantPrivateKey: boolean;
    hasPlatformPublicKey: boolean;
    hasApiV3Key: boolean;
    ready: boolean;
};

export type AdminPaymentSetting = {
    checkoutBaseUrl: string;
    wechat: AdminPaymentChannelSetting;
    alipay: AdminPaymentChannelSetting;
    updatedBy: string;
    createdAt: string;
    updatedAt: string;
};

export type PaymentChannelSettingInput = {
    enabled: boolean;
    appId: string;
    merchantId: string;
    merchantSerialNo: string;
    merchantPrivateKey: string;
    platformPublicKey: string;
    apiV3Key: string;
    notifyUrl: string;
    gatewayUrl: string;
};

export type UpdatePaymentSettingInput = {
    checkoutBaseUrl: string;
    wechat: PaymentChannelSettingInput;
    alipay: PaymentChannelSettingInput;
};

export type PaymentProvider = "wechat" | "alipay";
export type PaymentOrderStatus = "pending" | "paid" | "cancelled" | "refunded";
export type PaymentCheckoutStatus = "active" | "expired" | "consumed";
export type PaymentTransactionStatus = "created" | "pending" | "review_required" | "paid" | "closed" | "failed" | "refunded";
export type PaymentWebhookStatus = "received" | "processed" | "rejected" | "review_required";
export type PaymentProviderState = "paid" | "unpaid" | "not_found" | "unknown";

export type PaymentTransaction = {
    id: string;
    orderType: "membership" | "credit_topup";
    orderId: string;
    userId: string;
    provider: PaymentProvider;
    merchantOrderNo: string;
    providerTradeNo?: string;
    amountCents: number;
    currency: string;
    status: PaymentTransactionStatus;
    codeUrl?: string;
    failureReason?: string;
    expiresAt?: string;
    paidAt?: string;
    closedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type PaymentWebhookEvent = {
    id: string;
    provider: PaymentProvider;
    providerEventId: string;
    transactionId?: string;
    payloadDigest: string;
    status: PaymentWebhookStatus;
    failureReason?: string;
    receivedAt: string;
    processedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type AdminPaymentReconciliationResult = {
    transaction: PaymentTransaction;
    providerState: PaymentProviderState;
};

export type CreatePaymentCheckoutResult = {
    checkoutUrl: string;
    expiresAt: string;
};

export type MembershipCheckoutSummary = {
    audience: "personal" | "team";
    code: string;
    name: string;
    tier: string;
    billingCycle: "month" | "year";
    seats: number;
    actualPriceCents: number;
    originalPriceCents: number;
    creditsPerPeriod: number;
    totalCreditsPerPeriod: number;
};

export type CreditTopupCheckoutSummary = {
    actualPriceCents: number;
    totalMicrocredits: number;
};

export type PaymentCheckoutActiveTransaction = {
    provider: PaymentProvider;
    status: "pending";
    codeUrl: string;
    expiresAt: string;
};

type PaymentCheckoutBase = {
    orderNumber: string;
    orderStatus: PaymentOrderStatus;
    checkoutStatus: PaymentCheckoutStatus;
    currency: string;
    serverNow: string;
    expiresAt: string;
    providers: PaymentProvider[];
    activeTransaction?: PaymentCheckoutActiveTransaction;
};

export type MembershipPaymentCheckout = PaymentCheckoutBase & {
    orderType: "membership";
    membershipSummary: MembershipCheckoutSummary;
    creditTopupSummary?: never;
};

export type CreditTopupPaymentCheckout = PaymentCheckoutBase & {
    orderType: "credit_topup";
    membershipSummary?: never;
    creditTopupSummary: CreditTopupCheckoutSummary;
};

export type PaymentCheckout = MembershipPaymentCheckout | CreditTopupPaymentCheckout;

export type AdminPaymentPage<T> = {
    items: T[];
    total: number;
    page: number;
    limit: number;
};

export function getAdminPaymentSetting() {
    return request<AdminPaymentSetting>(api.get("/admin/settings/payment"));
}

export function updateAdminPaymentSetting(input: UpdatePaymentSettingInput) {
    return request<AdminPaymentSetting>(api.put("/admin/settings/payment", input));
}

export function listAdminPaymentTransactions() {
    return request<AdminPaymentPage<PaymentTransaction>>(api.get("/admin/payments/transactions"));
}

export function listAdminPaymentWebhookEvents() {
    return request<AdminPaymentPage<PaymentWebhookEvent>>(api.get("/admin/payments/webhooks"));
}

export function reconcileAdminPaymentTransaction(transactionId: string) {
    return request<AdminPaymentReconciliationResult>(api.post(`/admin/payments/transactions/${encodeURIComponent(transactionId)}/reconcile`));
}

export function createPaymentCheckout(orderId: string) {
    return request<CreatePaymentCheckoutResult>(api.post(`/membership/orders/${orderId}/checkout`));
}

export function getPaymentCheckout(token: string) {
    return request<PaymentCheckout>(api.get(`/payments/checkout/${encodeURIComponent(token)}`));
}

export function createPaymentTransaction(token: string, provider: PaymentProvider) {
    return request<PaymentCheckoutActiveTransaction>(api.post(`/payments/checkout/${encodeURIComponent(token)}/transactions`, { provider }));
}
