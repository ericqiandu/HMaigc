import axios from "axios";

const api = axios.create({ baseURL: import.meta.env.VITE_CANVAS_BACKEND_URL || "/api", withCredentials: true });

type BackendEnvelope<T> = { code: number; data: T; msg: string };

async function request<T>(promise: Promise<{ data: BackendEnvelope<T> }>): Promise<T> {
    try {
        const response = await promise;
        if (response.data.code !== 0) throw new Error(response.data.msg || "请求失败");
        return response.data.data;
    } catch (error) {
        if (axios.isAxiosError<BackendEnvelope<unknown>>(error)) throw new Error(error.response?.data?.msg || error.message || "请求失败");
        throw error;
    }
}

export type CreditProductCategory = "surprise" | "general";

export type CreditTopupProduct = {
    id: string;
    code: string;
    name: string;
    category: CreditProductCategory;
    baseMicrocredits: number;
    bonusMicrocredits: number;
    priceCents: number;
    effectivePriceCents?: number;
    discountBasisPoints?: number;
    originalPriceCents: number;
    currency: string;
    requiredMembershipTier?: string;
    weeklyPurchaseLimit: number;
    stockLimit: number;
    soldCount: number;
    saleEndsAt?: string;
    badge?: string;
    description?: string;
    imageUrl?: string;
    enabled: boolean;
    sortOrder: number;
    createdAt: string;
    updatedAt: string;
};

export type CreditTopupOrder = {
    id: string;
    orderNumber: string;
    userId: string;
    productId: string;
    baseMicrocredits: number;
    bonusMicrocredits: number;
    totalMicrocredits: number;
    totalPriceCents: number;
    currency: string;
    status: "pending" | "paid" | "cancelled" | "refunded";
    productSnapshotJson: string;
    paymentProvider?: string;
    providerTradeNo?: string;
    resolutionNote?: string;
    paidAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type SaveCreditTopupProductInput = Omit<
    CreditTopupProduct,
	"id" | "currency" | "soldCount" | "effectivePriceCents" | "discountBasisPoints" | "createdAt" | "updatedAt"
>;

export function getCreditStorefront() {
    return request<{ products: CreditTopupProduct[]; serverNow: string }>(api.get("/credit-store"));
}

export function createCreditTopupOrder(productId: string, idempotencyKey: string) {
    return request<CreditTopupOrder>(api.post("/credit-store/orders", { productId }, { headers: { "Idempotency-Key": idempotencyKey } }));
}

export function createCreditTopupCheckout(orderId: string) {
    return request<{ checkoutUrl: string; expiresAt: string }>(api.post(`/credit-store/orders/${encodeURIComponent(orderId)}/checkout`));
}

export function listAdminCreditTopupProducts() {
    return request<{ items: CreditTopupProduct[] }>(api.get("/admin/credit-store/products"));
}

export function saveAdminCreditTopupProduct(id: string, input: SaveCreditTopupProductInput) {
    return request<CreditTopupProduct>(api.put(`/admin/credit-store/products/${encodeURIComponent(id)}`, input));
}

export function createAdminCreditTopupProduct(input: SaveCreditTopupProductInput) {
    return request<CreditTopupProduct>(api.post("/admin/credit-store/products", input));
}

export function listAdminCreditTopupOrders(page = 1, limit = 30) {
    return request<{ items: CreditTopupOrder[]; total: number; page: number; limit: number }>(api.get("/admin/credit-store/orders", { params: { page, limit } }));
}
