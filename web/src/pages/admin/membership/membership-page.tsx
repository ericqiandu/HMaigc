import { Button, Form, Input, InputNumber, message, Modal, Space, Switch, Table, Tabs, Tag } from "antd";
import { useEffect, useState } from "react";

import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { TableSurface } from "@/components/layout/workspace-page";
import { confirmAdminMembershipOrder, closeAdminMembershipOrder, listAdminMembershipOrders, listAdminMembershipPlans, type MembershipOrder, type MembershipPlan, type UpdateMembershipPlanInput, updateAdminMembershipPlan } from "@/services/api/membership";
import { listAdminPaymentTransactions, listAdminPaymentWebhookEvents, type PaymentTransaction, type PaymentWebhookEvent } from "@/services/api/payment";
import { InvoiceAdminPanel } from "./invoice-admin-panel";

const credits = (value: number) => (value / 1_000_000).toLocaleString("zh-CN");
const money = (value: number) => `¥${(value / 100).toLocaleString("zh-CN")}`;
const tebibyte = 1024 ** 4;
type PlanFormValues = Omit<UpdateMembershipPlanInput, "teamStorageBytes"> & { teamStorageTB: number };

export default function MembershipAdminPage() {
    const [plans, setPlans] = useState<MembershipPlan[]>([]);
    const [orders, setOrders] = useState<MembershipOrder[]>([]);
    const [transactions, setTransactions] = useState<PaymentTransaction[]>([]);
    const [webhookEvents, setWebhookEvents] = useState<PaymentWebhookEvent[]>([]);
    const [editing, setEditing] = useState<MembershipPlan | null>(null);
    const [confirming, setConfirming] = useState<MembershipOrder | null>(null);
    const [closing, setClosing] = useState<MembershipOrder | null>(null);
    const [loading, setLoading] = useState(false);
    const [form] = Form.useForm<PlanFormValues>();
    const [confirmForm] = Form.useForm<{ providerTradeNo: string; note: string }>();
    const [closeForm] = Form.useForm<{ note: string }>();

    const load = async () => {
        setLoading(true);
        try {
            const [nextPlans, nextOrders, nextTransactions, nextWebhookEvents] = await Promise.all([listAdminMembershipPlans(), listAdminMembershipOrders(), listAdminPaymentTransactions(), listAdminPaymentWebhookEvents()]);
            setPlans(nextPlans);
            setOrders(nextOrders.items);
            setTransactions(nextTransactions.items);
            setWebhookEvents(nextWebhookEvents.items);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "会员数据加载失败");
        } finally {
            setLoading(false);
        }
    };
    useEffect(() => {
        void load();
    }, []);

    const openPlan = (plan: MembershipPlan) => {
        setEditing(plan);
        form.setFieldsValue({
            name: plan.name,
            priceCents: plan.priceCents,
            originalPriceCents: plan.originalPriceCents,
            creditsPerPeriod: plan.creditsPerPeriod,
            imageConcurrency: plan.imageConcurrency,
            videoConcurrency: plan.videoConcurrency,
            topupDiscountBasisPoints: plan.topupDiscountBasisPoints,
            minSeats: plan.minSeats,
            maxSeats: plan.maxSeats,
            benefits: plan.benefits,
            unlimitedTaskQueue: plan.unlimitedTaskQueue,
            teamStorageTB: plan.teamStorageBytes / tebibyte,
            sharedAssetsEnabled: plan.sharedAssetsEnabled,
            projectPermissionsEnabled: plan.projectPermissionsEnabled,
            invoicingEnabled: plan.invoicingEnabled,
            commercialUseEnabled: plan.commercialUseEnabled,
            enabled: plan.enabled,
            sortOrder: plan.sortOrder,
        });
    };
    const savePlan = async () => {
        if (!editing) return;
        const values = await form.validateFields();
        const { teamStorageTB, ...planValues } = values;
        await updateAdminMembershipPlan(editing.id, { ...planValues, teamStorageBytes: Math.round(teamStorageTB * tebibyte) });
        message.success("套餐配置已更新");
        setEditing(null);
        await load();
    };
    const confirmOrder = async () => {
        if (!confirming) return;
        const values = await confirmForm.validateFields();
        await confirmAdminMembershipOrder(confirming.id, values);
        message.success("订单已确认，订阅与积分已生效");
        setConfirming(null);
        confirmForm.resetFields();
        await load();
    };
    const closeOrder = async () => {
        if (!closing) return;
        const values = await closeForm.validateFields();
        await closeAdminMembershipOrder(closing.id, values);
        message.success("待支付订单已关闭");
        setClosing(null);
        closeForm.resetFields();
        await load();
    };

    return (
        <AdminPageFrame title="会员商业化" description="统一管理个人与团队套餐、并发权益及人工核款订单。">
            <Tabs
                className="admin-membership-tabs"
                items={[
                    {
                        key: "plans",
                        label: "套餐与权益",
                        children: (
                            <TableSurface className="admin-membership-table-surface mt-0">
                                <Table
                                    className="admin-membership-table"
                                    rowKey="id"
                                    loading={loading}
                                    dataSource={plans}
                                    pagination={false}
                                    size="small"
                                    columns={[
                                        {
                                            title: "套餐",
                                            render: (_, row) => (
                                                <Space>
                                                    <strong>{row.name}</strong>
                                                    <Tag>{row.audience === "team" ? "团队" : "个人"}</Tag>
                                                    <span>{row.billingCycle}</span>
                                                </Space>
                                            ),
                                        },
                                        { title: "价格", render: (_, row) => money(row.priceCents) },
                                        { title: "周期积分", render: (_, row) => credits(row.creditsPerPeriod) },
                                        { title: "图片 / 视频并发", render: (_, row) => `${row.imageConcurrency} / ${row.videoConcurrency}` },
                                        { title: "席位", render: (_, row) => (row.audience === "team" ? `${row.minSeats}–${row.maxSeats}` : "单人") },
                                        { title: "状态", render: (_, row) => <Tag color={row.enabled ? "green" : "default"}>{row.enabled ? "上架" : "下架"}</Tag> },
                                        {
                                            title: "操作",
                                            render: (_, row) => (
                                                <Button type="link" onClick={() => openPlan(row)}>
                                                    编辑
                                                </Button>
                                            ),
                                        },
                                    ]}
                                />
                            </TableSurface>
                        ),
                    },
                    {
                        key: "orders",
                        label: "会员订单",
                        children: (
                            <TableSurface className="admin-membership-orders-surface mt-0">
                                <Table
                                    className="admin-membership-orders"
                                    rowKey="id"
                                    loading={loading}
                                    dataSource={orders}
                                    size="small"
                                    columns={[
                                        { title: "订单号", dataIndex: "orderNumber" },
                                        { title: "用户", dataIndex: "userId", ellipsis: true },
                                        { title: "金额", render: (_, row) => money(row.totalPriceCents) },
                                        { title: "席位", dataIndex: "seats" },
                                        {
                                            title: "状态",
                                            render: (_, row) => (
                                                <Tag className="admin-membership-order-status" color={row.status === "paid" ? "green" : row.status === "cancelled" ? "default" : row.status === "refunded" ? "blue" : "gold"}>
                                                    {{ pending: "待支付", paid: "已支付", cancelled: "已关闭", refunded: "已退款" }[row.status]}
                                                </Tag>
                                            ),
                                        },
                                        { title: "创建时间", render: (_, row) => new Date(row.createdAt).toLocaleString("zh-CN") },
                                        { title: "处理记录", render: (_, row) => row.resolutionNote || "—", ellipsis: true },
                                        {
                                            title: "操作",
                                            render: (_, row) =>
                                                row.status === "pending" ? (
                                                    <Space className="admin-membership-order-actions" size={4}>
                                                        <Button className="admin-membership-confirm-button" type="primary" size="small" onClick={() => setConfirming(row)}>
                                                            确认收款
                                                        </Button>
                                                        <Button className="admin-membership-close-button" type="text" danger size="small" onClick={() => setClosing(row)}>
                                                            关闭
                                                        </Button>
                                                    </Space>
                                                ) : (
                                                    "—"
                                                ),
                                        },
                                    ]}
                                />
                            </TableSurface>
                        ),
                    },
                    {
                        key: "invoices",
                        label: "开票处理",
                        children: <InvoiceAdminPanel />,
                    },
                    {
                        key: "transactions",
                        label: "支付交易",
                        children: (
                            <TableSurface className="admin-payment-transactions-surface mt-0">
                                <Table
                                    className="admin-payment-transactions-table"
                                    rowKey="id"
                                    loading={loading}
                                    dataSource={transactions}
                                    size="small"
                                    pagination={{ pageSize: 30, hideOnSinglePage: true }}
                                    columns={[
                                        { title: "商户订单号", dataIndex: "merchantOrderNo", ellipsis: true },
                                        { title: "渠道", render: (_, row) => <Tag className="admin-payment-provider-tag">{row.provider === "wechat" ? "微信" : "支付宝"}</Tag> },
                                        { title: "用户", dataIndex: "userId", ellipsis: true },
                                        { title: "金额", render: (_, row) => money(row.amountCents) },
                                        {
                                            title: "状态",
                                            render: (_, row) => (
                                                <Tag className="admin-payment-status-tag" color={row.status === "paid" ? "green" : row.status === "failed" ? "red" : "gold"}>
                                                    {row.status}
                                                </Tag>
                                            ),
                                        },
                                        { title: "渠道流水", render: (_, row) => row.providerTradeNo || "—", ellipsis: true },
                                        { title: "失败原因", render: (_, row) => row.failureReason || "—", ellipsis: true },
                                        { title: "创建时间", render: (_, row) => new Date(row.createdAt).toLocaleString("zh-CN") },
                                    ]}
                                />
                            </TableSurface>
                        ),
                    },
                    {
                        key: "webhooks",
                        label: "回调审计",
                        children: (
                            <TableSurface className="admin-payment-webhooks-surface mt-0">
                                <Table
                                    className="admin-payment-webhooks-table"
                                    rowKey="id"
                                    loading={loading}
                                    dataSource={webhookEvents}
                                    size="small"
                                    pagination={{ pageSize: 30, hideOnSinglePage: true }}
                                    columns={[
                                        { title: "事件 ID", dataIndex: "providerEventId", ellipsis: true },
                                        { title: "渠道", render: (_, row) => <Tag className="admin-webhook-provider-tag">{row.provider === "wechat" ? "微信" : "支付宝"}</Tag> },
                                        { title: "交易 ID", render: (_, row) => row.transactionId || "—", ellipsis: true },
                                        {
                                            title: "状态",
                                            render: (_, row) => (
                                                <Tag className="admin-webhook-status-tag" color={row.status === "processed" ? "green" : row.status === "rejected" ? "red" : "gold"}>
                                                    {row.status}
                                                </Tag>
                                            ),
                                        },
                                        { title: "失败原因", render: (_, row) => row.failureReason || "—", ellipsis: true },
                                        { title: "接收时间", render: (_, row) => new Date(row.receivedAt).toLocaleString("zh-CN") },
                                    ]}
                                />
                            </TableSurface>
                        ),
                    },
                ]}
            />
            <Modal className="admin-membership-plan-modal" title={`编辑 ${editing?.name ?? ""}`} open={Boolean(editing)} onCancel={() => setEditing(null)} onOk={() => void savePlan()} okText="保存">
                <Form className="admin-membership-plan-form" form={form} layout="vertical">
                    <Form.Item className="admin-membership-form-item" name="name" label="名称" rules={[{ required: true }]}>
                        <Input />
                    </Form.Item>
                    <Space className="admin-membership-form-row" wrap>
                        <Form.Item name="priceCents" label="售价（分）" rules={[{ required: true }]}>
                            <InputNumber min={0} />
                        </Form.Item>
                        <Form.Item name="originalPriceCents" label="原价（分）">
                            <InputNumber min={0} />
                        </Form.Item>
                        <Form.Item name="creditsPerPeriod" label="周期积分（微积分）">
                            <InputNumber min={0} />
                        </Form.Item>
                        <Form.Item name="imageConcurrency" label="图片并发">
                            <InputNumber min={1} />
                        </Form.Item>
                        <Form.Item name="videoConcurrency" label="视频并发">
                            <InputNumber min={1} />
                        </Form.Item>
                        <Form.Item name="topupDiscountBasisPoints" label="充值折扣基点">
                            <InputNumber min={1} max={10000} />
                        </Form.Item>
                        <Form.Item name="minSeats" label="最少席位">
                            <InputNumber min={0} />
                        </Form.Item>
                        <Form.Item name="maxSeats" label="最多席位">
                            <InputNumber min={0} />
                        </Form.Item>
                        <Form.Item name="teamStorageTB" label="团队存储（TB）">
                            <InputNumber min={0} precision={1} />
                        </Form.Item>
                        <Form.Item name="sortOrder" label="排序">
                            <InputNumber />
                        </Form.Item>
                    </Space>
                    {editing?.audience === "team" ? (
                        <div className="admin-membership-team-entitlements grid grid-cols-2 gap-x-5">
                            <Form.Item className="admin-membership-entitlement-field" name="unlimitedTaskQueue" label="无限任务排队" valuePropName="checked">
                                <Switch className="admin-membership-entitlement-switch" />
                            </Form.Item>
                            <Form.Item className="admin-membership-entitlement-field" name="sharedAssetsEnabled" label="团队共享资产库" valuePropName="checked">
                                <Switch className="admin-membership-entitlement-switch" />
                            </Form.Item>
                            <Form.Item className="admin-membership-entitlement-field" name="projectPermissionsEnabled" label="项目权限管理" valuePropName="checked">
                                <Switch className="admin-membership-entitlement-switch" />
                            </Form.Item>
                            <Form.Item className="admin-membership-entitlement-field" name="invoicingEnabled" label="开票权益" valuePropName="checked">
                                <Switch className="admin-membership-entitlement-switch" />
                            </Form.Item>
                            <Form.Item className="admin-membership-entitlement-field" name="commercialUseEnabled" label="商业使用权益" valuePropName="checked">
                                <Switch className="admin-membership-entitlement-switch" />
                            </Form.Item>
                        </div>
                    ) : null}
                    {editing?.audience === "team" ? (
                        <div className="admin-membership-team-benefit-contract mb-5 bg-black/[0.025] px-4 py-3 dark:bg-white/[0.04]">
                            <strong className="admin-membership-team-benefit-title block text-sm font-medium">团队权益采用结构化配置</strong>
                            <p className="admin-membership-team-benefit-description mt-1 text-xs leading-5 text-foreground/55">
                                多人画布协作、席位管理、积分用量管控与团队资产隔离为团队套餐内置能力；共享资产、任务排队、项目权限、开票、存储与商业授权由上方配置决定，购买页会据此自动生成真实权益清单。
                            </p>
                        </div>
                    ) : (
                        <Form.List name="benefits">
                            {(fields, { add, remove }) => (
                                <div className="admin-membership-benefits">
                                    {fields.map(({ key, ...field }) => (
                                        <Space className="admin-membership-benefit-row" key={key}>
                                            <Form.Item {...field} rules={[{ required: true }]}>
                                                <Input placeholder="套餐权益" />
                                            </Form.Item>
                                            <Button onClick={() => remove(field.name)}>移除</Button>
                                        </Space>
                                    ))}
                                    <Button onClick={() => add("")}>添加权益</Button>
                                </div>
                            )}
                        </Form.List>
                    )}
                    <Form.Item className="admin-membership-form-item" name="enabled" label="上架" valuePropName="checked">
                        <Switch />
                    </Form.Item>
                </Form>
            </Modal>
            <Modal className="admin-membership-confirm-modal" title="确认会员订单收款" open={Boolean(confirming)} onCancel={() => setConfirming(null)} onOk={() => void confirmOrder()} okText="确认并开通">
                <Form className="admin-membership-confirm-form" form={confirmForm} layout="vertical">
                    <Form.Item name="providerTradeNo" label="支付流水号" rules={[{ required: true, message: "请输入真实支付流水号" }]}>
                        <Input />
                    </Form.Item>
                    <Form.Item name="note" label="核验备注" rules={[{ required: true }]}>
                        <Input.TextArea rows={3} />
                    </Form.Item>
                </Form>
            </Modal>
            <Modal className="admin-membership-close-modal" title="关闭待支付订单" open={Boolean(closing)} onCancel={() => setClosing(null)} onOk={() => void closeOrder()} okText="确认关闭" okButtonProps={{ danger: true }}>
                <Form className="admin-membership-close-form" form={closeForm} layout="vertical">
                    <Form.Item className="admin-membership-close-reason" name="note" label="关闭原因" rules={[{ required: true, message: "请输入关闭原因，便于后续审计" }]}>
                        <Input.TextArea className="admin-membership-close-input" rows={3} maxLength={500} showCount />
                    </Form.Item>
                </Form>
            </Modal>
        </AdminPageFrame>
    );
}
