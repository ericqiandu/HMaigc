import { Alert, message, Spin } from "antd";
import { ArrowLeft, Coins, Gift, Zap } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";

import { createCreditTopupCheckout, createCreditTopupOrder, getCreditStorefront, type CreditProductCategory, type CreditTopupProduct } from "@/services/api/credit-store";

import "./credit-store.css";

const categories: Array<{ key: CreditProductCategory; label: string; icon: typeof Gift }> = [
    { key: "surprise", label: "惊喜专区", icon: Gift },
    { key: "general", label: "通用积分卡", icon: Zap },
];

const numberFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });

function formatCredits(value: number) {
    return numberFormatter.format(value / 1_000_000);
}

function formatMoney(value: number) {
    return numberFormatter.format(value / 100);
}

function membershipName(tier?: string) {
    return ({ origin: "基础版", pro: "标准版", max: "高级版", ultra: "至尊版" } as Record<string, string>)[tier ?? ""] ?? "会员";
}

function remainingTime(target: string | undefined, now: number) {
    if (!target) return null;
    const seconds = Math.max(0, Math.floor((new Date(target).getTime() - now) / 1000));
    return {
        days: Math.floor(seconds / 86400),
        hours: Math.floor((seconds % 86400) / 3600),
        minutes: Math.floor((seconds % 3600) / 60),
        seconds: seconds % 60,
    };
}

function ProductCard({ buying, onBuy, product }: { buying: boolean; onBuy: (product: CreditTopupProduct) => void; product: CreditTopupProduct }) {
    const total = product.baseMicrocredits + product.bonusMicrocredits;
	const effectivePrice = product.effectivePriceCents ?? product.priceCents;
    const soldOut = product.stockLimit >= 0 && product.soldCount >= product.stockLimit;
    return (
        <article className={`credit-store-card is-${product.category}`}>
            <div className="credit-store-card-heading">
                <div className="credit-store-card-title-group">
                    <span className="credit-store-card-scope">全模型通用</span>
                    <h3 className="credit-store-card-title">{product.name}</h3>
                </div>
                {product.badge ? <span className="credit-store-card-badge">{product.badge}</span> : null}
            </div>
            <div className="credit-store-card-credit">
                <Coins aria-hidden="true" className="credit-store-card-credit-icon" />
                <strong className="credit-store-card-credit-value">{formatCredits(total)}</strong>
                <span className="credit-store-card-credit-unit">积分</span>
            </div>
            {product.bonusMicrocredits > 0 ? (
                <p className="credit-store-card-grant">充 {formatCredits(product.baseMicrocredits)} + 赠 {formatCredits(product.bonusMicrocredits)}</p>
            ) : null}
            <div className="credit-store-card-price">
				<strong className="credit-store-card-price-current">¥{formatMoney(effectivePrice)}</strong>
				{product.originalPriceCents > effectivePrice ? <span className="credit-store-card-price-original">¥{formatMoney(product.originalPriceCents)}</span> : null}
            </div>
            <p className="credit-store-card-description">{product.description || "到账积分长期有效"}</p>
            <button className="credit-store-card-action" disabled={buying || soldOut} onClick={() => onBuy(product)} type="button">
                {soldOut ? "已售罄" : buying ? "正在创建订单…" : "立即购买"}
            </button>
            <p className="credit-store-card-requirement">{membershipName(product.requiredMembershipTier)}及以上会员可购买</p>
        </article>
    );
}

export default function CreditStorePage() {
    const navigate = useNavigate();
    const [products, setProducts] = useState<CreditTopupProduct[]>([]);
    const [serverOffset, setServerOffset] = useState(0);
    const [now, setNow] = useState(Date.now());
    const [active, setActive] = useState<CreditProductCategory>("surprise");
    const [buyingId, setBuyingId] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const result = await getCreditStorefront();
            setProducts(result.products);
            setServerOffset(new Date(result.serverNow).getTime() - Date.now());
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "积分超市加载失败");
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void load();
    }, [load]);

    useEffect(() => {
        const timer = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, []);

    const grouped = useMemo(() => new Map(categories.map(({ key }) => [key, products.filter((product) => product.category === key)])), [products]);
    const saleEnd = grouped.get("surprise")?.map((product) => product.saleEndsAt).find(Boolean);
    const countdown = remainingTime(saleEnd, now + serverOffset);

    const buy = async (product: CreditTopupProduct) => {
        setBuyingId(product.id);
        let orderNumber = "";
        try {
            const order = await createCreditTopupOrder(product.id, crypto.randomUUID());
            orderNumber = order.orderNumber;
            const checkout = await createCreditTopupCheckout(order.id);
            message.success(`订单 ${order.orderNumber} 已创建，正在进入收银台`);
            window.location.assign(checkout.checkoutUrl);
        } catch (reason) {
            const detail = reason instanceof Error ? reason.message : "积分订单创建失败";
            message.error(orderNumber ? `订单 ${orderNumber} 已创建，但收银台打开失败：${detail}` : detail);
        } finally {
            setBuyingId("");
        }
    };

    if (loading) return <div className="credit-store-loading"><Spin className="credit-store-loading-spinner" size="large" /></div>;

    return (
        <main className="credit-store-page">
            <header className="credit-store-header">
                <button aria-label="返回会员页面" className="credit-store-back" onClick={() => navigate("/membership")} type="button"><ArrowLeft aria-hidden="true" className="credit-store-back-icon" /></button>
                <h1 className="credit-store-page-title">积分超市</h1>
                <nav aria-label="积分超市分区" className="credit-store-tabs">
                    {categories.map(({ icon: Icon, key, label }) => (
                        <button className={`credit-store-tab${active === key ? " is-active" : ""}`} key={key} onClick={() => { setActive(key); document.getElementById(`credit-store-${key}`)?.scrollIntoView({ behavior: "smooth" }); }} type="button">
                            <Icon aria-hidden="true" className="credit-store-tab-icon" />
                            <span className="credit-store-tab-label">{label}</span>
                        </button>
                    ))}
                </nav>
            </header>
            {error ? <Alert className="credit-store-error" message="积分超市暂不可用" description={error} type="error" showIcon action={<button className="credit-store-retry" onClick={() => void load()} type="button">重新加载</button>} /> : null}
            {!error && products.length === 0 ? <Alert className="credit-store-empty" message="积分商品尚未上架" description="管理员完成套餐配置后，商品会在这里展示。" type="info" showIcon /> : null}
            {categories.map(({ key, label }) => {
                const items = grouped.get(key) ?? [];
                if (items.length === 0) return null;
                return (
                    <section className={`credit-store-section is-${key}`} id={`credit-store-${key}`} key={key}>
                        <div className="credit-store-section-heading">
                            <h2 className="credit-store-section-title">{label}</h2>
                            {key === "surprise" && countdown ? <p className="credit-store-countdown">限时限量，距本场结束 <strong className="credit-store-countdown-value">{countdown.days}天 {String(countdown.hours).padStart(2, "0")}:{String(countdown.minutes).padStart(2, "0")}:{String(countdown.seconds).padStart(2, "0")}</strong></p> : null}
                            {key === "general" ? <p className="credit-store-section-copy">全模型可用，到账与消费记录全程可追溯</p> : null}
                        </div>
                        <div className="credit-store-grid">{items.map((product) => <ProductCard buying={buyingId === product.id} key={product.id} onBuy={buy} product={product} />)}</div>
                    </section>
                );
            })}
        </main>
    );
}
