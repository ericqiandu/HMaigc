import { App, Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs, Tag } from "antd";
import { Plus, RefreshCw, SquarePen, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useBlocker } from "react-router";

import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { AdminContentError, AdminContentSkeleton, AdminTableEmpty } from "@/pages/admin/components/admin-ui";
import { TableSurface } from "@/components/layout/workspace-page";
import { confirmAdminMembershipOrder, closeAdminMembershipOrder, listAdminMembershipOrders, listAdminMembershipPlans, type MembershipOrder, type MembershipPlan, type UpdateMembershipPlanInput, updateAdminMembershipPlan } from "@/services/api/membership";
import { listAdminPaymentTransactions, listAdminPaymentWebhookEvents, type PaymentTransaction, type PaymentWebhookEvent } from "@/services/api/payment";
import { InvoiceAdminPanel } from "./invoice-admin-panel";
import { MembershipStorefrontAdminPanel } from "./membership-storefront-admin-panel";

const credits = (value: number) => (value / 1_000_000).toLocaleString("zh-CN");
const money = (value: number) => `¥${(value / 100).toLocaleString("zh-CN")}`;
const tebibyte = 1024 ** 4;
type PlanFormValues = Omit<UpdateMembershipPlanInput, "teamStorageBytes"> & { teamStorageTB: number };
type MembershipSection = "plans" | "storefront" | "orders" | "invoices" | "transactions" | "webhooks";

const membershipSectionOptions: Array<{ value: MembershipSection; label: string }> = [
    { value: "plans", label: "套餐与权益" },
    { value: "storefront", label: "商城展示" },
    { value: "orders", label: "会员订单" },
    { value: "invoices", label: "开票处理" },
    { value: "transactions", label: "支付交易" },
    { value: "webhooks", label: "回调审计" },
];

type ResourceState<T> = {
    data: T;
    loading: boolean;
    loaded: boolean;
    error: string;
};

const resourceState = <T,>(data: T): ResourceState<T> => ({ data, loading: false, loaded: false, error: "" });

export default function MembershipAdminPage() {
    const { message, modal } = App.useApp();
    const [plansState, setPlansState] = useState(() => resourceState<MembershipPlan[]>([]));
    const [ordersState, setOrdersState] = useState(() => resourceState<MembershipOrder[]>([]));
    const [transactionsState, setTransactionsState] = useState(() => resourceState<PaymentTransaction[]>([]));
    const [webhooksState, setWebhooksState] = useState(() => resourceState<PaymentWebhookEvent[]>([]));
    const [editing, setEditing] = useState<MembershipPlan | null>(null);
    const [confirming, setConfirming] = useState<MembershipOrder | null>(null);
    const [closing, setClosing] = useState<MembershipOrder | null>(null);
    const [savingPlan, setSavingPlan] = useState(false);
    const [processingOrder, setProcessingOrder] = useState(false);
    const [planDirty, setPlanDirty] = useState(false);
    const [activeSection, setActiveSection] = useState<MembershipSection>("plans");
    const [form] = Form.useForm<PlanFormValues>();
    const [confirmForm] = Form.useForm<{ providerTradeNo: string; note: string }>();
    const [closeForm] = Form.useForm<{ note: string }>();
    const loadSequences = useRef<Record<Exclude<MembershipSection, "invoices" | "storefront">, number>>({ plans: 0, orders: 0, transactions: 0, webhooks: 0 });

    const loadPlans = useCallback(async () => {
        const sequence = ++loadSequences.current.plans;
        setPlansState((current) => ({ ...current, loading: true, error: "" }));
        try {
            const data = await listAdminMembershipPlans();
            if (sequence !== loadSequences.current.plans) return;
            setPlansState({ data, loading: false, loaded: true, error: "" });
        } catch (error) {
            if (sequence !== loadSequences.current.plans) return;
            const description = error instanceof Error ? error.message : "套餐配置加载失败";
            setPlansState((current) => ({ ...current, loading: false, loaded: true, error: description }));
        }
    }, []);

    const loadOrders = useCallback(async () => {
        const sequence = ++loadSequences.current.orders;
        setOrdersState((current) => ({ ...current, loading: true, error: "" }));
        try {
            const result = await listAdminMembershipOrders();
            if (sequence !== loadSequences.current.orders) return;
            setOrdersState({ data: result.items, loading: false, loaded: true, error: "" });
        } catch (error) {
            if (sequence !== loadSequences.current.orders) return;
            const description = error instanceof Error ? error.message : "会员订单加载失败";
            setOrdersState((current) => ({ ...current, loading: false, loaded: true, error: description }));
        }
    }, []);

    const loadTransactions = useCallback(async () => {
        const sequence = ++loadSequences.current.transactions;
        setTransactionsState((current) => ({ ...current, loading: true, error: "" }));
        try {
            const result = await listAdminPaymentTransactions();
            if (sequence !== loadSequences.current.transactions) return;
            setTransactionsState({ data: result.items, loading: false, loaded: true, error: "" });
        } catch (error) {
            if (sequence !== loadSequences.current.transactions) return;
            const description = error instanceof Error ? error.message : "支付交易加载失败";
            setTransactionsState((current) => ({ ...current, loading: false, loaded: true, error: description }));
        }
    }, []);

    const loadWebhooks = useCallback(async () => {
        const sequence = ++loadSequences.current.webhooks;
        setWebhooksState((current) => ({ ...current, loading: true, error: "" }));
        try {
            const result = await listAdminPaymentWebhookEvents();
            if (sequence !== loadSequences.current.webhooks) return;
            setWebhooksState({ data: result.items, loading: false, loaded: true, error: "" });
        } catch (error) {
            if (sequence !== loadSequences.current.webhooks) return;
            const description = error instanceof Error ? error.message : "支付回调加载失败";
            setWebhooksState((current) => ({ ...current, loading: false, loaded: true, error: description }));
        }
    }, []);

    useEffect(() => {
        if (activeSection === "plans" && !plansState.loaded) void loadPlans();
        if (activeSection === "orders" && !ordersState.loaded) void loadOrders();
        if (activeSection === "transactions" && !transactionsState.loaded) void loadTransactions();
        if (activeSection === "webhooks" && !webhooksState.loaded) void loadWebhooks();
    }, [activeSection, loadOrders, loadPlans, loadTransactions, loadWebhooks, ordersState.loaded, plansState.loaded, transactionsState.loaded, webhooksState.loaded]);

    const refreshActiveSection = () => {
        if (activeSection === "plans") void loadPlans();
        if (activeSection === "orders") void loadOrders();
        if (activeSection === "transactions") void loadTransactions();
        if (activeSection === "webhooks") void loadWebhooks();
    };

    const activeLoading = activeSection === "plans" ? plansState.loading : activeSection === "orders" ? ordersState.loading : activeSection === "transactions" ? transactionsState.loading : activeSection === "webhooks" ? webhooksState.loading : false;

    const openPlan = (plan: MembershipPlan) => {
        setEditing(plan);
        setPlanDirty(false);
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

    const closePlanEditor = () => {
        if (savingPlan) return;
        if (!planDirty) {
            setEditing(null);
            return;
        }
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "放弃未保存的套餐修改？",
            content: `“${editing?.name ?? "当前套餐"}”的价格、积分或权益配置尚未保存。`,
            okText: "放弃修改",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => {
                setPlanDirty(false);
                setEditing(null);
            },
        });
    };

    const blocker = useBlocker(Boolean(editing && planDirty) && !savingPlan);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "离开并放弃套餐修改？",
            content: `“${editing?.name ?? "当前套餐"}”仍有未保存配置，离开后将丢失。`,
            okText: "放弃并离开",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => blocker.proceed(),
            onCancel: () => blocker.reset(),
        });
    }, [blocker, editing?.name, modal]);

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!editing || !planDirty || savingPlan) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [editing, planDirty, savingPlan]);

    const savePlan = async () => {
        if (!editing) return;
        setSavingPlan(true);
        try {
            const values = await form.validateFields();
            const { teamStorageTB, ...planValues } = values;
            const updated = await updateAdminMembershipPlan(editing.id, { ...planValues, teamStorageBytes: Math.round(teamStorageTB * tebibyte) });
            setPlansState((current) => ({ ...current, data: current.data.map((plan) => (plan.id === updated.id ? updated : plan)) }));
            setPlanDirty(false);
            setEditing(null);
            message.success("套餐配置已更新");
        } catch (error) {
            if (error instanceof Error) message.error(error.message || "套餐配置保存失败");
        } finally {
            setSavingPlan(false);
        }
    };
    const confirmOrder = async () => {
        if (!confirming) return;
        setProcessingOrder(true);
        try {
            const values = await confirmForm.validateFields();
            await confirmAdminMembershipOrder(confirming.id, values);
            message.success("订单已确认，订阅与积分已生效");
            setConfirming(null);
            confirmForm.resetFields();
            await loadOrders();
        } catch (error) {
            if (error instanceof Error) message.error(error.message || "订单确认失败");
        } finally {
            setProcessingOrder(false);
        }
    };
    const closeOrder = async () => {
        if (!closing) return;
        setProcessingOrder(true);
        try {
            const values = await closeForm.validateFields();
            await closeAdminMembershipOrder(closing.id, values);
            message.success("待支付订单已关闭");
            setClosing(null);
            closeForm.resetFields();
            await loadOrders();
        } catch (error) {
            if (error instanceof Error) message.error(error.message || "订单关闭失败");
        } finally {
            setProcessingOrder(false);
        }
    };

    const pendingOrderCount = useMemo(() => ordersState.data.filter((order) => order.status === "pending").length, [ordersState.data]);

    return (
        <AdminPageFrame
            title="会员管理"
            description="统一维护套餐权益、会员订单、发票与支付事实。"
            actions={
                activeSection !== "invoices" && activeSection !== "storefront" ? (
                    <Button className="admin-membership-refresh" icon={<RefreshCw className="admin-membership-refresh-icon size-4" />} loading={activeLoading} onClick={refreshActiveSection}>
                        刷新数据
                    </Button>
                ) : undefined
            }
        >
            <div className="admin-membership-content">
                <section className="admin-membership-context" aria-label="会员运营概览">
                    <div className="admin-membership-context-item">
                        <span className="admin-membership-context-label">套餐目录</span>
                        <strong className="admin-membership-context-value">{plansState.loaded && !plansState.error ? `${plansState.data.length} 个套餐` : "等待读取"}</strong>
                    </div>
                    <div className="admin-membership-context-item">
                        <span className="admin-membership-context-label">待核款订单</span>
                        <strong className="admin-membership-context-value">{ordersState.loaded && !ordersState.error ? `${pendingOrderCount} 笔待处理` : "进入订单后读取"}</strong>
                    </div>
                    <p className="admin-membership-context-note">人工确认收款会立即开通订阅并发放积分，所有操作必须以真实支付流水为依据。</p>
                </section>
                <Select className="admin-membership-mobile-section" aria-label="选择会员管理功能" value={activeSection} options={membershipSectionOptions} onChange={(value: MembershipSection) => setActiveSection(value)} />
                <Tabs
                    className="admin-membership-tabs"
                    activeKey={activeSection}
                    onChange={(key) => setActiveSection(key as MembershipSection)}
                    items={[
                        {
                            key: "plans",
                            label: "套餐与权益",
                            children:
                                !plansState.loaded && plansState.loading ? (
                                    <AdminContentSkeleton rows={6} label="正在加载套餐配置" />
                                ) : plansState.error && plansState.data.length === 0 ? (
                                    <AdminContentError title="套餐配置加载失败" description={plansState.error} onRetry={() => void loadPlans()} />
                                ) : (
                                    <TableSurface className="admin-membership-table-surface mt-0">
                                        {plansState.error ? (
                                            <div className="admin-membership-inline-error" role="status">
                                                刷新失败，当前展示上次成功读取的数据：{plansState.error}
                                            </div>
                                        ) : null}
                                        <Table
                                            className="admin-membership-table"
                                            rowKey="id"
                                            loading={plansState.loading && plansState.data.length > 0}
                                            dataSource={plansState.data}
                                            locale={{ emptyText: <AdminTableEmpty title="暂无套餐配置" description="服务端尚未提供可运营的会员套餐。" /> }}
                                            pagination={false}
                                            size="small"
                                            columns={[
                                                {
                                                    title: "套餐",
                                                    width: 190,
                                                    render: (_, row) => (
                                                        <div className="admin-membership-plan-cell">
                                                            <strong className="admin-membership-plan-name">{row.name}</strong>
                                                            <span className="admin-membership-plan-meta">
                                                                {row.audience === "team" ? "团队套餐" : "个人套餐"} · {billingCycleLabel(row.billingCycle)}
                                                            </span>
                                                        </div>
                                                    ),
                                                },
                                                { title: "价格", width: 110, responsive: ["sm"], render: (_, row) => <span className="admin-membership-table-number is-price">{money(row.priceCents)}</span> },
                                                { title: "周期积分", width: 130, responsive: ["md"], render: (_, row) => <span className="admin-membership-table-number">{credits(row.creditsPerPeriod)}</span> },
                                                {
                                                    title: "图片 / 视频并发",
                                                    width: 140,
                                                    responsive: ["lg"],
                                                    render: (_, row) => (
                                                        <span className="admin-membership-table-number">
                                                            {row.imageConcurrency} / {row.videoConcurrency}
                                                        </span>
                                                    ),
                                                },
                                                { title: "席位", width: 90, responsive: ["lg"], render: (_, row) => <span className="admin-membership-seat-count">{row.audience === "team" ? `${row.minSeats}–${row.maxSeats}` : "1"}</span> },
                                                {
                                                    title: "状态",
                                                    width: 80,
                                                    responsive: ["sm"],
                                                    render: (_, row) => (
                                                        <Tag className="admin-membership-plan-status" color={row.enabled ? "green" : "default"}>
                                                            {row.enabled ? "上架" : "下架"}
                                                        </Tag>
                                                    ),
                                                },
                                                {
                                                    title: "操作",
                                                    width: 64,
                                                    align: "center",
                                                    render: (_, row) => (
                                                        <Button
                                                            className="admin-membership-edit-button"
                                                            type="text"
                                                            icon={<SquarePen className="admin-membership-edit-icon size-4" />}
                                                            aria-label={`编辑${row.name}套餐`}
                                                            title="编辑套餐"
                                                            onClick={() => openPlan(row)}
                                                        />
                                                    ),
                                                },
                                            ]}
                                            scroll={{ x: "max-content" }}
                                        />
                                    </TableSurface>
                                ),
                        },
                        {
                            key: "storefront",
                            label: "商城展示",
                            children: <MembershipStorefrontAdminPanel />,
                        },
                        {
                            key: "orders",
                            label: "会员订单",
                            children:
                                !ordersState.loaded && ordersState.loading ? (
                                    <AdminContentSkeleton rows={8} label="正在加载会员订单" />
                                ) : ordersState.error && ordersState.data.length === 0 ? (
                                    <AdminContentError title="会员订单加载失败" description={ordersState.error} onRetry={() => void loadOrders()} />
                                ) : (
                                    <TableSurface className="admin-membership-orders-surface mt-0">
                                        {ordersState.error ? (
                                            <div className="admin-membership-inline-error" role="status">
                                                刷新失败，当前展示上次成功读取的数据：{ordersState.error}
                                            </div>
                                        ) : null}
                                        <Table
                                            className="admin-membership-orders"
                                            rowKey="id"
                                            loading={ordersState.loading && ordersState.data.length > 0}
                                            dataSource={ordersState.data}
                                            locale={{ emptyText: <AdminTableEmpty title="暂无会员订单" description="当前没有会员购买或待核款记录。" /> }}
                                            size="small"
                                            scroll={{ x: 980 }}
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
                            children:
                                !transactionsState.loaded && transactionsState.loading ? (
                                    <AdminContentSkeleton rows={8} label="正在加载支付交易" />
                                ) : transactionsState.error && transactionsState.data.length === 0 ? (
                                    <AdminContentError title="支付交易加载失败" description={transactionsState.error} onRetry={() => void loadTransactions()} />
                                ) : (
                                    <TableSurface className="admin-payment-transactions-surface mt-0">
                                        {transactionsState.error ? (
                                            <div className="admin-membership-inline-error" role="status">
                                                刷新失败，当前展示上次成功读取的数据：{transactionsState.error}
                                            </div>
                                        ) : null}
                                        <Table
                                            className="admin-payment-transactions-table"
                                            rowKey="id"
                                            loading={transactionsState.loading && transactionsState.data.length > 0}
                                            dataSource={transactionsState.data}
                                            locale={{ emptyText: <AdminTableEmpty title="暂无支付交易" description="当前没有微信或支付宝交易记录。" /> }}
                                            size="small"
                                            pagination={{ pageSize: 30, hideOnSinglePage: true }}
                                            scroll={{ x: 1080 }}
                                            columns={[
                                                { title: "商户订单号", dataIndex: "merchantOrderNo", ellipsis: true },
                                                { title: "渠道", render: (_, row) => <Tag className="admin-payment-provider-tag">{row.provider === "wechat" ? "微信" : "支付宝"}</Tag> },
                                                { title: "用户", dataIndex: "userId", ellipsis: true },
                                                { title: "金额", render: (_, row) => money(row.amountCents) },
                                                {
                                                    title: "状态",
                                                    render: (_, row) => (
                                                        <Tag className="admin-payment-status-tag" color={row.status === "paid" ? "green" : row.status === "failed" ? "red" : "gold"}>
                                                            {paymentStatusLabel(row.status)}
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
                            children:
                                !webhooksState.loaded && webhooksState.loading ? (
                                    <AdminContentSkeleton rows={8} label="正在加载回调审计" />
                                ) : webhooksState.error && webhooksState.data.length === 0 ? (
                                    <AdminContentError title="回调审计加载失败" description={webhooksState.error} onRetry={() => void loadWebhooks()} />
                                ) : (
                                    <TableSurface className="admin-payment-webhooks-surface mt-0">
                                        {webhooksState.error ? (
                                            <div className="admin-membership-inline-error" role="status">
                                                刷新失败，当前展示上次成功读取的数据：{webhooksState.error}
                                            </div>
                                        ) : null}
                                        <Table
                                            className="admin-payment-webhooks-table"
                                            rowKey="id"
                                            loading={webhooksState.loading && webhooksState.data.length > 0}
                                            dataSource={webhooksState.data}
                                            locale={{ emptyText: <AdminTableEmpty title="暂无回调事件" description="当前没有支付渠道回调审计记录。" /> }}
                                            size="small"
                                            pagination={{ pageSize: 30, hideOnSinglePage: true }}
                                            scroll={{ x: 920 }}
                                            columns={[
                                                { title: "事件 ID", dataIndex: "providerEventId", ellipsis: true },
                                                { title: "渠道", render: (_, row) => <Tag className="admin-webhook-provider-tag">{row.provider === "wechat" ? "微信" : "支付宝"}</Tag> },
                                                { title: "交易 ID", render: (_, row) => row.transactionId || "—", ellipsis: true },
                                                {
                                                    title: "状态",
                                                    render: (_, row) => (
                                                        <Tag className="admin-webhook-status-tag" color={row.status === "processed" ? "green" : row.status === "rejected" ? "red" : "gold"}>
                                                            {webhookStatusLabel(row.status)}
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
            </div>
            <Modal
                className="admin-operation-modal admin-membership-plan-modal workspace-ui-scope"
                width={720}
                title={`编辑 ${editing?.name ?? ""}`}
                open={Boolean(editing)}
                closable={!savingPlan}
                maskClosable={!savingPlan}
                keyboard={!savingPlan}
                confirmLoading={savingPlan}
                okButtonProps={{ disabled: !planDirty }}
                onCancel={closePlanEditor}
                onOk={() => void savePlan()}
                okText={planDirty ? "保存并生效" : "已同步"}
            >
                <Form className="admin-membership-plan-form" form={form} layout="vertical" disabled={savingPlan} onValuesChange={() => setPlanDirty(true)}>
                    <div className="admin-membership-form-intro">
                        <strong className="admin-membership-form-intro-title">套餐计费与资源权益</strong>
                        <span className="admin-membership-form-intro-copy">价格、积分、并发和席位调整会影响后续新订单；已生效订阅按服务端订阅快照执行。</span>
                    </div>
                    <Form.Item className="admin-membership-form-item" name="name" label="名称" rules={[{ required: true }]}>
                        <Input className="admin-membership-name-input" />
                    </Form.Item>
                    <div className="admin-membership-form-grid">
                        <Form.Item className="admin-membership-grid-field" name="priceCents" label="售价（分）" rules={[{ required: true }]}>
                            <InputNumber className="admin-membership-number-input" min={0} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="originalPriceCents" label="原价（分）">
                            <InputNumber className="admin-membership-number-input" min={0} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="creditsPerPeriod" label="周期积分（微积分）">
                            <InputNumber className="admin-membership-number-input" min={0} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="imageConcurrency" label="图片并发">
                            <InputNumber className="admin-membership-number-input" min={1} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="videoConcurrency" label="视频并发">
                            <InputNumber className="admin-membership-number-input" min={1} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="topupDiscountBasisPoints" label="充值折扣基点">
                            <InputNumber className="admin-membership-number-input" min={1} max={10000} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="minSeats" label="最少席位">
                            <InputNumber className="admin-membership-number-input" min={0} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="maxSeats" label="最多席位">
                            <InputNumber className="admin-membership-number-input" min={0} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="teamStorageTB" label="团队存储（TB）">
                            <InputNumber className="admin-membership-number-input" min={0} precision={1} />
                        </Form.Item>
                        <Form.Item className="admin-membership-grid-field" name="sortOrder" label="排序">
                            <InputNumber className="admin-membership-number-input" />
                        </Form.Item>
                    </div>
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
                                    <div className="admin-membership-benefit-header">
                                        <div className="admin-membership-benefit-heading">
                                            <strong className="admin-membership-benefit-title">套餐权益</strong>
                                            <span className="admin-membership-benefit-summary">将展示在用户端套餐说明中</span>
                                        </div>
                                        <Button className="admin-membership-benefit-add" icon={<Plus className="admin-membership-benefit-action-icon" size={15} strokeWidth={1.8} />} onClick={() => add("")}>
                                            添加权益
                                        </Button>
                                    </div>
                                    <div className="admin-membership-benefit-list">
                                        {fields.map(({ key, ...field }, index) => (
                                            <div className="admin-membership-benefit-row" key={key}>
                                                <Form.Item className="admin-membership-benefit-field" {...field} rules={[{ required: true, message: "请输入套餐权益" }]}>
                                                    <Input className="admin-membership-benefit-input" placeholder={`权益 ${index + 1}`} />
                                                </Form.Item>
                                                <Button
                                                    className="admin-membership-benefit-remove"
                                                    type="text"
                                                    icon={<Trash2 className="admin-membership-benefit-action-icon" size={15} strokeWidth={1.8} />}
                                                    aria-label={`移除权益 ${index + 1}`}
                                                    title="移除权益"
                                                    onClick={() => remove(field.name)}
                                                />
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </Form.List>
                    )}
                    <Form.Item className="admin-membership-form-item" name="enabled" label="上架" valuePropName="checked">
                        <Switch />
                    </Form.Item>
                </Form>
            </Modal>
            <Modal
                className="admin-operation-modal admin-membership-confirm-modal workspace-ui-scope"
                title="确认会员订单收款"
                open={Boolean(confirming)}
                closable={!processingOrder}
                maskClosable={!processingOrder}
                keyboard={!processingOrder}
                confirmLoading={processingOrder}
                onCancel={() => setConfirming(null)}
                onOk={() => void confirmOrder()}
                okText="确认并开通"
            >
                <div className="admin-membership-order-impact">
                    <span>订单 {confirming?.orderNumber ?? "--"}</span>
                    <strong>{confirming ? money(confirming.totalPriceCents) : "--"}</strong>
                    <p>提交后将立即开通订阅并发放对应积分，该操作应与真实渠道流水逐笔核对。</p>
                </div>
                <Form className="admin-membership-confirm-form" form={confirmForm} layout="vertical" disabled={processingOrder}>
                    <Form.Item name="providerTradeNo" label="支付流水号" rules={[{ required: true, message: "请输入真实支付流水号" }]}>
                        <Input />
                    </Form.Item>
                    <Form.Item name="note" label="核验备注" rules={[{ required: true }]}>
                        <Input.TextArea rows={3} />
                    </Form.Item>
                </Form>
            </Modal>
            <Modal
                className="admin-operation-modal admin-membership-close-modal workspace-ui-scope"
                title="关闭待支付订单"
                open={Boolean(closing)}
                closable={!processingOrder}
                maskClosable={!processingOrder}
                keyboard={!processingOrder}
                confirmLoading={processingOrder}
                onCancel={() => setClosing(null)}
                onOk={() => void closeOrder()}
                okText="确认关闭"
                okButtonProps={{ danger: true }}
            >
                <div className="admin-membership-order-impact is-danger">
                    <span>订单 {closing?.orderNumber ?? "--"}</span>
                    <strong>{closing ? money(closing.totalPriceCents) : "--"}</strong>
                    <p>关闭后该订单不能继续支付，处理原因会写入运营审计记录。</p>
                </div>
                <Form className="admin-membership-close-form" form={closeForm} layout="vertical" disabled={processingOrder}>
                    <Form.Item className="admin-membership-close-reason" name="note" label="关闭原因" rules={[{ required: true, message: "请输入关闭原因，便于后续审计" }]}>
                        <Input.TextArea className="admin-membership-close-input" rows={3} maxLength={500} showCount />
                    </Form.Item>
                </Form>
            </Modal>
        </AdminPageFrame>
    );
}

function billingCycleLabel(cycle: MembershipPlan["billingCycle"]) {
    if (cycle === "free") return "免费";
    if (cycle === "month") return "月付";
    if (cycle === "year") return "年付";
    return cycle;
}

function paymentStatusLabel(status: PaymentTransaction["status"]) {
    const labels: Record<PaymentTransaction["status"], string> = {
        created: "已创建",
        pending: "待支付",
        paid: "已支付",
        closed: "已关闭",
        failed: "失败",
        refunded: "已退款",
    };
    return labels[status];
}

function webhookStatusLabel(status: PaymentWebhookEvent["status"]) {
    const labels: Record<PaymentWebhookEvent["status"], string> = {
        received: "已接收",
        processed: "已处理",
        rejected: "已拒绝",
    };
    return labels[status];
}
