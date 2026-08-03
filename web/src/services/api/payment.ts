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
export type MembershipOrderStatus = "pending" | "paid" | "cancelled" | "expired" | "refunded";
export type PaymentTransactionStatus = "created" | "pending" | "paid" | "closed" | "failed" | "refunded";
export type PaymentWebhookStatus = "received" | "processed" | "rejected";

export type PaymentTransaction = {
    id: string;
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

export type CreatePaymentCheckoutResult = {
    checkoutUrl: string;
    expiresAt: string;
};

export type PaymentCheckout = {
    orderId: string;
    orderNumber: string;
    amountCents: number;
    currency: string;
    status: MembershipOrderStatus;
    expiresAt: string;
    providers: PaymentProvider[];
};

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

export function createPaymentCheckout(orderId: string) {
    return request<CreatePaymentCheckoutResult>(api.post(`/membership/orders/${orderId}/checkout`));
}

export function getPaymentCheckout(token: string) {
    return request<PaymentCheckout>(api.get(`/payments/checkout/${encodeURIComponent(token)}`));
}

export function createPaymentTransaction(token: string, provider: PaymentProvider) {
    return request<PaymentTransaction>(api.post(`/payments/checkout/${encodeURIComponent(token)}/transactions`, { provider }));
}
