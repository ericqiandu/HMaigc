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

export type SiteSettings = {
    siteName: string;
    homeHeroSlogan: string;
    logoUrl: string;
    footerCopyright: string;
    icpRegistrationNumber: string;
    icpRegistrationUrl: string;
    publicSecurityRegistrationNumber: string;
    publicSecurityRegistrationUrl: string;
    userAgreement: string;
    privacyPolicy: string;
    membershipAgreement: string;
    homeBannerEnabled: boolean;
    homeBannerLabel: string;
    homeBannerText: string;
    homeBannerPrimaryActionLabel: string;
    homeBannerPrimaryActionUrl: string;
    homeBannerSecondaryActionLabel: string;
    homeBannerSecondaryActionUrl: string;
    homeBannerFrequency: "always" | "once" | "daily" | "session";
    marketingPopupEnabled: boolean;
    marketingPopupImageUrl: string;
    marketingPopupTitle: string;
    marketingPopupDescription: string;
    marketingPopupActionLabel: string;
    marketingPopupActionUrl: string;
    marketingPopupFrequency: "once" | "daily" | "session";
    updatedBy: string;
    createdAt: string;
    updatedAt: string;
};

export type UpdateSiteSettingsInput = Pick<
    SiteSettings,
    | "siteName"
    | "homeHeroSlogan"
    | "footerCopyright"
    | "icpRegistrationNumber"
    | "icpRegistrationUrl"
    | "publicSecurityRegistrationNumber"
    | "publicSecurityRegistrationUrl"
    | "homeBannerEnabled"
    | "homeBannerLabel"
    | "homeBannerText"
    | "homeBannerPrimaryActionLabel"
    | "homeBannerPrimaryActionUrl"
    | "homeBannerSecondaryActionLabel"
    | "homeBannerSecondaryActionUrl"
    | "homeBannerFrequency"
    | "marketingPopupEnabled"
    | "marketingPopupTitle"
    | "marketingPopupDescription"
    | "marketingPopupActionLabel"
    | "marketingPopupActionUrl"
    | "marketingPopupFrequency"
>;

export type UpdateLegalSettingsInput = Pick<SiteSettings, "userAgreement" | "privacyPolicy" | "membershipAgreement">;

export const publicSiteSettingsQueryKey = ["public-site-settings"] as const;
export const adminSiteSettingsQueryKey = ["admin-site-settings"] as const;

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

export function getPublicSiteSettings() {
    return request<SiteSettings>(api.get("/public/site"));
}

export function getAdminSiteSettings() {
    return request<SiteSettings>(api.get("/admin/settings/site"));
}

export function updateAdminSiteSettings(input: UpdateSiteSettingsInput) {
    return request<SiteSettings>(api.put("/admin/settings/site", input));
}

export function updateAdminLegalSettings(input: UpdateLegalSettingsInput) {
    return request<SiteSettings>(api.put("/admin/settings/legal", input));
}

export function uploadAdminSiteLogo(file: File) {
    const formData = new FormData();
    formData.append("file", file, file.name);
    return request<SiteSettings>(api.post("/admin/settings/site/logo", formData));
}

export function removeAdminSiteLogo() {
    return request<SiteSettings>(api.delete("/admin/settings/site/logo"));
}

export function uploadAdminMarketingPopupImage(file: File) {
    const formData = new FormData();
    formData.append("file", file, file.name);
    return request<SiteSettings>(api.post("/admin/settings/site/marketing-image", formData));
}

export function removeAdminMarketingPopupImage() {
    return request<SiteSettings>(api.delete("/admin/settings/site/marketing-image"));
}
