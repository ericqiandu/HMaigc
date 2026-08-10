import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

import { paymentFormValuesEqual, toPaymentFormValues, toPaymentSettingRequest, type PaymentFormValues } from "../src/pages/admin/settings/payment-settings-form";
import type { AdminPaymentSetting, UpdatePaymentSettingInput } from "../src/services/api/payment";

const adminSetting = {
    checkoutBaseUrl: "https://hm.kunagent.com",
    wechat: {
        enabled: true,
        appId: "wx-app-id",
        merchantId: "1900000001",
        merchantSerialNo: "merchant-api-certificate-serial",
        wechatpayPublicKeyId: "PUB_KEY_ID_3000000001",
        notifyUrl: "https://hm.kunagent.com/api/payments/webhooks/wechat",
        gatewayUrl: "https://api.mch.weixin.qq.com",
        hasMerchantPrivateKey: true,
        hasWechatpayPublicKey: true,
        hasApiV3Key: true,
        ready: true,
    },
    alipay: {
        enabled: false,
        appId: "alipay-app-id",
        merchantId: "2088000000000000",
        notifyUrl: "https://hm.kunagent.com/api/payments/webhooks/alipay",
        gatewayUrl: "https://openapi.alipay.com/gateway.do",
        hasMerchantPrivateKey: true,
        hasPlatformPublicKey: true,
        ready: false,
    },
    updatedBy: "admin-user",
    createdAt: "2026-08-10T09:00:00Z",
    updatedAt: "2026-08-10T10:00:00Z",
} satisfies AdminPaymentSetting;

describe("payment settings WeChat public-key contract", () => {
    test("maps configured secrets to blank inputs without making the untouched form dirty", () => {
        const values = toPaymentFormValues(adminSetting);

        expect(values).toEqual({
            checkoutBaseUrl: "https://hm.kunagent.com",
            wechat: {
                enabled: true,
                appId: "wx-app-id",
                merchantId: "1900000001",
                merchantSerialNo: "merchant-api-certificate-serial",
                merchantPrivateKey: "",
                wechatpayPublicKeyId: "PUB_KEY_ID_3000000001",
                wechatpayPublicKey: "",
                apiV3Key: "",
                notifyUrl: "https://hm.kunagent.com/api/payments/webhooks/wechat",
                gatewayUrl: "https://api.mch.weixin.qq.com",
            },
            alipay: {
                enabled: false,
                appId: "alipay-app-id",
                merchantId: "2088000000000000",
                merchantPrivateKey: "",
                platformPublicKey: "",
                notifyUrl: "https://hm.kunagent.com/api/payments/webhooks/alipay",
                gatewayUrl: "https://openapi.alipay.com/gateway.do",
            },
        });
        expect(paymentFormValuesEqual(values, adminSetting)).toBe(true);
        expect(paymentFormValuesEqual({ ...values, wechat: { ...values.wechat, wechatpayPublicKey: "new-key" } }, adminSetting)).toBe(false);
    });

    test("emits separate trimmed provider requests with no cross-provider credential fields", () => {
        const values: PaymentFormValues = {
            checkoutBaseUrl: " https://hm.kunagent.com ",
            wechat: {
                ...toPaymentFormValues(adminSetting).wechat,
                wechatpayPublicKeyId: " PUB_KEY_ID_3000000001 ",
                wechatpayPublicKey: " PUBLIC KEY PEM ",
                merchantPrivateKey: " MERCHANT PRIVATE KEY ",
                apiV3Key: " 0123456789ABCDEF0123456789ABCDEF ",
            },
            alipay: {
                ...toPaymentFormValues(adminSetting).alipay,
                merchantPrivateKey: " ALIPAY PRIVATE KEY ",
                platformPublicKey: " ALIPAY PUBLIC KEY ",
            },
        };

        const request = toPaymentSettingRequest(values) satisfies UpdatePaymentSettingInput;
        expect(request.wechat).toMatchObject({
            merchantSerialNo: "merchant-api-certificate-serial",
            merchantPrivateKey: "MERCHANT PRIVATE KEY",
            wechatpayPublicKeyId: "PUB_KEY_ID_3000000001",
            wechatpayPublicKey: "PUBLIC KEY PEM",
            apiV3Key: "0123456789ABCDEF0123456789ABCDEF",
        });
        expect(Object.hasOwn(request.wechat, "platformPublicKey")).toBe(false);
        expect(request.alipay).toMatchObject({
            merchantPrivateKey: "ALIPAY PRIVATE KEY",
            platformPublicKey: "ALIPAY PUBLIC KEY",
        });
        expect(Object.hasOwn(request.alipay, "merchantSerialNo")).toBe(false);
        expect(Object.hasOwn(request.alipay, "wechatpayPublicKeyId")).toBe(false);
        expect(Object.hasOwn(request.alipay, "wechatpayPublicKey")).toBe(false);
        expect(Object.hasOwn(request.alipay, "apiV3Key")).toBe(false);
    });

    test("the real administrator page exposes only the current WeChat public-key workflow", () => {
        const page = readFileSync(new URL("../src/pages/admin/settings/payment-settings-page.tsx", import.meta.url), "utf8");

        for (const expected of ["微信支付公钥 ID", "PUB_KEY_ID_...", "微信支付公钥", "账户中心 → API安全 → 微信支付公钥"]) {
            expect(page).toContain(expected);
        }
        expect(page).not.toContain("微信支付需要商户私钥、平台公钥");
        expect(page).not.toContain("<PaymentChannelCard");
    });
});
