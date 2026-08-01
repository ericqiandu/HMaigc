import { CircleAlert, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import { Checkbox } from "antd";
import { type ReactNode, useId } from "react";
import { Link } from "react-router";

export function LinuxDOIcon() {
    return (
        <span className="auth-linuxdo-icon" aria-hidden>
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-dark" />
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-light" />
            <span className="auth-linuxdo-icon-band auth-linuxdo-icon-band-gold" />
        </span>
    );
}

export function AuthField({ label, children }: { label: string; children: ReactNode }) {
    return (
        <label className="auth-field">
            <span className="auth-field-label">{label}</span>
            <div className="auth-field-control">{children}</div>
        </label>
    );
}

export function AuthLegalConsent({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
    const labelId = useId();
    return (
        <div className="auth-legal-consent">
            <Checkbox className="auth-legal-checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} aria-labelledby={labelId} />
            <span className="auth-legal-consent-copy" id={labelId}>
                我已阅读并同意
                <Link className="auth-legal-link" to="/legal/user-agreement" target="_blank" rel="noreferrer">
                    《用户协议》
                </Link>
                和
                <Link className="auth-legal-link" to="/legal/privacy-policy" target="_blank" rel="noreferrer">
                    《隐私政策》
                </Link>
            </span>
        </div>
    );
}

const noticeIcons: Record<"error" | "info" | "warning", LucideIcon> = {
    error: CircleAlert,
    info: Info,
    warning: TriangleAlert,
};

export function AuthNotice({ tone, children }: { tone: "error" | "info" | "warning"; children: ReactNode }) {
    const Icon = noticeIcons[tone];
    return (
        <div className={`auth-notice auth-notice-${tone}`} role={tone === "error" ? "alert" : "status"}>
            <Icon className="auth-notice-icon" />
            <span className="auth-notice-text">{children}</span>
        </div>
    );
}
