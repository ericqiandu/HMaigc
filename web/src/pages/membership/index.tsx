import { Alert, Button, Empty, message, Spin } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import { Crown, ImageIcon, Video } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

import { membershipQueryKey } from "@/hooks/use-membership-action";
import {
    cancelMembershipOrder,
    createMembershipOrder,
    createTeam,
    getMembershipStorefront,
    getMyMembership,
    type MembershipAudience,
    type MembershipBillingCycle,
    type MembershipOverview,
    type MembershipPlan,
    type MembershipStorefront,
} from "@/services/api/membership";
import { createPaymentCheckout } from "@/services/api/payment";
import { useUserStore } from "@/stores/use-user-store";

import { clampSeats, publicPlanName, topupDiscountLabel } from "./membership-formatters";
import { MembershipOrderHistory } from "./membership-order-history";
import { MembershipInvoiceCenter } from "./membership-invoice-center";
import { MembershipPurchaseModal } from "./membership-purchase-modal";
import { MembershipStorefrontFAQs } from "./membership-storefront-faq";
import { MembershipStorefrontGeneration } from "./membership-storefront-generation";
import { MembershipStorefrontPricing } from "./membership-storefront-pricing";
import { MembershipStorefrontPromo } from "./membership-storefront-promo";
import "./membership.css";
import "./membership-order.css";
import "./membership-responsive.css";
import "./membership-storefront.css";

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
    const [storefront, setStorefront] = useState<MembershipStorefront | null>(null);
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

    const load = useCallback(async () => {
        setLoading(true);
        setLoadError("");
        try {
            const [nextStorefront, nextOverview] = await Promise.all([getMembershipStorefront(), user ? getMyMembership() : Promise.resolve(null)]);
            setStorefront(nextStorefront);
            setOverview(nextOverview);
            if (user && nextOverview) {
                queryClient.setQueryData(membershipQueryKey(user.id), nextOverview);
            }
            setTeamSeats((current) => {
                const next = { ...current };
                nextStorefront.plans
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

    const plans = storefront?.plans ?? [];

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

    const selectAudience = (nextAudience: MembershipAudience) => {
        setAudience(nextAudience);
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
        <main className="membership-storefront-page min-h-screen bg-[#070b11] font-sans antialiased">
            {storefront ? <MembershipStorefrontPromo promotion={storefront.presentation.promotion} serverNow={storefront.serverNow} /> : null}

            {loadError ? (
                <section aria-label="会员商城加载失败" className="membership-storefront-error mx-auto max-w-[1300px] px-6 py-20">
                    <Alert
                        action={
                            <Button className="membership-storefront-retry" onClick={() => void load()}>
                                重新加载
                            </Button>
                        }
                        className="membership-storefront-error-alert"
                        description={loadError}
                        message="会员商城加载失败"
                        showIcon
                        type="error"
                    />
                </section>
            ) : loading || !storefront ? (
                <section aria-label="会员商城加载中" className="membership-storefront-loading flex min-h-[50vh] items-center justify-center">
                    <Spin className="membership-storefront-loading-spinner" size="large" />
                </section>
            ) : (
                <div className="membership-storefront-content pb-4">
                    <MembershipStorefrontPricing
                        allPlans={plans}
                        audience={audience}
                        availableCycles={availableCycles}
                        cycle={cycle}
                        onAudienceChange={selectAudience}
                        onCycleChange={setCycle}
                        onOpenWallet={() => (user ? navigate("/credit-store") : navigate("/login?next=%2Fcredit-store"))}
                        onPurchase={beginPurchase}
                        onSeatsChange={(plan, seats) => setTeamSeats((current) => ({ ...current, [plan.id]: clampSeats(plan, seats) }))}
                        plans={visiblePlans}
                        presentation={storefront.presentation}
                        teamSeats={teamSeats}
                    />

                    {visiblePlans.length === 0 ? (
                        <section aria-label="暂无会员套餐" className="membership-storefront-empty mx-auto max-w-[1300px] px-6 py-14">
                            <Empty className="membership-storefront-empty-state" description="后台暂无当前类型与周期的上架套餐" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                        </section>
                    ) : null}

                    <MembershipStorefrontGeneration presentation={storefront.presentation} />
                    <MembershipStorefrontFAQs heading={storefront.presentation.copy.faqHeading} items={storefront.presentation.faqs} />

                    {overview ? (
                        <section aria-label="当前会员权益" className="membership-account-panel membership-storefront-account mx-auto mb-20 max-w-[1300px]">
                            <div className="membership-account-heading membership-storefront-account-heading">
                                <div className="membership-storefront-account-heading-copy">
                                    <h2 className="membership-storefront-account-title">当前会员权益</h2>
                                    <p className="membership-storefront-account-description">账户可用额度与并发能力实时同步</p>
                                </div>
                            </div>
                            <div className="membership-overview membership-storefront-overview">
                                <div className="membership-overview-heading membership-storefront-overview-heading">
                                    <span className="membership-overview-icon membership-storefront-overview-icon">
                                        <Crown className="membership-overview-icon-svg membership-storefront-overview-icon-svg" />
                                    </span>
                                    <div className="membership-overview-title membership-storefront-overview-title">
                                        <span className="membership-overview-label membership-storefront-overview-label">当前方案</span>
                                        <strong className="membership-overview-plan membership-storefront-overview-plan">{publicPlanName({ name: overview.entitlement.planName })}</strong>
                                    </div>
                                </div>
                                <div className="membership-overview-metrics membership-storefront-overview-metrics">
                                    <span className="membership-overview-metric membership-storefront-overview-metric">
                                        <small className="membership-overview-metric-label membership-storefront-overview-metric-label">有效期</small>
                                        <strong className="membership-overview-metric-value membership-storefront-overview-metric-value">
                                            {overview.entitlement.expiresAt ? new Date(overview.entitlement.expiresAt).toLocaleDateString("zh-CN") : "长期有效"}
                                        </strong>
                                    </span>
                                    <span className="membership-overview-metric membership-storefront-overview-metric">
                                        <ImageIcon className="membership-overview-metric-icon membership-storefront-overview-metric-icon" />
                                        <small className="membership-overview-metric-label membership-storefront-overview-metric-label">图片并发</small>
                                        <strong className="membership-overview-metric-value membership-storefront-overview-metric-value">{overview.entitlement.imageConcurrency}</strong>
                                    </span>
                                    <span className="membership-overview-metric membership-storefront-overview-metric">
                                        <Video className="membership-overview-metric-icon membership-storefront-overview-metric-icon" />
                                        <small className="membership-overview-metric-label membership-storefront-overview-metric-label">视频并发</small>
                                        <strong className="membership-overview-metric-value membership-storefront-overview-metric-value">{overview.entitlement.videoConcurrency}</strong>
                                    </span>
                                    <span className="membership-overview-metric membership-storefront-overview-metric">
                                        <small className="membership-overview-metric-label membership-storefront-overview-metric-label">积分充值</small>
                                        <strong className="membership-overview-metric-value membership-storefront-overview-metric-value">{topupDiscountLabel(overview.entitlement.topupDiscountBasisPoints)}</strong>
                                    </span>
                                </div>
                            </div>
                            <MembershipOrderHistory
                                cancellingId={cancellingId}
                                className="membership-orders-section membership-storefront-orders"
                                onCancel={(orderId) => void cancelOrder(orderId)}
                                onPay={(orderId) => void openCheckout(orderId)}
                                orders={overview.orders}
                                payingId={payingId}
                                plansById={plansById}
                            />
                            <MembershipInvoiceCenter email={user?.email || ""} orders={overview.orders} plansById={plansById} />
                        </section>
                    ) : null}
                </div>
            )}

            <MembershipPurchaseModal
                className="membership-purchase-dialog membership-storefront-purchase-dialog"
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
