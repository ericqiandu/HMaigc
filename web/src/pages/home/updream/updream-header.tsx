import { SiteBrandLink } from "@/components/layout/site-brand-link";
import { SiteAccountActions } from "@/components/layout/site-account-actions";
import "@/pages/home/updream/updream-sticky-header.css";

export function UpdreamHeader() {
    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <SiteBrandLink />
            <SiteAccountActions />
        </header>
    );
}
