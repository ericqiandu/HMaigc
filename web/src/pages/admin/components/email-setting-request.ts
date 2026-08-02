import type { EmailSetting, EmailSettingUpdateRequest } from "@/services/api/wallet";

export type EmailSettingFormValues = Omit<
    EmailSettingUpdateRequest,
    "port"
> & {
    port: number | string;
};

const parseSmtpPort = (value: number | string): number => {
    const port = typeof value === "number" ? value : Number(value.trim());
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
        throw new Error("SMTP 端口必须是 1 到 65535 的整数");
    }
    return port;
};

export const buildEmailSettingRequest = (
    values: EmailSettingFormValues,
): EmailSettingUpdateRequest => ({
    enabled: values.enabled,
    host: values.host.trim(),
    port: parseSmtpPort(values.port),
    username: values.username.trim(),
    password: values.password.trim(),
    encryption: values.encryption,
    fromEmail: values.fromEmail.trim(),
    fromName: values.fromName.trim(),
});

export const emailSettingToFormValues = (
    setting: EmailSetting,
): EmailSettingFormValues => ({
    enabled: setting.enabled,
    host: setting.host,
    port: setting.port,
    username: setting.username,
    password: "",
    encryption: setting.encryption,
    fromEmail: setting.fromEmail,
    fromName: setting.fromName,
});

export const emailSettingValuesEqual = (
    values: Partial<EmailSettingFormValues>,
    setting: EmailSetting,
): boolean => {
    return (
        (values.enabled === true) === setting.enabled &&
        clean(values.host) === clean(setting.host) &&
        parseComparablePort(values.port) === setting.port &&
        clean(values.username) === clean(setting.username) &&
        clean(values.password) === "" &&
        values.encryption === setting.encryption &&
        clean(values.fromEmail) === clean(setting.fromEmail) &&
        clean(values.fromName) === clean(setting.fromName)
    );
};

const clean = (value: string | undefined): string => value?.trim() || "";

const parseComparablePort = (value: number | string | undefined): number | null => {
    if (value === undefined || value === "") return null;
    const parsed = typeof value === "number" ? value : Number(value.trim());
    return Number.isInteger(parsed) ? parsed : null;
};
