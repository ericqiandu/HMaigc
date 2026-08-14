import { App, Button, Form, Input, InputNumber, Modal, Select, Switch, Table, Tabs, Tag } from "antd";
import { Plus, RefreshCw, SquarePen } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { TableSurface } from "@/components/layout/workspace-page";
import { AdminContentSection, AdminDataLayout } from "@/pages/admin/components/admin-data-layout";
import { AdminFormGrid, AdminFormIntro, AdminFormSection } from "@/pages/admin/components/admin-form-system";
import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { createAdminCreditTopupProduct, listAdminCreditTopupOrders, listAdminCreditTopupProducts, saveAdminCreditTopupProduct, type CreditTopupOrder, type CreditTopupProduct, type SaveCreditTopupProductInput } from "@/services/api/credit-store";

type ProductForm = SaveCreditTopupProductInput & { saleEndsAtText?: string };

const credits = (value: number) => (value / 1_000_000).toLocaleString("zh-CN");
const money = (value: number) => `¥${(value / 100).toLocaleString("zh-CN")}`;

export default function AdminCreditStorePage() {
    const { message } = App.useApp();
    const [form] = Form.useForm<ProductForm>();
    const [products, setProducts] = useState<CreditTopupProduct[]>([]);
    const [orders, setOrders] = useState<CreditTopupOrder[]>([]);
    const [editing, setEditing] = useState<CreditTopupProduct | null>(null);
    const [creating, setCreating] = useState(false);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const [productResult, orderResult] = await Promise.all([listAdminCreditTopupProducts(), listAdminCreditTopupOrders()]);
            setProducts(productResult.items);
            setOrders(orderResult.items);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "积分商城数据读取失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => {
        void load();
    }, [load]);

    const openEditor = (product: CreditTopupProduct) => {
        setCreating(false);
        setEditing(product);
        form.setFieldsValue({
            ...product,
            imageUrl: product.imageUrl ?? "",
            requiredMembershipTier: product.requiredMembershipTier ?? "origin",
            saleEndsAtText: product.saleEndsAt ? new Date(product.saleEndsAt).toISOString() : "",
        });
    };

    const openCreator = () => {
        setEditing(null);
        setCreating(true);
        form.setFieldsValue({
            code: "",
            name: "",
            category: "general",
            baseMicrocredits: 1_000_000,
            bonusMicrocredits: 0,
            priceCents: 100,
            originalPriceCents: 100,
            requiredMembershipTier: "origin",
            weeklyPurchaseLimit: 0,
            stockLimit: -1,
            badge: "",
            description: "到账积分长期有效",
            imageUrl: "",
            enabled: false,
            sortOrder: 100,
            saleEndsAtText: "",
        });
    };

    const save = async () => {
        if (!editing && !creating) return;
        const value = await form.validateFields();
        setSaving(true);
        try {
            const { saleEndsAtText, ...input } = value;
            const payload = { ...input, saleEndsAt: saleEndsAtText || undefined };
            if (editing) await saveAdminCreditTopupProduct(editing.id, payload);
            else await createAdminCreditTopupProduct(payload);
            message.success("积分商品已保存");
            setEditing(null);
            setCreating(false);
            await load();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "积分商品保存失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <AdminPageFrame title="积分商城" description="管理积分套餐、限购库存、会员门槛和充值订单">
            <AdminDataLayout>
                <AdminContentSection
                    className="admin-credit-store-content-section"
                    title="积分商品与订单"
                    description="集中维护积分套餐、库存与用户充值订单。"
                    actions={
                        <div className="admin-credit-store-toolbar flex justify-end gap-2">
                            <Button className="admin-credit-store-create" icon={<Plus aria-hidden="true" className="admin-credit-store-create-icon size-4" />} onClick={openCreator} type="primary">
                                新增套餐
                            </Button>
                            <Button className="admin-credit-store-refresh" icon={<RefreshCw aria-hidden="true" className="admin-credit-store-refresh-icon size-4" />} loading={loading} onClick={() => void load()}>
                                刷新
                            </Button>
                        </div>
                    }
                >
                    <Tabs
                        className="admin-credit-store-tabs"
                        items={[
                            {
                                key: "products",
                                label: "积分套餐",
                                children: (
                                    <TableSurface className="admin-credit-store-table-surface">
                                        <Table<CreditTopupProduct>
                                            className="admin-credit-store-products"
                                            dataSource={products}
                                            loading={loading}
                                            pagination={false}
                                            rowKey="id"
                                            columns={[
                                                {
                                                    title: "套餐",
                                                    render: (_, row) => (
                                                        <div className="admin-credit-product-name">
                                                            <strong className="admin-credit-product-title">{row.name}</strong>
                                                            <span className="admin-credit-product-code block text-xs text-foreground/50">{row.code}</span>
                                                        </div>
                                                    ),
                                                },
                                                { title: "分区", dataIndex: "category", width: 110 },
                                                { title: "到账积分", width: 130, render: (_, row) => <span className="admin-credit-product-credits tabular-nums">{credits(row.baseMicrocredits + row.bonusMicrocredits)}</span> },
                                                { title: "价格", width: 120, render: (_, row) => <span className="admin-credit-product-price tabular-nums">{money(row.priceCents)}</span> },
                                                { title: "库存", width: 110, render: (_, row) => <span className="admin-credit-product-stock">{row.stockLimit < 0 ? "不限" : `${row.soldCount}/${row.stockLimit}`}</span> },
                                                {
                                                    title: "状态",
                                                    width: 90,
                                                    render: (_, row) => (
                                                        <Tag className="admin-credit-product-status" color={row.enabled ? "green" : "default"}>
                                                            {row.enabled ? "上架" : "下架"}
                                                        </Tag>
                                                    ),
                                                },
                                                {
                                                    title: "操作",
                                                    width: 80,
                                                    render: (_, row) => (
                                                        <Button
                                                            aria-label={`编辑${row.name}`}
                                                            className="admin-credit-product-edit"
                                                            icon={<SquarePen aria-hidden="true" className="admin-credit-product-edit-icon size-4" />}
                                                            onClick={() => openEditor(row)}
                                                            type="text"
                                                        />
                                                    ),
                                                },
                                            ]}
                                        />
                                    </TableSurface>
                                ),
                            },
                            {
                                key: "orders",
                                label: "充值订单",
                                children: (
                                    <TableSurface className="admin-credit-store-table-surface">
                                        <Table<CreditTopupOrder>
                                            className="admin-credit-store-orders"
                                            dataSource={orders}
                                            loading={loading}
                                            pagination={false}
                                            rowKey="id"
                                            columns={[
                                                { title: "订单号", dataIndex: "orderNumber" },
                                                { title: "用户", dataIndex: "userId", ellipsis: true },
                                                { title: "到账积分", width: 130, render: (_, row) => <span className="admin-credit-order-credits tabular-nums">{credits(row.totalMicrocredits)}</span> },
                                                { title: "金额", width: 110, render: (_, row) => <span className="admin-credit-order-price tabular-nums">{money(row.totalPriceCents)}</span> },
                                                {
                                                    title: "状态",
                                                    width: 100,
                                                    render: (_, row) => (
                                                        <Tag className="admin-credit-order-status" color={row.status === "paid" ? "green" : row.status === "pending" ? "gold" : "default"}>
                                                            {row.status}
                                                        </Tag>
                                                    ),
                                                },
                                                {
                                                    title: "创建时间",
                                                    width: 180,
                                                    render: (_, row) => (
                                                        <time className="admin-credit-order-time" dateTime={row.createdAt}>
                                                            {new Date(row.createdAt).toLocaleString("zh-CN")}
                                                        </time>
                                                    ),
                                                },
                                            ]}
                                        />
                                    </TableSurface>
                                ),
                            },
                        ]}
                    />
                </AdminContentSection>
            </AdminDataLayout>
            <Modal
                className="admin-operation-modal admin-credit-product-modal workspace-ui-scope"
                confirmLoading={saving}
                destroyOnHidden
                onCancel={() => {
                    setEditing(null);
                    setCreating(false);
                }}
                onOk={() => void save()}
                open={Boolean(editing) || creating}
                title={creating ? "新增积分套餐" : "编辑积分套餐"}
                width={780}
            >
                <Form className="admin-credit-product-form admin-form-stack" form={form} layout="vertical">
                    <AdminFormIntro title="积分套餐" description="统一维护商品身份、积分价值和销售约束；修改后仅影响新订单。" />
                    <AdminFormSection title="商品信息" description="用户购买时展示的名称与分区。">
                        <AdminFormGrid>
                            <Form.Item className="admin-credit-product-field" label="商品编码" name="code" rules={[{ required: true }]}>
                                <Input className="admin-credit-product-input" />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="商品名称" name="name" rules={[{ required: true }]}>
                                <Input className="admin-credit-product-input" />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="分区" name="category" rules={[{ required: true }]}>
                                <Select
                                    className="admin-credit-product-select"
                                    options={[
                                        { value: "surprise", label: "惊喜专区" },
                                        { value: "general", label: "通用积分卡" },
                                    ]}
                                />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="最低会员" name="requiredMembershipTier">
                                <Select
                                    className="admin-credit-product-select"
                                    options={[
                                        { value: "origin", label: "基础版" },
                                        { value: "pro", label: "标准版" },
                                        { value: "max", label: "高级版" },
                                        { value: "ultra", label: "至尊版" },
                                    ]}
                                />
                            </Form.Item>
                        </AdminFormGrid>
                    </AdminFormSection>
                    <AdminFormSection title="积分与价格" description="积分使用微积分、价格使用人民币分。">
                        <AdminFormGrid>
                            <Form.Item className="admin-credit-product-field" label="基础积分（微积分）" name="baseMicrocredits" rules={[{ required: true }]}>
                                <InputNumber className="admin-credit-product-number" min={1} />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="赠送积分（微积分）" name="bonusMicrocredits">
                                <InputNumber className="admin-credit-product-number" min={0} />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="售价（分）" name="priceCents" rules={[{ required: true }]}>
                                <InputNumber className="admin-credit-product-number" min={1} />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="原价（分）" name="originalPriceCents" rules={[{ required: true }]}>
                                <InputNumber className="admin-credit-product-number" min={1} />
                            </Form.Item>
                        </AdminFormGrid>
                    </AdminFormSection>
                    <AdminFormSection title="销售规则" description="限制购买频率、库存和活动时间。">
                        <AdminFormGrid>
                            <Form.Item className="admin-credit-product-field" label="每周限购（0 不限）" name="weeklyPurchaseLimit">
                                <InputNumber className="admin-credit-product-number" min={0} />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="库存（-1 不限）" name="stockLimit">
                                <InputNumber className="admin-credit-product-number" min={-1} />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="排序" name="sortOrder">
                                <InputNumber className="admin-credit-product-number" />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="活动结束时间（ISO 8601）" name="saleEndsAtText">
                                <Input className="admin-credit-product-input" placeholder="2026-12-31T15:59:59.000Z" />
                            </Form.Item>
                        </AdminFormGrid>
                    </AdminFormSection>
                    <AdminFormSection title="展示与状态" description="配置商城角标、封面和上架状态。">
                        <AdminFormGrid>
                            <Form.Item className="admin-credit-product-field" label="角标" name="badge">
                                <Input className="admin-credit-product-input" />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field" label="图片 URL" name="imageUrl">
                                <Input className="admin-credit-product-input" />
                            </Form.Item>
                            <Form.Item className="admin-credit-product-field is-full" label="说明" name="description">
                                <Input.TextArea className="admin-credit-product-textarea" rows={3} />
                            </Form.Item>
                            <div className="admin-form-switch-row is-full">
                                <span className="admin-form-note">允许用户在积分商城购买此套餐</span>
                                <Form.Item className="admin-credit-product-field" name="enabled" valuePropName="checked">
                                    <Switch className="admin-credit-product-switch" aria-label="积分套餐上架状态" />
                                </Form.Item>
                            </div>
                        </AdminFormGrid>
                    </AdminFormSection>
                </Form>
            </Modal>
        </AdminPageFrame>
    );
}
