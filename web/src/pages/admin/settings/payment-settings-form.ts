import type { AdminAlipayPaymentChannelSetting, AdminPaymentSetting, AdminWechatPaymentChannelSetting, AlipayPaymentChannelSettingInput, UpdatePaymentSettingInput, WechatPaymentChannelSettingInput } from "@/services/api/payment";

export type WechatPaymentChannelFormValues = Partial<WechatPaymentChannelSettingInput>;
export type AlipayPaymentChannelFormValues = Partial<AlipayPaymentChannelSettingInput>;

export type PaymentFormValues = {
    checkoutBaseUrl?: string;
    wechat?: WechatPaymentChannelFormValues;
    alipay?: AlipayPaymentChannelFormValues;
};

export function toPaymentFormValues(setting: AdminPaymentSetting): PaymentFormValues {
    return {
        checkoutBaseUrl: setting.checkoutBaseUrl,
        wechat: {
            enabled: setting.wechat.enabled,
            appId: setting.wechat.appId,
            merchantId: setting.wechat.merchantId,
            merchantSerialNo: setting.wechat.merchantSerialNo,
            merchantPrivateKey: "",
            wechatpayPublicKeyId: setting.wechat.wechatpayPublicKeyId,
            wechatpayPublicKey: "",
            apiV3Key: "",
            notifyUrl: setting.wechat.notifyUrl,
            gatewayUrl: setting.wechat.gatewayUrl,
        },
        alipay: {
            enabled: setting.alipay.enabled,
            appId: setting.alipay.appId,
            merchantId: setting.alipay.merchantId,
            merchantPrivateKey: "",
            platformPublicKey: "",
            notifyUrl: setting.alipay.notifyUrl,
            gatewayUrl: setting.alipay.gatewayUrl,
        },
    };
}

export function toPaymentSettingRequest(values: PaymentFormValues): UpdatePaymentSettingInput {
    return {
        checkoutBaseUrl: cleanPaymentSettingValue(values.checkoutBaseUrl),
        wechat: wechatPaymentSettingRequest(values.wechat),
        alipay: alipayPaymentSettingRequest(values.alipay),
    };
}

export function paymentFormValuesEqual(values: PaymentFormValues, setting: AdminPaymentSetting): boolean {
    return cleanPaymentSettingValue(values.checkoutBaseUrl) === cleanPaymentSettingValue(setting.checkoutBaseUrl) && wechatPaymentValuesEqual(values.wechat, setting.wechat) && alipayPaymentValuesEqual(values.alipay, setting.alipay);
}

function wechatPaymentSettingRequest(values?: WechatPaymentChannelFormValues): WechatPaymentChannelSettingInput {
    return {
        enabled: values?.enabled === true,
        appId: cleanPaymentSettingValue(values?.appId),
        merchantId: cleanPaymentSettingValue(values?.merchantId),
        merchantSerialNo: cleanPaymentSettingValue(values?.merchantSerialNo),
        merchantPrivateKey: cleanPaymentSettingValue(values?.merchantPrivateKey),
        wechatpayPublicKeyId: cleanPaymentSettingValue(values?.wechatpayPublicKeyId),
        wechatpayPublicKey: cleanPaymentSettingValue(values?.wechatpayPublicKey),
        apiV3Key: cleanPaymentSettingValue(values?.apiV3Key),
        notifyUrl: cleanPaymentSettingValue(values?.notifyUrl),
        gatewayUrl: cleanPaymentSettingValue(values?.gatewayUrl),
    };
}

function alipayPaymentSettingRequest(values?: AlipayPaymentChannelFormValues): AlipayPaymentChannelSettingInput {
    return {
        enabled: values?.enabled === true,
        appId: cleanPaymentSettingValue(values?.appId),
        merchantId: cleanPaymentSettingValue(values?.merchantId),
        merchantPrivateKey: cleanPaymentSettingValue(values?.merchantPrivateKey),
        platformPublicKey: cleanPaymentSettingValue(values?.platformPublicKey),
        notifyUrl: cleanPaymentSettingValue(values?.notifyUrl),
        gatewayUrl: cleanPaymentSettingValue(values?.gatewayUrl),
    };
}

function wechatPaymentValuesEqual(values: WechatPaymentChannelFormValues | undefined, setting: AdminWechatPaymentChannelSetting): boolean {
    return (
        (values?.enabled === true) === setting.enabled &&
        cleanPaymentSettingValue(values?.appId) === cleanPaymentSettingValue(setting.appId) &&
        cleanPaymentSettingValue(values?.merchantId) === cleanPaymentSettingValue(setting.merchantId) &&
        cleanPaymentSettingValue(values?.merchantSerialNo) === cleanPaymentSettingValue(setting.merchantSerialNo) &&
        cleanPaymentSettingValue(values?.wechatpayPublicKeyId) === cleanPaymentSettingValue(setting.wechatpayPublicKeyId) &&
        cleanPaymentSettingValue(values?.notifyUrl) === cleanPaymentSettingValue(setting.notifyUrl) &&
        cleanPaymentSettingValue(values?.gatewayUrl) === cleanPaymentSettingValue(setting.gatewayUrl) &&
        !cleanPaymentSettingValue(values?.merchantPrivateKey) &&
        !cleanPaymentSettingValue(values?.wechatpayPublicKey) &&
        !cleanPaymentSettingValue(values?.apiV3Key)
    );
}

function alipayPaymentValuesEqual(values: AlipayPaymentChannelFormValues | undefined, setting: AdminAlipayPaymentChannelSetting): boolean {
    return (
        (values?.enabled === true) === setting.enabled &&
        cleanPaymentSettingValue(values?.appId) === cleanPaymentSettingValue(setting.appId) &&
        cleanPaymentSettingValue(values?.merchantId) === cleanPaymentSettingValue(setting.merchantId) &&
        cleanPaymentSettingValue(values?.notifyUrl) === cleanPaymentSettingValue(setting.notifyUrl) &&
        cleanPaymentSettingValue(values?.gatewayUrl) === cleanPaymentSettingValue(setting.gatewayUrl) &&
        !cleanPaymentSettingValue(values?.merchantPrivateKey) &&
        !cleanPaymentSettingValue(values?.platformPublicKey)
    );
}

function cleanPaymentSettingValue(value?: string): string {
    return value?.trim() || "";
}
