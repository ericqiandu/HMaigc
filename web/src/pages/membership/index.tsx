import { Alert, Button, Empty, message, Segmented, Spin } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowRight, ChevronLeft, Crown, ImageIcon, Video, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import { membershipQueryKey } from "@/hooks/use-membership-action";
import { cancelMembershipOrder, createMembershipOrder, createTeam, getMyMembership, listMembershipPlans, type MembershipAudience, type MembershipBillingCycle, type MembershipOverview, type MembershipPlan } from "@/services/api/membership";
import { createPaymentCheckout } from "@/services/api/payment";
import { useUserStore } from "@/stores/use-user-store";

import { billingCycleShortLabel, clampSeats, publicPlanName, topupDiscountLabel } from "./membership-formatters";
import { MembershipOrderHistory } from "./membership-order-history";
import { MembershipInvoiceCenter } from "./membership-invoice-center";
import { MembershipPlanCard } from "./membership-plan-card";
import { MembershipPurchaseModal } from "./membership-purchase-modal";
import "./membership.css";
import "./membership-plan-card.css";
import "./membership-order.css";
import "./membership-responsive.css";

type PurchaseSelection = {
    plan: MembershipPlan;
    seats: number;
};

const paidCycleOrder: MembershipBillingCycle[] = ["year", "month"];

export default function MembershipPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const [searchParams] = useSearchParams();
    const requestedAudience = searchParams.get("audience") === "team" ? "team" : "personal";
    const requestedTeamId = searchParams.get("teamId") || undefined;
    const user = useUserStore((state) => state.user);
    const { settings } = useSiteSettings();
    const carouselRef = useRef<HTMLDivElement>(null);
    const [plans, setPlans] = useState<MembershipPlan[]>([]);
    const [overview, setOverview] = useState<MembershipOverview | null>(null);
    const [audience, setAudience] = useState<MembershipAudience>(requestedAudience);
    const [cycle, setCycle] = useState<MembershipBillingCycle>("year");
    const [teamSeats, setTeamSeats] = useState<Record<string, number>>({});
    const [selection, setSelection] = useState<PurchaseSelection | null>(null);
    const [teamId, setTeamId] = useState<string>();
    const [teamName, setTeamName] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [payingId, setPayingId] = useState("");
    const [cancellingId, setCancellingId] = useState("");
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [canScrollLeft, setCanScrollLeft] = useState(false);
    const [canScrollRight, setCanScrollRight] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const [nextPlans, nextOverview] = await Promise.all([listMembershipPlans(), user ? getMyMembership() : Promise.resolve(null)]);
            setPlans(nextPlans);
            setOverview(nextOverview);
            if (user && nextOverview) {
                queryClient.setQueryData(membershipQueryKey(user.id), nextOverview);
            }
            setTeamSeats((current) => {
                const next = { ...current };
                nextPlans
                    .filter((plan) => plan.audience === "team")
                    .forEach((plan) => {
                        next[plan.id] = clampSeats(plan, current[plan.id] ?? plan.minSeats);
                    });
                return next;
            });
        } catch (error) {
            const reason = error instanceof Error ? error.message : "会员数据加载失败";
            setLoadError(reason);
            message.error(reason);
        } finally {
            setLoading(false);
        }
    }, [queryClient, user]);

    useEffect(() => {
        void load();
    }, [load]);

    useEffect(() => {
        if (!requestedTeamId || !overview?.teams.some((team) => team.id === requestedTeamId)) return;
        setTeamId(requestedTeamId);
    }, [overview, requestedTeamId]);

    useEffect(() => {
        const closeOnEscape = (event: KeyboardEvent) => {
            if (event.key === "Escape" && !selection) navigate(-1);
        };
        window.addEventListener("keydown", closeOnEscape);
        return () => window.removeEventListener("keydown", closeOnEscape);
    }, [navigate, selection]);

    const availableCycles = useMemo(() => paidCycleOrder.filter((candidate) => plans.some((plan) => plan.audience === audience && plan.billingCycle === candidate)), [audience, plans]);

    useEffect(() => {
        if (availableCycles.length > 0 && !availableCycles.includes(cycle)) {
            setCycle(availableCycles[0]);
        }
    }, [availableCycles, cycle]);

    const visiblePlans = useMemo(
        () =>
            plans
                .filter((plan) => {
                    if (plan.audience !== audience) return false;
                    return plan.billingCycle === cycle;
                })
                .sort((left, right) => left.sortOrder - right.sortOrder),
        [audience, cycle, plans],
    );

    const plansById = useMemo(() => new Map(plans.map((plan) => [plan.id, plan])), [plans]);

    const updateCarouselState = useCallback(() => {
        const carousel = carouselRef.current;
        if (!carousel) return;
        setCanScrollLeft(carousel.scrollLeft > 4);
        setCanScrollRight(carousel.scrollLeft + carousel.clientWidth < carousel.scrollWidth - 4);
    }, []);

    useEffect(() => {
        const carousel = carouselRef.current;
        if (!carousel) return;
        updateCarouselState();
        const observer = new ResizeObserver(updateCarouselState);
        observer.observe(carousel);
        return () => observer.disconnect();
    }, [updateCarouselState, visiblePlans.length]);

    const scrollPlans = (direction: -1 | 1) => {
        const carousel = carouselRef.current;
        if (!carousel) return;
        carousel.scrollBy({ behavior: "smooth", left: direction * Math.max(280, carousel.clientWidth * 0.78) });
    };

    const selectAudience = (nextAudience: MembershipAudience) => {
        setAudience(nextAudience);
        carouselRef.current?.scrollTo({ left: 0 });
    };

    const beginPurchase = (plan: MembershipPlan, seats: number) => {
        if (!user) {
            navigate("/login?next=%2Fmembership");
            return;
        }
        const normalizedSeats = clampSeats(plan, seats);
        setSelection({ plan, seats: normalizedSeats });
        setTeamId(requestedTeamId && overview?.teams.some((team) => team.id === requestedTeamId) ? requestedTeamId : overview?.teams[0]?.id);
        setTeamName("");
    };

    const submitOrder = async () => {
        if (!selection) return;
        setSubmitting(true);
        let createdOrderId = "";
        try {
            let resolvedTeamId = teamId;
            if (selection.plan.audience === "team" && !resolvedTeamId) {
                if (!teamName.trim()) throw new Error("请输入团队名称");
                const team = await createTeam(teamName.trim());
                resolvedTeamId = team.id;
            }
            const order = await createMembershipOrder({
                planId: selection.plan.id,
                seats: selection.plan.audience === "team" ? selection.seats : 1,
                teamId: resolvedTeamId,
            });
            createdOrderId = order.id;
            setSelection(null);
            const checkout = await createPaymentCheckout(order.id);
            message.success(`订单 ${order.orderNumber} 已创建，正在进入收银台`);
            window.location.assign(checkout.checkoutUrl);
        } catch (error) {
            const reason = error instanceof Error ? error.message : "创建订单失败";
            if (createdOrderId) {
                message.error(`订单已创建，但收银台打开失败：${reason}。请在会员订单中继续支付`);
                await load();
            } else {
                message.error(reason);
            }
        } finally {
            setSubmitting(false);
        }
    };

    const openCheckout = async (orderId: string) => {
        setPayingId(orderId);
        try {
            const checkout = await createPaymentCheckout(orderId);
            window.location.assign(checkout.checkoutUrl);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "收银台打开失败");
        } finally {
            setPayingId("");
        }
    };

    const cancelOrder = async (orderId: string) => {
        setCancellingId(orderId);
        try {
            await cancelMembershipOrder(orderId);
            message.success("待支付订单已关闭");
            await load();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "关闭订单失败");
        } finally {
            setCancellingId("");
        }
    };

    return (
        <main className="membership-page">
            <div aria-hidden="true" className="membership-backdrop" />
            <section aria-labelledby="membership-window-title" aria-modal="true" className="membership-window" role="dialog">
                <button aria-label="关闭会员中心" className="membership-window-close" onClick={() => navigate(-1)} type="button">
                    <X className="membership-window-close-icon" />
                </button>

                <header className="membership-window-header">
                    <div className="membership-audience-tabs" role="tablist" aria-label="会员类型">
                        <button aria-selected={audience === "personal"} className={`membership-audience-tab ${audience === "personal" ? "is-active" : ""}`} onClick={() => selectAudience("personal")} role="tab" type="button">
                            创作会员
                        </button>
                        <button aria-selected={audience === "team"} className={`membership-audience-tab ${audience === "team" ? "is-active" : ""}`} onClick={() => selectAudience("team")} role="tab" type="button">
                            团队版会员
                        </button>
                    </div>
                    <h1 className="membership-page-title sr-only" id="membership-window-title">
                        {settings.siteName} {audience === "team" ? "团队版会员方案" : "创作会员方案"}
                    </h1>
                </header>

                <div className="membership-window-scroll">
                    <section aria-label="会员礼遇" className="membership-campaign">
                        <div className="membership-campaign-copy">
                            <span className="membership-campaign-kicker">HMAIGC MEMBERSHIP</span>
                            <strong className="membership-campaign-title">为持续创作释放更多算力</strong>
                            <span className="membership-campaign-description">套餐价格、积分与并发权益均以后台实时配置为准</span>
                        </div>
                        <div aria-hidden="true" className="membership-campaign-mark"><Crown /></div>
                    </section>
                    <section className="membership-hero">
                        {availableCycles.length > 0 ? (
                            <Segmented
                                className="membership-cycle-switch"
                                onChange={(value) => setCycle(value as MembershipBillingCycle)}
                                options={availableCycles.map((availableCycle) => ({
                                    label: audience === "personal"
                                        ? availableCycle === "year" ? "连续包年" : availableCycle === "month" ? "连续包月" : billingCycleShortLabel[availableCycle]
                                        : availableCycle === "year" ? "按年购买" : availableCycle === "month" ? "按月购买" : billingCycleShortLabel[availableCycle],
                                    value: availableCycle,
                                }))}
                                value={cycle}
                            />
                        ) : null}
                    </section>

                    <section className="membership-plans-section">
                        <button aria-label="查看上一组套餐" className="membership-carousel-button membership-carousel-button-left" disabled={!canScrollLeft} onClick={() => scrollPlans(-1)} type="button">
                            <ChevronLeft className="membership-carousel-icon" />
                        </button>
                        <button aria-label="查看下一组套餐" className="membership-carousel-button membership-carousel-button-right" disabled={!canScrollRight} onClick={() => scrollPlans(1)} type="button">
                            <ArrowRight className="membership-carousel-icon" />
                        </button>

                        {loadError ? (
                            <Alert
                                action={
                                    <Button className="membership-retry-button" onClick={() => void load()} size="small">
                                        重新加载
                                    </Button>
                                }
                                className="membership-load-error"
                                description={loadError}
                                message="会员套餐加载失败"
                                showIcon
                                type="error"
                            />
                        ) : loading ? (
                            <div className="membership-loading">
                                <Spin className="membership-loading-spinner" />
                            </div>
                        ) : visiblePlans.length === 0 ? (
                            <Empty className="membership-empty" description="后台暂无当前类型与周期的上架套餐" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                        ) : (
                            <div className={`membership-plan-carousel ${audience === "team" ? "is-team" : ""}`} onScroll={updateCarouselState} ref={carouselRef}>
                                {visiblePlans.map((plan) => (
                                    <MembershipPlanCard
                                        className="membership-plan-carousel-item"
                                        currentPlanId={overview?.entitlement.planId}
                                        featured={plan.tier === "max"}
                                        key={plan.id}
                                        onPurchase={beginPurchase}
                                        onSeatsChange={(planId, seats) => setTeamSeats((current) => ({ ...current, [planId]: clampSeats(plan, seats) }))}
                                        plan={plan}
                                        seats={teamSeats[plan.id] ?? plan.minSeats}
                                    />
                                ))}
                            </div>
                        )}
                    </section>

                    {overview ? (
                        <section aria-label="当前会员权益" className="membership-account-panel">
                            <div className="membership-overview">
                                <div className="membership-overview-heading">
                                    <span className="membership-overview-icon">
                                        <Crown className="membership-overview-icon-svg" />
                                    </span>
                                    <div className="membership-overview-title">
                                        <span className="membership-overview-label">当前方案</span>
                                        <strong className="membership-overview-plan">{publicPlanName({ name: overview.entitlement.planName, tier: overview.entitlement.tier })}</strong>
                                    </div>
                                </div>
                                <div className="membership-overview-metrics">
                                    <span className="membership-overview-metric">
                                        <small className="membership-overview-metric-label">有效期</small>
                                        <strong className="membership-overview-metric-value">{overview.entitlement.expiresAt ? new Date(overview.entitlement.expiresAt).toLocaleDateString("zh-CN") : "长期有效"}</strong>
                                    </span>
                                    <span className="membership-overview-metric">
                                        <ImageIcon className="membership-overview-metric-icon" />
                                        <small className="membership-overview-metric-label">图片并发</small>
                                        <strong className="membership-overview-metric-value">{overview.entitlement.imageConcurrency}</strong>
                                    </span>
                                    <span className="membership-overview-metric">
                                        <Video className="membership-overview-metric-icon" />
                                        <small className="membership-overview-metric-label">视频并发</small>
                                        <strong className="membership-overview-metric-value">{overview.entitlement.videoConcurrency}</strong>
                                    </span>
                                    <span className="membership-overview-metric">
                                        <small className="membership-overview-metric-label">积分充值</small>
                                        <strong className="membership-overview-metric-value">{topupDiscountLabel(overview.entitlement.topupDiscountBasisPoints)}</strong>
                                    </span>
                                </div>
                            </div>
                            <MembershipOrderHistory cancellingId={cancellingId} className="membership-orders-section" onCancel={(orderId) => void cancelOrder(orderId)} onPay={(orderId) => void openCheckout(orderId)} orders={overview.orders} payingId={payingId} plansById={plansById} />
                            <MembershipInvoiceCenter email={user?.email || ""} orders={overview.orders} plansById={plansById} />
                        </section>
                    ) : null}
                </div>
            </section>

            <MembershipPurchaseModal
                className="membership-purchase-dialog"
                onCancel={() => setSelection(null)}
                onSeatsChange={(seats) => setSelection((current) => (current ? { ...current, seats: clampSeats(current.plan, seats) } : null))}
                onSubmit={() => void submitOrder()}
                onTeamIdChange={setTeamId}
                onTeamNameChange={setTeamName}
                open={Boolean(selection)}
                plan={selection?.plan ?? null}
                seats={selection?.seats ?? 1}
                submitting={submitting}
                teamId={teamId}
                teamName={teamName}
                teams={overview?.teams ?? []}
            />
        </main>
    );
}
