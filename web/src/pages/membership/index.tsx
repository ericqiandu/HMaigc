import { Button, Empty, Input, InputNumber, message, Modal, Popconfirm, Segmented, Select, Spin, Tag } from "antd";
import { ArrowLeft, Check, Clock3, Crown, ReceiptText, Users } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";

import {
    cancelMembershipOrder,
    createMembershipOrder,
    createTeam,
    getMyMembership,
    listMembershipPlans,
    type MembershipAudience,
    type MembershipBillingCycle,
    type MembershipOverview,
    type MembershipPlan,
} from "@/services/api/membership";
import { useUserStore } from "@/stores/use-user-store";

import "./membership.css";

const money = (value: number) => (value / 100).toLocaleString("zh-CN");
const creditAmount = (value: number) => (value / 1_000_000).toLocaleString("zh-CN");
const orderStatus = {
    pending: { label: "待支付", color: "gold" },
    paid: { label: "已支付", color: "green" },
    cancelled: { label: "已关闭", color: "default" },
    refunded: { label: "已退款", color: "blue" },
} as const;
const billingCycleLabel = { free: "永久免费", month: "月付", year: "年付" } as const;

export default function MembershipPage() {
    const navigate = useNavigate();
    const user = useUserStore((state) => state.user);
    const [plans, setPlans] = useState<MembershipPlan[]>([]);
    const [overview, setOverview] = useState<MembershipOverview | null>(null);
    const [audience, setAudience] = useState<MembershipAudience>("personal");
    const [cycle, setCycle] = useState<MembershipBillingCycle>("year");
    const [selected, setSelected] = useState<MembershipPlan | null>(null);
    const [teamId, setTeamId] = useState<string>();
    const [teamName, setTeamName] = useState("");
    const [seats, setSeats] = useState(2);
    const [submitting, setSubmitting] = useState(false);
    const [cancellingId, setCancellingId] = useState("");
    const [loading, setLoading] = useState(true);

    const load = async () => {
        setLoading(true);
        try {
            const nextPlans = await listMembershipPlans();
            setPlans(nextPlans);
            if (user) setOverview(await getMyMembership());
        } catch (error) {
            message.error(error instanceof Error ? error.message : "会员数据加载失败");
        } finally {
            setLoading(false);
        }
    };
    useEffect(() => { void load(); }, [user?.id]);

    const visiblePlans = useMemo(() => plans.filter((plan) => {
        if (plan.audience !== audience) return false;
        if (audience === "team") return plan.billingCycle === cycle;
        return plan.billingCycle === "free" || plan.billingCycle === cycle;
    }), [audience, cycle, plans]);

    const beginPurchase = (plan: MembershipPlan) => {
        if (!user) {
            navigate("/login");
            return;
        }
        setSelected(plan);
        setSeats(Math.max(plan.minSeats, 2));
        setTeamId(overview?.teams[0]?.id);
    };
    const submitOrder = async () => {
        if (!selected) return;
        setSubmitting(true);
        try {
            let resolvedTeamId = teamId;
            if (selected.audience === "team" && !resolvedTeamId) {
                if (!teamName.trim()) throw new Error("请输入团队名称");
                const team = await createTeam(teamName.trim());
                resolvedTeamId = team.id;
            }
            const order = await createMembershipOrder({ planId: selected.id, teamId: resolvedTeamId, seats: selected.audience === "team" ? seats : 1 });
            message.success(`订单 ${order.orderNumber} 已创建`);
            setSelected(null);
            await load();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "创建订单失败");
        } finally {
            setSubmitting(false);
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
    const plansById = useMemo(() => new Map(plans.map((plan) => [plan.id, plan])), [plans]);

    return (
        <main className="membership-page">
            <header className="membership-header">
                <button type="button" className="membership-back" onClick={() => navigate(-1)} aria-label="返回"><ArrowLeft className="size-4" /></button>
                <span className="membership-brand"><Crown className="size-4" />HMaigc 会员</span>
                {overview ? <span className="membership-current">当前：{overview.entitlement.planName} · 图片 {overview.entitlement.imageConcurrency} / 视频 {overview.entitlement.videoConcurrency} 并发</span> : null}
            </header>
            <section className="membership-hero">
                <span className="membership-eyebrow">MEMBERSHIP</span>
                <h1>选择适合你的创作规模</h1>
                <p>套餐权益、积分与并发均由后台统一配置，个人与团队订阅分别管理。</p>
                <div className="membership-switches">
                    <Segmented value={audience} onChange={(value) => setAudience(value as MembershipAudience)} options={[{ label: "个人会员", value: "personal" }, { label: "团队会员", value: "team" }]} />
                    <Segmented value={cycle} onChange={(value) => setCycle(value as MembershipBillingCycle)} options={[{ label: "月付", value: "month" }, { label: "年付 · 更优惠", value: "year" }]} />
                </div>
            </section>
            {overview ? (
                <section className="membership-overview" aria-label="当前会员权益">
                    <div className="membership-overview-heading">
                        <span className="membership-overview-icon"><Crown className="size-4" /></span>
                        <div className="membership-overview-title">
                            <span>当前方案</span>
                            <strong>{overview.entitlement.planName}</strong>
                        </div>
                    </div>
                    <div className="membership-overview-metrics">
                        <span className="membership-overview-metric"><small className="membership-overview-label">有效期</small><strong className="membership-overview-value">{overview.entitlement.expiresAt ? new Date(overview.entitlement.expiresAt).toLocaleDateString("zh-CN") : "长期有效"}</strong></span>
                        <span className="membership-overview-metric"><small className="membership-overview-label">图片并发</small><strong className="membership-overview-value">{overview.entitlement.imageConcurrency}</strong></span>
                        <span className="membership-overview-metric"><small className="membership-overview-label">视频并发</small><strong className="membership-overview-value">{overview.entitlement.videoConcurrency}</strong></span>
                        <span className="membership-overview-metric"><small className="membership-overview-label">积分充值折扣</small><strong className="membership-overview-value">{overview.entitlement.topupDiscountBasisPoints < 10000 ? `${overview.entitlement.topupDiscountBasisPoints / 1000} 折` : "无折扣"}</strong></span>
                    </div>
                </section>
            ) : null}
            {loading ? <div className="membership-loading"><Spin /></div> : (
                <section className="membership-grid">
                    {visiblePlans.map((plan) => (
                        <article className={`membership-card membership-card-${plan.tier}`} key={plan.id}>
                            <div className="membership-card-heading"><div><span>{plan.audience === "team" ? <Users className="size-4" /> : <Crown className="size-4" />}</span><h2>{plan.name}</h2></div><small>{plan.billingCycle === "free" ? "永久免费" : plan.billingCycle === "year" ? "按年订阅" : "按月订阅"}</small></div>
                            <div className="membership-price"><span>¥</span><strong>{money(plan.priceCents)}</strong><small>{plan.audience === "team" ? "/席位" : ""}</small></div>
                            {plan.originalPriceCents > plan.priceCents ? <div className="membership-original">原价 ¥{money(plan.originalPriceCents)}</div> : null}
                            <div className="membership-metrics">
                                <span><strong>{creditAmount(plan.creditsPerPeriod)}</strong> 周期积分</span>
                                <span><strong>{plan.imageConcurrency}</strong> 图片并发</span>
                                <span><strong>{plan.videoConcurrency}</strong> 视频并发</span>
                            </div>
                            <ul>{plan.benefits.map((benefit) => <li key={benefit}><Check className="size-4" /><span>{benefit}</span></li>)}</ul>
                            <Button type={plan.tier === "max" ? "primary" : "default"} disabled={plan.billingCycle === "free"} onClick={() => beginPurchase(plan)}>{plan.billingCycle === "free" ? "当前基础方案" : "创建购买订单"}</Button>
                        </article>
                    ))}
                </section>
            )}
            {overview ? (
                <section className="membership-orders" aria-labelledby="membership-orders-title">
                    <div className="membership-orders-heading">
                        <div className="membership-orders-title-wrap">
                            <ReceiptText className="size-4" />
                            <div className="membership-orders-title">
                                <h2 id="membership-orders-title">订单中心</h2>
                                <span>待支付订单超过 24 小时将自动关闭</span>
                            </div>
                        </div>
                        <span className="membership-orders-count">{overview.orders.length} 笔</span>
                    </div>
                    {overview.orders.length === 0 ? (
                        <Empty className="membership-orders-empty" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会员订单" />
                    ) : (
                        <div className="membership-order-list">
                            {overview.orders.map((order) => {
                                const plan = plansById.get(order.planId);
                                const status = orderStatus[order.status];
                                return (
                                    <article className="membership-order-row" key={order.id}>
                                        <div className="membership-order-primary">
                                            <strong>{plan?.name ?? "套餐记录不可用"}</strong>
                                            <span>{order.orderNumber}</span>
                                        </div>
                                        <div className="membership-order-meta">
                                            <span>{plan ? billingCycleLabel[plan.billingCycle] : "未知周期"}</span>
                                            <span>{order.seats} 席位</span>
                                            <span>{new Date(order.createdAt).toLocaleString("zh-CN")}</span>
                                        </div>
                                        <strong className="membership-order-amount">{money(order.totalPriceCents)}</strong>
                                        <Tag className="membership-order-status" color={status.color}>{status.label}</Tag>
                                        <div className="membership-order-action">
                                            {order.status === "pending" ? (
                                                <Popconfirm title="关闭这笔待支付订单？" description="关闭后不能继续支付，需要重新创建订单。" okText="确认关闭" cancelText="保留订单" onConfirm={() => void cancelOrder(order.id)}>
                                                    <Button className="membership-cancel-order" type="text" danger loading={cancellingId === order.id}>关闭</Button>
                                                </Popconfirm>
                                            ) : <span className="membership-order-resolved">{order.resolutionNote || "—"}</span>}
                                        </div>
                                    </article>
                                );
                            })}
                        </div>
                    )}
                </section>
            ) : null}
            <Modal className="membership-order-modal" title={`购买 ${selected?.name ?? ""}`} open={Boolean(selected)} confirmLoading={submitting} onCancel={() => setSelected(null)} onOk={() => void submitOrder()} okText="创建待付款订单">
                {selected?.audience === "team" ? <div className="membership-team-fields">
                    {overview?.teams.length ? <Select className="membership-team-select" value={teamId} onChange={setTeamId} options={overview.teams.map((team) => ({ label: team.name, value: team.id }))} /> : <Input value={teamName} onChange={(event) => setTeamName(event.target.value)} placeholder="新团队名称" />}
                    <label>席位数量<InputNumber min={selected.minSeats} max={selected.maxSeats} value={seats} onChange={(value) => setSeats(value ?? selected.minSeats)} /></label>
                </div> : null}
                <p className="membership-order-note"><Clock3 className="size-4" />订单创建后不会立即开通；支付回调或管理员核验成功后，订阅、并发与积分才会生效。</p>
            </Modal>
        </main>
    );
}
