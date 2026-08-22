import { SiteBrandLink } from "@/components/layout/site-brand-link";
import { SiteAccountActions } from "@/components/layout/site-account-actions";
import "@/pages/home/updream/updream-sticky-header.css";

export function UpdreamHeader() {
    return (
        <header className="updream-header">
            <SiteBrandLink />
            <SiteAccountActions />
        </header>
    );
}
