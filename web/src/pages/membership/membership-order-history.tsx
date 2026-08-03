import { Button, Empty, Popconfirm, Tag } from "antd";
import { ChevronDown, ReceiptText } from "lucide-react";

import type { MembershipOrder, MembershipPlan } from "@/services/api/membership";

import { billingCycleShortLabel, formatMoney, publicPlanName } from "./membership-formatters";

const orderStatus = {
    pending: { label: "待支付", color: "gold" },
    paid: { label: "已支付", color: "green" },
    cancelled: { label: "已关闭", color: "default" },
    refunded: { label: "已退款", color: "blue" },
} as const;

type MembershipOrderHistoryProps = {
    cancellingId: string;
    className?: string;
    onCancel: (orderId: string) => void;
    onPay: (orderId: string) => void;
    orders: MembershipOrder[];
    payingId: string;
    plansById: Map<string, MembershipPlan>;
};

export function MembershipOrderHistory({
    cancellingId,
    className = "",
    onCancel,
    onPay,
    orders,
    payingId,
    plansById,
}: MembershipOrderHistoryProps) {
    return (
        <details className={`membership-orders ${className}`}>
            <summary className="membership-orders-summary">
                <span className="membership-orders-summary-main">
                    <ReceiptText className="membership-orders-summary-icon" />
                    <span className="membership-orders-summary-copy">
                        <strong className="membership-orders-title">我的会员订单</strong>
                        <small className="membership-orders-description">待支付订单超过 24 小时将自动关闭</small>
                    </span>
                </span>
                <span className="membership-orders-summary-side">
                    <span className="membership-orders-count">{orders.length} 笔</span>
                    <ChevronDown className="membership-orders-chevron" />
                </span>
            </summary>
            <div className="membership-orders-content">
                {orders.length === 0 ? (
                    <Empty className="membership-orders-empty" description="暂无会员订单" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                ) : (
                    <div className="membership-order-list">
                        {orders.map((order) => {
                            const plan = plansById.get(order.planId);
                            const status = orderStatus[order.status];
                            return (
                                <article className="membership-order-row" key={order.id}>
                                    <div className="membership-order-primary">
                                        <strong className="membership-order-plan">{plan ? publicPlanName(plan) : "套餐记录不可用"}</strong>
                                        <span className="membership-order-number">{order.orderNumber}</span>
                                    </div>
                                    <div className="membership-order-meta">
                                        <span className="membership-order-meta-item">{plan ? billingCycleShortLabel[plan.billingCycle] : "未知周期"}</span>
                                        <span className="membership-order-meta-item">{order.seats} 席位</span>
                                        <span className="membership-order-meta-item">{new Date(order.createdAt).toLocaleString("zh-CN")}</span>
                                    </div>
                                    <strong className="membership-order-amount">¥{formatMoney(order.totalPriceCents)}</strong>
                                    <Tag className="membership-order-status" color={status.color}>{status.label}</Tag>
                                    <div className="membership-order-action">
                                        {order.status === "pending" ? (
                                            <span className="membership-order-pending-actions">
                                                <Button className="membership-pay-order" loading={payingId === order.id} onClick={() => onPay(order.id)} size="small" type="primary">去支付</Button>
                                                <Popconfirm
                                                    cancelText="保留订单"
                                                    description="关闭后不能继续支付，需要重新创建订单。"
                                                    okText="确认关闭"
                                                    onConfirm={() => onCancel(order.id)}
                                                    title="关闭这笔待支付订单？"
                                                >
                                                    <Button className="membership-cancel-order" danger loading={cancellingId === order.id} size="small" type="text">关闭</Button>
                                                </Popconfirm>
                                            </span>
                                        ) : (
                                            <span className="membership-order-resolved">{order.resolutionNote || "—"}</span>
                                        )}
                                    </div>
                                </article>
                            );
                        })}
                    </div>
                )}
            </div>
        </details>
    );
}
