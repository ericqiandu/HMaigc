import { Alert, message, Spin } from "antd";
import { X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";

import { createCreditTopupCheckout, createCreditTopupOrder, getCreditStorefront, type CreditTopupProduct } from "@/services/api/credit-store";
import { STOREFRONT_EXIT_DESTINATION, shouldExitStorefront } from "@/lib/storefront-navigation";

import bannerImage from "./assets/banner-surprise.jpg";
import { GeneralCard, SurpriseCard } from "./credit-store-cards";
import "./credit-store.css";

type StoreSection = "surprise" | "general" | "model";

const sections: Array<{ key: StoreSection; label: string; icon: string }> = [
    { key: "surprise", label: "惊喜专区", icon: "🎁" },
    { key: "general", label: "通用积分卡", icon: "⚡" },
    { key: "model", label: "专属模型卡", icon: "🎲" },
];

function countdownParts(target: string | undefined, now: number) {
    const total = target ? Math.max(0, Math.floor((new Date(target).getTime() - now) / 1000)) : 0;
    return [
        { label: "天", value: Math.floor(total / 86400) },
        { label: "时", value: Math.floor((total % 86400) / 3600) },
        { label: "分", value: Math.floor((total % 3600) / 60) },
        { label: "秒", value: total % 60 },
    ];
}

export default function CreditStorePage() {
    const navigate = useNavigate();
    const pageRef = useRef<HTMLDivElement>(null);
    const [products, setProducts] = useState<CreditTopupProduct[]>([]);
    const [serverOffset, setServerOffset] = useState(0);
    const [now, setNow] = useState(Date.now());
    const [activeSection, setActiveSection] = useState<StoreSection>("surprise");
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
    useEffect(() => {
        const closeOnEscape = (event: KeyboardEvent) => {
            if (shouldExitStorefront(event.key, false)) navigate(STOREFRONT_EXIT_DESTINATION, { replace: true });
        };
        window.addEventListener("keydown", closeOnEscape);
        return () => window.removeEventListener("keydown", closeOnEscape);
    }, [navigate]);
    useEffect(() => {
        const page = pageRef.current;
        if (!page) return;
        const updateActiveSection = () => {
            let current: StoreSection = "surprise";
            for (const section of sections) {
                const element = document.getElementById(`points-${section.key}`);
                if (element && element.getBoundingClientRect().top <= 140) current = section.key;
            }
            setActiveSection(current);
        };
        page.addEventListener("scroll", updateActiveSection, { passive: true });
        updateActiveSection();
        return () => page.removeEventListener("scroll", updateActiveSection);
    }, [loading]);

    const surpriseProducts = useMemo(() => products.filter((product) => product.category === "surprise"), [products]);
    const generalProducts = useMemo(() => products.filter((product) => product.category === "general"), [products]);
    const saleEnd = surpriseProducts.map((product) => product.saleEndsAt).find(Boolean);
    const countdown = countdownParts(saleEnd, now + serverOffset);

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

    const scrollToSection = (section: StoreSection) => {
        document.getElementById(`points-${section}`)?.scrollIntoView({ behavior: "smooth" });
        setActiveSection(section);
    };

    return (
        <div className="points-market-page" ref={pageRef}>
            <header className="points-market-header">
                <nav aria-label="积分超市分区" className="points-market-tabs">
                    {sections.map((section) => (
                        <button className={`points-market-tab${activeSection === section.key ? " is-active" : ""}`} key={section.key} onClick={() => scrollToSection(section.key)} type="button">
                            <span aria-hidden="true" className="points-market-tab-icon">
                                {section.icon}
                            </span>
                            <span className="points-market-tab-label">{section.label}</span>
                        </button>
                    ))}
                </nav>
                <button aria-label="关闭积分超市并返回首页" className="points-market-close" onClick={() => navigate(STOREFRONT_EXIT_DESTINATION, { replace: true })} title="返回首页" type="button">
                    <X aria-hidden="true" className="points-market-close-icon" />
                </button>
            </header>
            <main className="points-market-main">
                {loading ? (
                    <div aria-busy="true" aria-label="正在加载积分商品" className="points-market-loading">
                        <Spin className="points-market-spinner" size="large" />
                    </div>
                ) : null}
                {error ? (
                    <Alert
                        action={
                            <button className="points-market-retry" onClick={() => void load()} type="button">
                                重新加载
                            </button>
                        }
                        className="points-market-error"
                        description={error}
                        message="积分超市暂不可用"
                        showIcon
                        type="error"
                    />
                ) : null}
                {!loading && !error ? (
                    <>
                        <section className="points-surprise-section" id="points-surprise">
                            <img alt="" className="points-surprise-background" src={bannerImage} />
                            <div className="points-surprise-overlay" />
                            <div className="points-surprise-content">
                                <h1 className="points-surprise-title">积分超市 惊喜专区</h1>
                                <div className="points-countdown">
                                    <span className="points-countdown-copy">{saleEnd ? "限时限量，距本场结束" : "限时商品以实际上架时间为准"}</span>
                                    {saleEnd
                                        ? countdown.map((part) => (
                                              <span className="points-countdown-cell" key={part.label}>
                                                  <strong className="points-countdown-value">{String(part.value).padStart(2, "0")}</strong>
                                                  <span className="points-countdown-label">{part.label}</span>
                                              </span>
                                          ))
                                        : null}
                                </div>
                                {surpriseProducts.length ? (
                                    <div className="points-surprise-grid">
                                        {surpriseProducts.map((product) => (
                                            <SurpriseCard buying={buyingId === product.id} key={product.id} onBuy={buy} product={product} />
                                        ))}
                                    </div>
                                ) : (
                                    <div className="points-section-empty points-surprise-empty" role="status">
                                        惊喜专区暂无上架商品
                                    </div>
                                )}
                            </div>
                        </section>
                        <section className="points-general-section" id="points-general">
                            <h2 className="points-section-title">通用积分卡</h2>
                            <p className="points-section-copy">全模型可用 · 到账积分长期有效</p>
                            {generalProducts.length ? (
                                <div className="points-general-grid">
                                    {generalProducts.map((product) => (
                                        <GeneralCard buying={buyingId === product.id} key={product.id} onBuy={buy} product={product} />
                                    ))}
                                </div>
                            ) : (
                                <div className="points-section-empty" role="status">
                                    通用积分卡暂无上架商品
                                </div>
                            )}
                        </section>
                        <section className="points-model-section" id="points-model">
                            <h2 className="points-section-title">专属模型卡</h2>
                            <p className="points-section-copy">指定模型使用更划算 · 套餐正在准备中</p>
                            <div className="points-model-empty" role="status">
                                专属模型套餐尚未上架
                            </div>
                        </section>
                    </>
                ) : null}
            </main>
        </div>
    );
}
