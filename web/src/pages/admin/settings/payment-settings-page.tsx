import { Alert, App, Button, Form, Input, Space, Switch, Tag, type FormInstance } from "antd";
import { BadgeDollarSign, CreditCard, KeyRound, ShieldCheck, Webhook } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";

import { getAdminPaymentSetting, updateAdminPaymentSetting, type AdminAlipayPaymentChannelSetting, type AdminPaymentSetting, type AdminWechatPaymentChannelSetting } from "@/services/api/payment";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, AdminSettingsActionBar, AdminSettingsSection, AdminSettingsSwitchPanel, configuredSecretText } from "../components/admin-ui";
import { paymentFormValuesEqual, toPaymentFormValues, toPaymentSettingRequest, type PaymentFormValues } from "./payment-settings-form";

type ChannelField = keyof NonNullable<PaymentFormValues["wechat"]> | keyof NonNullable<PaymentFormValues["alipay"]>;
type ChannelName = "wechat" | "alipay";

export default function PaymentSettingsPage() {
    const { message } = App.useApp();
    const [form] = Form.useForm<PaymentFormValues>();
    const [setting, setSetting] = useState<AdminPaymentSetting | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState("");
    const [dirty, setDirty] = useState(false);

    const loadSetting = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const value = await getAdminPaymentSetting();
            setSetting(value);
            form.setFieldsValue(toPaymentFormValues(value));
            setDirty(false);
        } catch (error) {
            const reason = error instanceof Error ? error.message : "读取支付配置失败";
            setLoadError(reason);
            message.error(reason);
        } finally {
            setLoading(false);
        }
    }, [form, message]);

    useEffect(() => {
        void loadSetting();
    }, [loadSetting]);

    const save = async () => {
        let values: PaymentFormValues;
        try {
            values = await form.validateFields();
        } catch {
            return;
        }
        setSaving(true);
        try {
            const result = await updateAdminPaymentSetting(toPaymentSettingRequest(values));
            setSetting(result);
            form.setFieldsValue(toPaymentFormValues(result));
            setDirty(false);
            message.success("支付配置已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存支付配置失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <AdminPageFrame title="支付配置" description="统一收银台与微信、支付宝商户参数">
            <div className="payment-settings-page admin-settings-page space-y-5">
                <Alert
                    className="payment-settings-notice"
                    type="warning"
                    showIcon
                    message="启用前请完成真实支付验收"
                    description="渠道启用且参数完整后，用户下单会直接请求真实支付渠道。请先在公网 HTTPS 环境完成回调与小额支付验证；当前仅支持一次性扫码支付，尚未接入退款通道。"
                />

                {loadError ? <AdminContentError title="支付配置加载失败" description={loadError} onRetry={() => void loadSetting()} /> : null}

                {loading ? (
                    <AdminContentSkeleton rows={10} label="正在加载支付配置" />
                ) : !loadError && setting ? (
                    <Form className="payment-settings-form" form={form} layout="vertical" requiredMark={false} disabled={saving} onValuesChange={(_, values: PaymentFormValues) => setDirty(!paymentFormValuesEqual(values, setting))}>
                        <AdminSettingsSwitchPanel
                            icon={<CreditCard className="payment-settings-checkout-icon size-4" />}
                            title="统一收银台"
                            description="用户扫码或跳转支付时使用的公开站点地址。"
                            status={
                                <Tag className="payment-settings-checkout-status" color={setting.checkoutBaseUrl ? "success" : "default"}>
                                    {setting.checkoutBaseUrl ? "已配置" : "未配置"}
                                </Tag>
                            }
                        >
                            <AdminSettingsSection
                                id="payment-checkout-address-heading"
                                icon={<Webhook className="payment-settings-checkout-address-icon size-4" />}
                                title="公开访问地址"
                                description="填写用户实际访问的 HTTPS 收银台地址，用于下单结果页和支付跳转。"
                            >
                                <Form.Item className="payment-settings-checkout-field" name="checkoutBaseUrl" label="收银台基础地址" rules={[{ type: "url", message: "请输入完整的 HTTP(S) 地址" }]}>
                                    <Input className="payment-settings-checkout-input" autoComplete="off" placeholder="例如：https://pay.example.com" />
                                </Form.Item>
                            </AdminSettingsSection>
                        </AdminSettingsSwitchPanel>

                        <WechatPaymentChannelCard form={form} setting={setting.wechat} />

                        <AlipayPaymentChannelCard form={form} setting={setting.alipay} />

                        <AdminSettingsActionBar meta={setting.updatedAt ? `上次更新：${formatTime(setting.updatedAt)}` : "尚未保存支付配置"} status={dirty ? "有未保存的支付配置变更" : "支付配置已同步 · 商户密钥仅管理员可见"}>
                            <Button className="payment-settings-save-button" type="primary" loading={saving} disabled={!dirty} onClick={() => void save()}>
                                保存支付配置
                            </Button>
                        </AdminSettingsActionBar>
                    </Form>
                ) : null}

                <div className="payment-settings-security-notes grid gap-3 text-xs text-foreground/55 sm:grid-cols-3">
                    <Notice icon={<KeyRound className="payment-settings-key-icon size-3.5" />} text="密钥留空时保留原配置" />
                    <Notice icon={<ShieldCheck className="payment-settings-shield-icon size-3.5" />} text="敏感密钥不会在后台回显" />
                    <Notice icon={<CreditCard className="payment-settings-card-icon size-3.5" />} text="参数完整不代表支付通道已接通" />
                </div>
            </div>
        </AdminPageFrame>
    );
}

function WechatPaymentChannelCard({ form, setting }: { form: FormInstance<PaymentFormValues>; setting: AdminWechatPaymentChannelSetting }) {
    return (
        <AdminSettingsSwitchPanel
            icon={<BadgeDollarSign className="payment-settings-wechat-icon size-4" />}
            title="微信支付"
            description="配置微信 Native 支付商户身份、API v3 密钥与微信支付公钥。"
            status={<PaymentChannelStatus channel="wechat" enabled={setting.enabled} ready={setting.ready} />}
        >
            <AdminSettingsSection id="payment-wechat-merchant-heading" icon={<BadgeDollarSign className="payment-settings-wechat-merchant-icon size-4" />} title="商户身份" description="配置渠道状态、已绑定的微信支付应用与商户 API 证书身份。">
                <Form.Item className="payment-settings-wechat-enabled-field" name={["wechat", "enabled"]} label="启用渠道" valuePropName="checked">
                    <Switch className="payment-settings-wechat-enabled-switch" />
                </Form.Item>
                <ChannelInput form={form} channel="wechat" field="appId" label="应用 ID（App ID）" placeholder="与商户号完成绑定的 AppID" requiredWhenEnabled />
                <ChannelInput form={form} channel="wechat" field="merchantId" label="商户号" placeholder="微信支付商户号" requiredWhenEnabled />
                <ChannelInput form={form} channel="wechat" field="merchantSerialNo" label="商户 API 证书序列号" placeholder="商户 API 证书序列号" requiredWhenEnabled />
            </AdminSettingsSection>
            <AdminSettingsSection id="payment-wechat-endpoint-heading" icon={<Webhook className="payment-settings-wechat-endpoint-icon size-4" />} title="接口地址" description="生产环境使用微信支付官方网关与可公开访问的 HTTPS 通知入口。">
                <ChannelInput form={form} channel="wechat" field="notifyUrl" label="异步通知地址" placeholder="https://hm.kunagent.com/api/payments/webhooks/wechat" url requiredWhenEnabled />
                <ChannelInput form={form} channel="wechat" field="gatewayUrl" label="支付网关地址" placeholder="https://api.mch.weixin.qq.com" url requiredWhenEnabled />
            </AdminSettingsSection>
            <AdminSettingsSection
                id="payment-wechat-signature-heading"
                icon={<KeyRound className="payment-settings-wechat-signature-icon size-4" />}
                title="签名凭据"
                description="公钥 ID 与公钥来自商户平台：账户中心 → API安全 → 微信支付公钥。敏感内容保存后不回显。"
            >
                <PemSecretInput form={form} channel="wechat" field="merchantPrivateKey" label="商户私钥" configured={setting.hasMerchantPrivateKey} />
                <ChannelInput form={form} channel="wechat" field="wechatpayPublicKeyId" label="微信支付公钥 ID" placeholder="PUB_KEY_ID_..." requiredWhenEnabled />
                <PemSecretInput form={form} channel="wechat" field="wechatpayPublicKey" label="微信支付公钥" configured={setting.hasWechatpayPublicKey} />
                <SecretInput form={form} channel="wechat" field="apiV3Key" label="API v3 密钥" configured={setting.hasApiV3Key} />
            </AdminSettingsSection>
        </AdminSettingsSwitchPanel>
    );
}

function AlipayPaymentChannelCard({ form, setting }: { form: FormInstance<PaymentFormValues>; setting: AdminAlipayPaymentChannelSetting }) {
    return (
        <AdminSettingsSwitchPanel
            icon={<BadgeDollarSign className="payment-settings-alipay-icon size-4" />}
            title="支付宝"
            description="配置支付宝应用、商户私钥和平台公钥。"
            status={<PaymentChannelStatus channel="alipay" enabled={setting.enabled} ready={setting.ready} />}
        >
            <AdminSettingsSection id="payment-alipay-merchant-heading" icon={<BadgeDollarSign className="payment-settings-alipay-merchant-icon size-4" />} title="商户身份" description="配置渠道状态、支付宝应用与商户身份。">
                <Form.Item className="payment-settings-alipay-enabled-field" name={["alipay", "enabled"]} label="启用渠道" valuePropName="checked">
                    <Switch className="payment-settings-alipay-enabled-switch" />
                </Form.Item>
                <ChannelInput form={form} channel="alipay" field="appId" label="应用 ID（App ID）" placeholder="支付宝开放平台应用 ID" requiredWhenEnabled />
                <ChannelInput form={form} channel="alipay" field="merchantId" label="商户号" placeholder="支付宝商户号" requiredWhenEnabled />
            </AdminSettingsSection>
            <AdminSettingsSection id="payment-alipay-endpoint-heading" icon={<Webhook className="payment-settings-alipay-endpoint-icon size-4" />} title="接口地址" description="配置支付宝网关与异步通知入口，生产环境必须使用可公开访问的 HTTPS 地址。">
                <ChannelInput form={form} channel="alipay" field="notifyUrl" label="异步通知地址" placeholder="https://hm.kunagent.com/api/payments/webhooks/alipay" url requiredWhenEnabled />
                <ChannelInput form={form} channel="alipay" field="gatewayUrl" label="支付网关地址" placeholder="https://openapi.alipay.com/gateway.do" url requiredWhenEnabled />
            </AdminSettingsSection>
            <AdminSettingsSection id="payment-alipay-signature-heading" icon={<KeyRound className="payment-settings-alipay-signature-icon size-4" />} title="签名凭据" description="支付宝需要商户私钥与平台公钥。敏感内容保存后不回显。">
                <PemSecretInput form={form} channel="alipay" field="merchantPrivateKey" label="商户私钥" configured={setting.hasMerchantPrivateKey} />
                <PemSecretInput form={form} channel="alipay" field="platformPublicKey" label="平台公钥" configured={setting.hasPlatformPublicKey} />
            </AdminSettingsSection>
        </AdminSettingsSwitchPanel>
    );
}

function PaymentChannelStatus({ channel, enabled, ready }: { channel: ChannelName; enabled: boolean; ready: boolean }) {
    return (
        <Space className={`payment-settings-${channel}-status`} size={6}>
            <Tag className={`payment-settings-${channel}-enabled-tag`} color={enabled ? "success" : "default"}>
                {enabled ? "已启用" : "未启用"}
            </Tag>
            <Tag className={`payment-settings-${channel}-ready-tag`} color={ready ? "blue" : "warning"}>
                {ready ? "参数完整" : "待补充参数"}
            </Tag>
        </Space>
    );
}

function ChannelInput({
    form,
    channel,
    field,
    label,
    placeholder,
    url = false,
    requiredWhenEnabled = false,
}: {
    form: FormInstance<PaymentFormValues>;
    channel: ChannelName;
    field: ChannelField;
    label: string;
    placeholder: string;
    url?: boolean;
    requiredWhenEnabled?: boolean;
}) {
    const rules = [...(requiredWhenEnabled ? [requiredChannelField(form, channel, label)] : []), ...(url ? [{ type: "url" as const, message: "请输入完整的 HTTP(S) 地址" }] : [])];
    return (
        <Form.Item className={`payment-settings-${channel}-${field}-field`} name={[channel, field]} label={label} dependencies={[[channel, "enabled"]]} rules={rules}>
            <Input className={`payment-settings-${channel}-${field}-input`} autoComplete="off" placeholder={placeholder} />
        </Form.Item>
    );
}

function SecretInput({ form, channel, field, label, configured }: { form: FormInstance<PaymentFormValues>; channel: ChannelName; field: ChannelField; label: string; configured: boolean }) {
    return (
        <Form.Item
            className={`payment-settings-${channel}-${field}-field`}
            name={[channel, field]}
            label={configured ? `${label}（${configuredSecretText}）` : label}
            dependencies={[[channel, "enabled"]]}
            rules={[requiredChannelSecret(form, channel, label, configured)]}
        >
            <Input.Password className={`payment-settings-${channel}-${field}-input`} autoComplete="new-password" placeholder={configured ? "留空保留原密钥" : `请输入${label}`} />
        </Form.Item>
    );
}

function PemSecretInput({ form, channel, field, label, configured }: { form: FormInstance<PaymentFormValues>; channel: ChannelName; field: ChannelField; label: string; configured: boolean }) {
    return (
        <Form.Item
            className={`payment-settings-${channel}-${field}-field`}
            name={[channel, field]}
            label={configured ? `${label}（${configuredSecretText}）` : label}
            extra="请粘贴包含 BEGIN/END 行的完整 PEM 内容，并保留原始换行"
            dependencies={[[channel, "enabled"]]}
            rules={[requiredChannelSecret(form, channel, label, configured)]}
        >
            <Input.TextArea
                className={`payment-settings-${channel}-${field}-input payment-settings-pem-secret-input`}
                autoComplete="off"
                autoSize={{ minRows: 4, maxRows: 8 }}
                spellCheck={false}
                placeholder={configured ? "留空保留原凭据" : `请粘贴完整的${label} PEM 内容`}
            />
        </Form.Item>
    );
}

function requiredChannelField(form: FormInstance<PaymentFormValues>, channel: ChannelName, label: string) {
    return {
        validator: (_: unknown, value?: string) => {
            if (form.getFieldValue([channel, "enabled"]) !== true || value?.trim()) return Promise.resolve();
            return Promise.reject(new Error(`启用渠道前请填写${label}`));
        },
    };
}

function requiredChannelSecret(form: FormInstance<PaymentFormValues>, channel: ChannelName, label: string, configured: boolean) {
    return {
        validator: (_: unknown, value?: string) => {
            if (form.getFieldValue([channel, "enabled"]) !== true || configured || value?.trim()) return Promise.resolve();
            return Promise.reject(new Error(`启用渠道前请填写${label}`));
        },
    };
}

function formatTime(value: string) {
    return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function Notice({ icon, text }: { icon: ReactNode; text: string }) {
    return (
        <div className="payment-settings-security-note flex items-center gap-2">
            <span className="payment-settings-security-note-icon text-foreground/40">{icon}</span>
            <span className="payment-settings-security-note-text">{text}</span>
        </div>
    );
}
