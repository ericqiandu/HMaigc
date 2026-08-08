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

export type MembershipAudience = "personal" | "team";
export type MembershipBillingCycle = "free" | "month" | "year";

export type MembershipPlan = {
    id: string;
    code: string;
    name: string;
    tier: string;
    audience: MembershipAudience;
    billingCycle: MembershipBillingCycle;
    priceCents: number;
    originalPriceCents: number;
    currency: string;
    creditsPerPeriod: number;
    imageConcurrency: number;
    videoConcurrency: number;
    unlimitedTaskQueue: boolean;
    teamStorageBytes: number;
    sharedAssetsEnabled: boolean;
    projectPermissionsEnabled: boolean;
    invoicingEnabled: boolean;
    commercialUseEnabled: boolean;
    topupDiscountBasisPoints: number;
    minSeats: number;
    maxSeats: number;
    benefitsJson: string;
    benefits: string[];
    enabled: boolean;
    sortOrder: number;
    createdAt: string;
    updatedAt: string;
};

export type MembershipEntitlement = {
    planId: string;
    planName: string;
    tier: string;
    audience: MembershipAudience;
    isActiveMember: boolean;
    imageConcurrency: number;
    videoConcurrency: number;
    topupDiscountBasisPoints: number;
    unlimitedTaskQueue: boolean;
    teamStorageBytes: number;
    sharedAssetsEnabled: boolean;
    projectPermissionsEnabled: boolean;
    invoicingEnabled: boolean;
    commercialUseEnabled: boolean;
    teamId?: string;
    expiresAt?: string;
};

export type MembershipOrder = {
    id: string;
    orderNumber: string;
    userId: string;
    teamId?: string;
    planId: string;
    seats: number;
    unitPriceCents: number;
    totalPriceCents: number;
    currency: string;
    status: "pending" | "paid" | "cancelled" | "refunded";
    planSnapshotJson: string;
    paymentProvider: string;
    providerTradeNo: string;
    resolutionNote?: string;
    paidAt?: string;
    createdAt: string;
    updatedAt: string;
};

export type Team = { id: string; ownerUserId: string; name: string; status: "active" | "disabled"; createdAt: string; updatedAt: string };

export type MembershipOverview = { entitlement: MembershipEntitlement; orders: MembershipOrder[]; teams: Team[] };

export type MembershipStorefrontPromotion = {
    enabled: boolean;
    title: string;
    subtitle: string;
    subtitleHighlight: string;
    endsAt: string;
};

export type MembershipStorefrontActivity = { icon: string; text: string };

export type MembershipStorefrontCopy = {
    creatorTab: string;
    teamTab: string;
    yearCycle: string;
    monthCycle: string;
    creditStore: string;
    activityHeading: string;
    exclusiveHeading: string;
    generationHeading: string;
    faqHeading: string;
};

export type MembershipStorefrontPlanHighlight = {
    tier: string;
    images: string;
    videos: string;
};

export type MembershipStorefrontGenerationColumn = { key: string; label: string };

export type MembershipStorefrontGenerationRow = {
    model: string;
    icon: string;
    unit: string;
    values: string[];
};

export type MembershipStorefrontGenerationSection = {
    title: string;
    rows: MembershipStorefrontGenerationRow[];
};

export type MembershipStorefrontFAQ = { question: string; answer: string };

export type MembershipStorefrontSetting = {
    promotion: MembershipStorefrontPromotion;
    copy: MembershipStorefrontCopy;
    activities: MembershipStorefrontActivity[];
    commonFeatures: string[];
    exclusiveFeatures: string[];
    planHighlights: MembershipStorefrontPlanHighlight[];
    generationColumns: MembershipStorefrontGenerationColumn[];
    generationSections: MembershipStorefrontGenerationSection[];
    generationFootnote: string;
    membershipNotes: string[];
    faqs: MembershipStorefrontFAQ[];
};

export type MembershipStorefront = {
    presentation: MembershipStorefrontSetting;
    plans: MembershipPlan[];
    serverNow: string;
    updatedAt?: string;
};

export function getMembershipStorefront() {
    return request<MembershipStorefront>(api.get("/membership/storefront"));
}

export function listMembershipPlans() {
    return request<MembershipPlan[]>(api.get("/membership/plans"));
}

export function getMyMembership() {
    return request<MembershipOverview>(api.get("/membership"));
}

export function createMembershipOrder(input: { planId: string; teamId?: string; seats?: number }) {
    return request<MembershipOrder>(api.post("/membership/orders", input));
}

export function cancelMembershipOrder(id: string) {
    return request<MembershipOrder>(api.post(`/membership/orders/${encodeURIComponent(id)}/cancel`, {}));
}

export function createTeam(name: string) {
    return request<Team>(api.post("/teams", { name }));
}

export function listAdminMembershipPlans() {
    return request<MembershipPlan[]>(api.get("/admin/membership/plans"));
}

export function getAdminMembershipStorefront() {
    return request<MembershipStorefront>(api.get("/admin/membership/storefront"));
}

export function updateAdminMembershipStorefront(input: MembershipStorefrontSetting) {
    return request<MembershipStorefront>(api.put("/admin/membership/storefront", input));
}

export type UpdateMembershipPlanInput = {
    name: string;
    priceCents: number;
    originalPriceCents: number;
    creditsPerPeriod: number;
    imageConcurrency: number;
    videoConcurrency: number;
    topupDiscountBasisPoints: number;
    unlimitedTaskQueue: boolean;
    teamStorageBytes: number;
    sharedAssetsEnabled: boolean;
    projectPermissionsEnabled: boolean;
    invoicingEnabled: boolean;
    commercialUseEnabled: boolean;
    minSeats: number;
    maxSeats: number;
    benefits: string[];
    enabled: boolean;
    sortOrder: number;
};

export type InvoiceRequestStatus = "pending" | "issued" | "rejected";

export type InvoiceRequest = {
    id: string;
    userId: string;
    teamId?: string;
    membershipOrderId: string;
    title: string;
    taxNumber?: string;
    email: string;
    amountCents: number;
    status: InvoiceRequestStatus;
    invoiceNumber?: string;
    invoiceUrl?: string;
    resolutionNote?: string;
    resolvedBy?: string;
    resolvedAt?: string;
    createdAt: string;
    updatedAt: string;
};

export function updateAdminMembershipPlan(id: string, input: UpdateMembershipPlanInput) {
    return request<MembershipPlan>(api.patch(`/admin/membership/plans/${encodeURIComponent(id)}`, input));
}

export function listAdminMembershipOrders(page = 1, limit = 30) {
    return request<{ items: MembershipOrder[]; total: number; page: number; limit: number }>(api.get("/admin/membership/orders", { params: { page, limit } }));
}

export function confirmAdminMembershipOrder(id: string, input: { providerTradeNo: string; note: string }) {
    return request<MembershipOrder>(api.post(`/admin/membership/orders/${encodeURIComponent(id)}/confirm`, input));
}

export function closeAdminMembershipOrder(id: string, input: { note: string }) {
    return request<MembershipOrder>(api.post(`/admin/membership/orders/${encodeURIComponent(id)}/close`, input));
}

export function listMyInvoiceRequests() {
    return request<{ items: InvoiceRequest[] }>(api.get("/membership/invoices"));
}

export function createInvoiceRequest(input: { membershipOrderId: string; title: string; taxNumber?: string; email: string }) {
    return request<InvoiceRequest>(api.post("/membership/invoices", input));
}

export function listAdminInvoiceRequests(status?: InvoiceRequestStatus, page = 1, limit = 30) {
    return request<{ items: InvoiceRequest[]; total: number; page: number; limit: number }>(api.get("/admin/membership/invoices", { params: { status, page, limit } }));
}

export function resolveAdminInvoiceRequest(
    id: string,
    input: {
        status: Exclude<InvoiceRequestStatus, "pending">;
        invoiceNumber?: string;
        invoiceUrl?: string;
        note: string;
    },
) {
    return request<{ resolved: boolean }>(api.post(`/admin/membership/invoices/${encodeURIComponent(id)}/resolve`, input));
}
