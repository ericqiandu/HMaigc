import { Fragment } from "react";
import { Link } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";

export function UpdreamFooter() {
    const { settings } = useSiteSettings();
    const registrationItems = [
        { number: settings.icpRegistrationNumber.trim(), url: settings.icpRegistrationUrl.trim() },
        { number: settings.publicSecurityRegistrationNumber.trim(), url: settings.publicSecurityRegistrationUrl.trim() },
    ].filter((item) => item.number);

    return (
        <footer className="updream-footer pb-10 text-center text-[12px] leading-7 text-white/35">
            <p className="updream-footer-legal-links">
                <Link to="/legal/user-agreement" className="updream-footer-link transition-colors hover:text-white/70">用户协议</Link>
                <span className="updream-footer-divider mx-3 text-white/20">|</span>
                <Link to="/legal/privacy-policy" className="updream-footer-link transition-colors hover:text-white/70">隐私政策</Link>
            </p>
            {settings.footerCopyright ? <p className="updream-footer-copyright">{settings.footerCopyright}</p> : null}
            {registrationItems.length > 0 ? (
                <p className="updream-footer-registration">
                    {registrationItems.map((item, index) => (
                        <Fragment key={item.number}>
                            {index > 0 ? <span className="updream-footer-registration-divider mx-3 text-white/20">|</span> : null}
                            {item.url ? (
                                <a className="updream-footer-registration-link transition-colors hover:text-white/70" href={item.url} target="_blank" rel="noreferrer">
                                    {item.number}
                                </a>
                            ) : (
                                <span className="updream-footer-registration-text">{item.number}</span>
                            )}
                        </Fragment>
                    ))}
                </p>
            ) : null}
        </footer>
    );
}
