import { Link } from "react-router";

import { UpdreamAccountActions } from "@/pages/home/updream/updream-account-actions";

export function UpdreamHeader() {
    return (
        <header className="updream-header flex h-[72px] items-center justify-between px-5 sm:px-8">
            <Link to="/" className="updream-header-logo-link flex items-center" aria-label="HMaigc 首页">
                <span className="updream-header-logo text-[22px] font-bold tracking-[-0.04em] text-white">
                    HMaigc
                </span>
            </Link>
            <UpdreamAccountActions />
        </header>
    );
}
