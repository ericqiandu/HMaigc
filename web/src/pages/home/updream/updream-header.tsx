import { SiteBrandLink } from "@/components/layout/site-brand-link";
import { UpdreamAccountActions } from "@/pages/home/updream/updream-account-actions";
import "@/pages/home/updream/updream-sticky-header.css";

export function UpdreamHeader() {
    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <SiteBrandLink />
            <UpdreamAccountActions />
        </header>
    );
}
