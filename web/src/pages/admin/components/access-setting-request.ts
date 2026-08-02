import type { LinuxDOSetting } from "@/services/api/wallet";

export type LinuxDOFormValues = Omit<LinuxDOSetting, "hasClientSecret" | "updatedAt">;

const clean = (value: string | undefined): string => value?.trim() || "";

const normalizeScopes = (scopes: string[] | undefined): string[] =>
    Array.from(new Set((scopes || []).map((scope) => scope.trim()).filter(Boolean))).sort();

export const linuxDOSettingToFormValues = (setting: LinuxDOSetting): LinuxDOFormValues => ({
    enabled: setting.enabled,
    clientId: setting.clientId,
    clientSecret: "",
    authorizationUrl: setting.authorizationUrl,
    tokenUrl: setting.tokenUrl,
    userInfoUrl: setting.userInfoUrl,
    redirectUrl: setting.redirectUrl,
    scopes: [...setting.scopes],
    clientAuthMethod: setting.clientAuthMethod,
    subjectField: setting.subjectField,
    usernameField: setting.usernameField,
    displayNameField: setting.displayNameField,
    emailField: setting.emailField,
    avatarField: setting.avatarField,
});

export const buildLinuxDOSettingRequest = (values: LinuxDOFormValues): LinuxDOFormValues => ({
    enabled: values.enabled,
    clientId: clean(values.clientId),
    clientSecret: clean(values.clientSecret),
    authorizationUrl: clean(values.authorizationUrl),
    tokenUrl: clean(values.tokenUrl),
    userInfoUrl: clean(values.userInfoUrl),
    redirectUrl: clean(values.redirectUrl),
    scopes: normalizeScopes(values.scopes),
    clientAuthMethod: values.clientAuthMethod,
    subjectField: clean(values.subjectField),
    usernameField: clean(values.usernameField),
    displayNameField: clean(values.displayNameField),
    emailField: clean(values.emailField),
    avatarField: clean(values.avatarField),
});

export const linuxDOSettingValuesEqual = (
    values: Partial<LinuxDOFormValues>,
    setting: LinuxDOSetting,
): boolean => {
    const currentScopes = normalizeScopes(values.scopes);
    const savedScopes = normalizeScopes(setting.scopes);
    return (
        (values.enabled === true) === setting.enabled &&
        clean(values.clientId) === clean(setting.clientId) &&
        clean(values.clientSecret) === "" &&
        clean(values.authorizationUrl) === clean(setting.authorizationUrl) &&
        clean(values.tokenUrl) === clean(setting.tokenUrl) &&
        clean(values.userInfoUrl) === clean(setting.userInfoUrl) &&
        clean(values.redirectUrl) === clean(setting.redirectUrl) &&
        currentScopes.length === savedScopes.length &&
        currentScopes.every((scope, index) => scope === savedScopes[index]) &&
        values.clientAuthMethod === setting.clientAuthMethod &&
        clean(values.subjectField) === clean(setting.subjectField) &&
        clean(values.usernameField) === clean(setting.usernameField) &&
        clean(values.displayNameField) === clean(setting.displayNameField) &&
        clean(values.emailField) === clean(setting.emailField) &&
        clean(values.avatarField) === clean(setting.avatarField)
    );
};
