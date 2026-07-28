import { Link } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";

export function UpdreamFooter() {
    const { settings } = useSiteSettings();

    return (
        <footer className="updream-footer pb-10 text-center text-[12px] leading-7 text-white/35">
            <p className="updream-footer-legal-links">
                <Link to="/legal/user-agreement" className="updream-footer-link transition-colors hover:text-white/70">用户协议</Link>
                <span className="updream-footer-divider mx-3 text-white/20">|</span>
                <Link to="/legal/privacy-policy" className="updream-footer-link transition-colors hover:text-white/70">隐私政策</Link>
            </p>
            {settings.footerCopyright ? <p className="updream-footer-copyright">{settings.footerCopyright}</p> : null}
        </footer>
    );
}
