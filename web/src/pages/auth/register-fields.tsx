import { Button, Input } from "antd";
import { LockKeyhole, Mail, ShieldCheck, UserRound } from "lucide-react";

import { AuthField } from "./auth-components";

type RegisterFieldsProps = {
    username: string;
    displayName: string;
    email: string;
    emailCode: string;
    password: string;
    confirmPassword: string;
    requireEmail: boolean;
    requireCode: boolean;
    disabled: boolean;
    sendingCode: boolean;
    countdown: number;
    onUsernameChange: (value: string) => void;
    onDisplayNameChange: (value: string) => void;
    onEmailChange: (value: string) => void;
    onEmailCodeChange: (value: string) => void;
    onPasswordChange: (value: string) => void;
    onConfirmPasswordChange: (value: string) => void;
    onSendCode: () => void;
};

export function RegisterFields({
    username,
    displayName,
    email,
    emailCode,
    password,
    confirmPassword,
    requireEmail,
    requireCode,
    disabled,
    sendingCode,
    countdown,
    onUsernameChange,
    onDisplayNameChange,
    onEmailChange,
    onEmailCodeChange,
    onPasswordChange,
    onConfirmPasswordChange,
    onSendCode,
}: RegisterFieldsProps) {
    return (
        <div className="auth-fields">
            <div className="auth-field-grid">
                <AuthField label="用户名">
                    <Input className="auth-input" prefix={<UserRound className="auth-input-icon" />} value={username} onChange={(event) => onUsernameChange(event.target.value)} placeholder="用户名" autoComplete="username" required disabled={disabled} />
                </AuthField>
                <AuthField label="显示名称">
                    <Input className="auth-input" value={displayName} onChange={(event) => onDisplayNameChange(event.target.value)} placeholder="显示名称（选填）" disabled={disabled} />
                </AuthField>
            </div>

            <AuthField label="邮箱">
                <Input className="auth-input" prefix={<Mail className="auth-input-icon" />} value={email} onChange={(event) => onEmailChange(event.target.value)} placeholder="邮箱地址" autoComplete="email" required={requireEmail} disabled={disabled} />
            </AuthField>

            {requireCode ? (
                <AuthField label="邮箱验证码">
                    <div className="auth-code-field">
                        <Input
                            className="auth-input auth-code-input"
                            prefix={<ShieldCheck className="auth-input-icon" />}
                            value={emailCode}
                            onChange={(event) => onEmailCodeChange(event.target.value.replace(/\D/g, "").slice(0, 6))}
                            placeholder="6 位验证码"
                            inputMode="numeric"
                            autoComplete="one-time-code"
                            required
                            disabled={disabled}
                        />
                        <Button className="auth-code-button" loading={sendingCode} disabled={disabled || countdown > 0} onClick={onSendCode}>
                            {countdown > 0 ? `${countdown}s` : "获取验证码"}
                        </Button>
                    </div>
                </AuthField>
            ) : null}

            <div className="auth-field-grid">
                <AuthField label="密码">
                    <Input.Password
                        className="auth-input"
                        prefix={<LockKeyhole className="auth-input-icon" />}
                        value={password}
                        onChange={(event) => onPasswordChange(event.target.value)}
                        placeholder="设置密码"
                        autoComplete="new-password"
                        required
                        disabled={disabled}
                    />
                </AuthField>
                <AuthField label="确认密码">
                    <Input.Password
                        className="auth-input"
                        prefix={<LockKeyhole className="auth-input-icon" />}
                        value={confirmPassword}
                        onChange={(event) => onConfirmPasswordChange(event.target.value)}
                        placeholder="再次输入密码"
                        autoComplete="new-password"
                        required
                        disabled={disabled}
                    />
                </AuthField>
            </div>
        </div>
    );
}
