import type { CreditTopupProduct } from "@/services/api/credit-store";

const numberFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });

function formatCredits(value: number) {
    return numberFormatter.format(value / 1_000_000);
}

function formatMoney(value: number) {
    return `¥${numberFormatter.format(value / 100)}`;
}

function membershipName(tier?: string) {
    return ({ origin: "基础版", pro: "标准版", max: "高级版", ultra: "至尊版" } as Record<string, string>)[tier ?? ""] ?? "会员";
}

type ProductCardProps = {
    buying: boolean;
    onBuy: (product: CreditTopupProduct) => void;
    product: CreditTopupProduct;
};

function ProductAction({ buying, className, onBuy, product }: ProductCardProps & { className: string }) {
    const soldOut = product.stockLimit >= 0 && product.soldCount >= product.stockLimit;
    return <button className={className} disabled={buying || soldOut} onClick={() => onBuy(product)} type="button">{soldOut ? "已售罄" : buying ? "正在创建订单…" : "立即购买"}</button>;
}

export function SurpriseCard({ buying, onBuy, product }: ProductCardProps) {
    const effectivePrice = product.effectivePriceCents ?? product.priceCents;
    const remaining = product.stockLimit >= 0 ? product.stockLimit - product.soldCount : null;
    return (
        <article className="points-surprise-card">
            <div className="points-card-heading"><h3 className="points-card-name">{product.name}</h3>{product.badge ? <span className="points-surprise-badge">{product.badge}</span> : null}</div>
            <div className="points-credit-line"><span aria-hidden="true" className="points-credit-icon">⚡</span><strong className="points-surprise-credit">{formatCredits(product.baseMicrocredits + product.bonusMicrocredits)}</strong></div>
            <p className="points-membership-limit"><span aria-hidden="true" className="points-membership-diamond">◈</span>{membershipName(product.requiredMembershipTier)}及以上会员专享</p>
            <div className="points-price-line"><strong className="points-current-price">{formatMoney(effectivePrice)}</strong>{product.originalPriceCents > effectivePrice ? <span className="points-original-price">{formatMoney(product.originalPriceCents)}</span> : null}</div>
            <ProductAction buying={buying} className="points-surprise-action" onBuy={onBuy} product={product} />
            <p className="points-card-note">{remaining !== null ? `剩余 ${Math.max(0, remaining)} 份 · ` : ""}{product.description || "到账积分长期有效"}</p>
        </article>
    );
}

export function GeneralCard({ buying, onBuy, product }: ProductCardProps) {
    const effectivePrice = product.effectivePriceCents ?? product.priceCents;
    const highlighted = product.bonusMicrocredits >= product.baseMicrocredits && product.bonusMicrocredits > 0;
    return (
        <article className={`points-general-card${highlighted ? " is-highlighted" : ""}`}>
            <div className="points-card-heading"><h3 className="points-card-name">{product.name}</h3>{product.badge ? <span className={`points-general-badge${highlighted ? " is-filled" : ""}`}>{product.badge}</span> : null}</div>
            <div className="points-credit-line"><span aria-hidden="true" className="points-credit-icon">⚡</span><strong className="points-general-credit">{formatCredits(product.baseMicrocredits + product.bonusMicrocredits)}</strong></div>
            {product.bonusMicrocredits > 0 ? <p className="points-grant-copy">充 {formatCredits(product.baseMicrocredits)} + <span className="points-grant-bonus">赠 {formatCredits(product.bonusMicrocredits)}</span></p> : null}
            <div className="points-general-price"><strong className="points-current-price">{formatMoney(effectivePrice)}</strong>{product.originalPriceCents > effectivePrice ? <span className="points-saving-copy">较原价立省 <strong className="points-saving-value">{numberFormatter.format((product.originalPriceCents - effectivePrice) / 100)}</strong> 元</span> : null}</div>
            <ProductAction buying={buying} className={`points-general-action${highlighted ? " is-highlighted" : ""}`} onBuy={onBuy} product={product} />
            <p className="points-card-note">{membershipName(product.requiredMembershipTier)}及以上会员专享</p>
        </article>
    );
}
