import { useEffect, useMemo, useState } from "react";

import type { MembershipStorefrontPromotion } from "@/services/api/membership";

import { countdownParts } from "./membership-storefront-domain";

type MembershipStorefrontPromoProps = {
    promotion: MembershipStorefrontPromotion;
    serverNow: string;
};

export function MembershipStorefrontPromo({ promotion, serverNow }: MembershipStorefrontPromoProps) {
    const [clientStartedAt] = useState(() => Date.now());
    const [clientNow, setClientNow] = useState(clientStartedAt);

    useEffect(() => {
        if (!promotion.enabled) return undefined;
        const timer = window.setInterval(() => setClientNow(Date.now()), 1_000);
        return () => window.clearInterval(timer);
    }, [promotion.enabled]);

    const cells = useMemo(() => countdownParts(promotion.endsAt, serverNow, clientStartedAt, clientNow), [clientNow, clientStartedAt, promotion.endsAt, serverNow]);
    if (!promotion.enabled) return null;

    return (
        <section aria-label="限时会员活动" className="membership-storefront-promo mx-auto max-w-[1300px] px-6 pt-6">
            <div className="membership-storefront-promo-surface relative overflow-hidden rounded-xl border border-[#26303e] bg-gradient-to-r from-[#0b1017] via-[#101826] to-[#1a1220]">
                <div aria-hidden="true" className="membership-storefront-promo-lights pointer-events-none absolute inset-0">
                    <div className="membership-storefront-promo-blue-light absolute right-[8%] top-[-60%] h-[220%] w-[45%] rotate-[18deg] bg-gradient-to-b from-transparent via-[#3a5a8c]/40 to-transparent blur-2xl" />
                    <div className="membership-storefront-promo-red-light absolute right-[20%] top-[-40%] h-[180%] w-[16%] rotate-[18deg] bg-gradient-to-b from-transparent via-[#e0657a]/30 to-transparent blur-xl" />
                    <div className="membership-storefront-promo-cyan-light absolute right-[32%] top-[-30%] h-[160%] w-[10%] rotate-[18deg] bg-gradient-to-b from-transparent via-[#6fd3e0]/25 to-transparent blur-lg" />
                    <div className="membership-storefront-promo-warm-light absolute left-[38%] top-[30%] h-[60%] w-[22%] bg-[#c86a3a]/20 blur-3xl" />
                </div>
                <div className="membership-storefront-promo-content relative flex items-center justify-between gap-8 px-10 py-7 max-md:flex-col max-md:items-start max-md:px-5">
                    <div className="membership-storefront-promo-copy">
                        <h1 className="membership-storefront-promo-title text-[22px] font-bold leading-snug text-white">{promotion.title}</h1>
                        <p className="membership-storefront-promo-subtitle mt-2 text-[20px] font-bold text-white">
                            {promotion.subtitle}
                            <span className="membership-storefront-promo-highlight text-[#f5c16c]">{promotion.subtitleHighlight}</span>
                        </p>
                    </div>
                    <div aria-label="活动剩余时间" className="membership-storefront-countdown flex shrink-0 items-start gap-6 max-sm:w-full max-sm:justify-between max-sm:gap-2">
                        {cells.map((cell) => (
                            <span className="membership-storefront-countdown-cell flex flex-col items-center" key={cell.key}>
                                <strong className="membership-storefront-countdown-value font-mono text-[40px] font-bold leading-none tracking-wider text-white max-sm:text-[30px]">{cell.value}</strong>
                                <small className="membership-storefront-countdown-label mt-1.5 text-sm text-[#8b95a5]">{cell.label}</small>
                            </span>
                        ))}
                    </div>
                </div>
            </div>
        </section>
    );
}
