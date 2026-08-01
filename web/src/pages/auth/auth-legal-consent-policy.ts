export type AuthLegalConsentValidation = { ok: true } | { ok: false; message: string };

export function validateAuthLegalConsent(accepted: boolean): AuthLegalConsentValidation {
    if (accepted) return { ok: true };
    return {
        ok: false,
        message: "请先阅读并同意《用户协议》和《隐私政策》",
    };
}
