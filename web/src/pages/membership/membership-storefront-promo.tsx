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
        <section aria-label="限时会员活动" className="membership-storefront-promo">
            <div className="membership-storefront-promo-surface">
                <div className="membership-storefront-promo-content">
                    <div className="membership-storefront-promo-copy">
                        <h2 className="membership-storefront-promo-title">{promotion.title}</h2>
                        <p className="membership-storefront-promo-subtitle">
                            {promotion.subtitle}
                            <span className="membership-storefront-promo-highlight">{promotion.subtitleHighlight}</span>
                        </p>
                    </div>
                    <div aria-label="活动剩余时间" className="membership-storefront-countdown">
                        {cells.map((cell) => (
                            <span className="membership-storefront-countdown-cell" key={cell.key}>
                                <strong className="membership-storefront-countdown-value">{cell.value}</strong>
                                <small className="membership-storefront-countdown-label">{cell.label}</small>
                            </span>
                        ))}
                    </div>
                </div>
            </div>
        </section>
    );
}
