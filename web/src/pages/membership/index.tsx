import { Alert, Button, Empty, message, Spin } from "antd";
import { useQueryClient } from "@tanstack/react-query";
import { Crown, ImageIcon, Video } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
    type MembershipOrder,
    type MembershipOrderRequestIdentity,
    type MembershipPlan,
    type MembershipStorefront,
    submitMembershipOrderRequest,
} from "@/services/api/membership";
import { createPaymentCheckout } from "@/services/api/payment";
import { useUserStore } from "@/stores/use-user-store";

import { paymentCheckoutTokenFromURL } from "../payment/payment-checkout-domain";
import { membershipOrderFactsFromOrder, type MembershipOrderLifecycle } from "../payment/membership-order-facts-domain";
import { clampSeats, publicPlanName, topupDiscountLabel } from "./membership-formatters";
import { MembershipOrderHistory } from "./membership-order-history";
import { MembershipInvoiceCenter } from "./membership-invoice-center";
import { MembershipPaymentDialog, shouldNavigateFromMembershipPage } from "./membership-payment-dialog";
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
    const [dialogOpen, setDialogOpen] = useState(false);
    const [teamId, setTeamId] = useState<string>();
    const [teamName, setTeamName] = useState("");
    const [orderLifecycle, setOrderLifecycle] = useState<MembershipOrderLifecycle>({ kind: "preorder" });
    const [checkoutToken, setCheckoutToken] = useState("");
    const [creationError, setCreationError] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const [openingCheckout, setOpeningCheckout] = useState(false);
    const [payingId, setPayingId] = useState("");
    const [cancellingId, setCancellingId] = useState("");
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const orderRequestIdentityRef = useRef<MembershipOrderRequestIdentity | null>(null);
    const resolvedTeamIDRef = useRef<string | undefined>(undefined);
    const paymentDialogWriteRef = useRef(false);

    const persistResolvedTeamID = useCallback((nextTeamID?: string) => {
        resolvedTeamIDRef.current = nextTeamID;
        setTeamId(nextTeamID);
    }, []);

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
        persistResolvedTeamID(requestedTeamId);
    }, [overview, persistResolvedTeamID, requestedTeamId]);

    useEffect(() => {
        const closeOnEscape = (event: KeyboardEvent) => {
            if (shouldNavigateFromMembershipPage(event.key, dialogOpen)) navigate(-1);
        };
        window.addEventListener("keydown", closeOnEscape);
        return () => window.removeEventListener("keydown", closeOnEscape);
    }, [dialogOpen, navigate]);

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
    const paymentPlanOptions = useMemo(() => {
        if (selection?.plan.audience !== "team") return selection?.plan ? [selection.plan] : [];
        return plans.filter((plan) => plan.audience === "team" && plan.billingCycle !== "free" && plan.tier === selection.plan.tier).sort((left, right) => left.sortOrder - right.sortOrder);
    }, [plans, selection]);

    const selectAudience = (nextAudience: MembershipAudience) => {
        setAudience(nextAudience);
    };

    const resetPaymentDialogFacts = useCallback(() => {
        setOrderLifecycle({ kind: "preorder" });
        setCheckoutToken("");
        setCreationError("");
        setSubmitting(false);
        setOpeningCheckout(false);
        orderRequestIdentityRef.current = null;
    }, []);

    const storeFrozenOrderFacts = useCallback((order: MembershipOrder): MembershipOrderLifecycle => {
        let lifecycle: MembershipOrderLifecycle;
        try {
            lifecycle = { facts: membershipOrderFactsFromOrder(order), kind: "frozen-ready", orderId: order.id };
        } catch (error) {
            lifecycle = { error: `订单冻结事实验证失败：${error instanceof Error ? error.message : "未知错误"}`, kind: "frozen-invalid" };
        }
        setOrderLifecycle(lifecycle);
        return lifecycle;
    }, []);

    const openCheckoutForOrder = useCallback(async (orderId: string) => {
        if (paymentDialogWriteRef.current) return;
        paymentDialogWriteRef.current = true;
        setOpeningCheckout(true);
        setCreationError("");
        try {
            const checkout = await createPaymentCheckout(orderId);
            setCheckoutToken(paymentCheckoutTokenFromURL(checkout.checkoutUrl, window.location.origin));
        } catch (error) {
            setCreationError(error instanceof Error ? error.message : "收银台打开失败");
        } finally {
            paymentDialogWriteRef.current = false;
            setOpeningCheckout(false);
        }
    }, []);

    const createOrderAndOpenCheckout = useCallback(
        async (captured: PurchaseSelection) => {
            if (paymentDialogWriteRef.current) return;
            paymentDialogWriteRef.current = true;
            setSubmitting(true);
            setCreationError("");
            try {
                let order: MembershipOrder;
                try {
                    order = await submitMembershipOrderRequest(
                        {
                            planId: captured.plan.id,
                            seats: captured.plan.audience === "team" ? captured.seats : 1,
                            teamId: resolvedTeamIDRef.current,
                        },
                        teamName,
                        captured.plan.audience === "team",
                        orderRequestIdentityRef.current,
                        crypto.randomUUID(),
                        {
                            createTeam,
                            createOrder: createMembershipOrder,
                            persistResolvedTeamID,
                            persistIdentity: (identity) => {
                                orderRequestIdentityRef.current = identity;
                            },
                        },
                    );
                } catch (error) {
                    setCreationError(error instanceof Error ? error.message : "创建付款订单失败");
                    return;
                }
                orderRequestIdentityRef.current = null;
                const lifecycle = storeFrozenOrderFacts(order);
                if (lifecycle.kind !== "frozen-ready") return;
                setSubmitting(false);
                setOpeningCheckout(true);
                try {
                    const checkout = await createPaymentCheckout(lifecycle.orderId);
                    setCheckoutToken(paymentCheckoutTokenFromURL(checkout.checkoutUrl, window.location.origin));
                } catch (error) {
                    setCreationError(error instanceof Error ? error.message : "收银台打开失败");
                    await load();
                }
            } finally {
                paymentDialogWriteRef.current = false;
                setSubmitting(false);
                setOpeningCheckout(false);
            }
        },
        [load, persistResolvedTeamID, storeFrozenOrderFacts, teamName],
    );

    const beginPurchase = (plan: MembershipPlan, seats: number) => {
        if (!user) {
            navigate("/login?next=%2Fmembership");
            return;
        }
        const next = { plan, seats: clampSeats(plan, seats) };
        persistResolvedTeamID(plan.audience === "team" ? (requestedTeamId && overview?.teams.some((team) => team.id === requestedTeamId) ? requestedTeamId : overview?.teams[0]?.id) : undefined);
        setTeamName("");
        resetPaymentDialogFacts();
        setSelection(next);
        setDialogOpen(true);
        if (plan.audience === "personal") void createOrderAndOpenCheckout(next);
    };

    const retryPaymentDialog = () => {
        if (orderLifecycle.kind === "frozen-ready") {
            void openCheckoutForOrder(orderLifecycle.orderId);
            return;
        }
        if (orderLifecycle.kind === "preorder" && selection) void createOrderAndOpenCheckout(selection);
    };

    const closePaymentDialog = () => {
        if (paymentDialogWriteRef.current || submitting || openingCheckout) return;
        setDialogOpen(false);
        setSelection(null);
        resetPaymentDialogFacts();
        void load();
    };

    const openCheckout = (orderId: string) => {
        const order = overview?.orders.find((candidate) => candidate.id === orderId);
        if (!order) {
            message.error("未找到待支付会员订单，无法打开收银台");
            return;
        }
        resetPaymentDialogFacts();
        setSelection(null);
        const lifecycle = storeFrozenOrderFacts(order);
        setDialogOpen(true);
        setPayingId(orderId);
        if (lifecycle.kind !== "frozen-ready") {
            setPayingId("");
            return;
        }
        void openCheckoutForOrder(lifecycle.orderId).finally(() => setPayingId(""));
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

            <MembershipPaymentDialog
                checkoutToken={checkoutToken}
                className="membership-storefront-payment-dialog"
                creationError={creationError}
                onClose={closePaymentDialog}
                onConfirm={() => {
                    if (selection) void createOrderAndOpenCheckout(selection);
                }}
                onPlanChange={(planID) => {
                    if (orderLifecycle.kind !== "preorder" || submitting || openingCheckout) return;
                    const nextPlan = paymentPlanOptions.find((plan) => plan.id === planID);
                    if (!nextPlan) return;
                    setCreationError("");
                    setSelection((current) => (current ? { plan: nextPlan, seats: clampSeats(nextPlan, current.seats) } : current));
                }}
                onRetry={retryPaymentDialog}
                onSeatsChange={(seats) => setSelection((current) => (current ? { ...current, seats: clampSeats(current.plan, seats) } : null))}
                onTeamIdChange={persistResolvedTeamID}
                onTeamNameChange={setTeamName}
                open={dialogOpen}
                openingCheckout={openingCheckout}
                orderLifecycle={orderLifecycle}
                plan={selection?.plan ?? null}
                planOptions={paymentPlanOptions}
                seats={selection?.seats ?? 1}
                submitting={submitting}
                teamId={teamId}
                teamName={teamName}
                teams={overview?.teams ?? []}
            />
        </main>
    );
}
