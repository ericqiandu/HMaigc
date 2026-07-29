import { App, Button, Empty, Form, Input, Modal, Select, Tag } from "antd";
import { ChevronDown, FileText } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

import { createInvoiceRequest, listMyInvoiceRequests, type InvoiceRequest, type MembershipOrder, type MembershipPlan } from "@/services/api/membership";

type InvoiceForm = {
    membershipOrderId: string;
    title: string;
    taxNumber?: string;
    email: string;
};

type MembershipInvoiceCenterProps = {
    email: string;
    orders: MembershipOrder[];
    plansById: Map<string, MembershipPlan>;
};

export function MembershipInvoiceCenter({ email, orders, plansById }: MembershipInvoiceCenterProps) {
    const { message } = App.useApp();
    const [items, setItems] = useState<InvoiceRequest[]>([]);
    const [loading, setLoading] = useState(false);
    const [open, setOpen] = useState(false);
    const [form] = Form.useForm<InvoiceForm>();

    const load = useCallback(async () => {
        setLoading(true);
        try {
            const response = await listMyInvoiceRequests();
            setItems(response.items);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "开票记录加载失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => {
        void load();
    }, [load]);

    const requestedOrderIds = useMemo(() => new Set(items.map((item) => item.membershipOrderId)), [items]);
    const eligibleOrders = useMemo(() => orders.filter((order) => order.status === "paid" && plansById.get(order.planId)?.invoicingEnabled && !requestedOrderIds.has(order.id)), [orders, plansById, requestedOrderIds]);

    const showCreate = () => {
        form.setFieldsValue({
            membershipOrderId: eligibleOrders[0]?.id,
            title: "",
            taxNumber: "",
            email,
        });
        setOpen(true);
    };

    const submit = async () => {
        const values = await form.validateFields();
        setLoading(true);
        try {
            await createInvoiceRequest({
                membershipOrderId: values.membershipOrderId,
                title: values.title.trim(),
                taxNumber: values.taxNumber?.trim(),
                email: values.email.trim(),
            });
            setOpen(false);
            await load();
            message.success("开票申请已提交");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "提交开票申请失败");
        } finally {
            setLoading(false);
        }
    };

    return (
        <details className="membership-invoices membership-orders-section">
            <summary className="membership-invoices-summary">
                <span className="membership-invoices-summary-main">
                    <FileText className="membership-invoices-summary-icon" />
                    <span className="membership-invoices-summary-copy">
                        <strong className="membership-invoices-title">发票中心</strong>
                        <small className="membership-invoices-description">仅支持包含开票权益的已支付订单</small>
                    </span>
                </span>
                <span className="membership-invoices-summary-side">
                    <span className="membership-invoices-count">{items.length} 条</span>
                    <ChevronDown className="membership-invoices-chevron" />
                </span>
            </summary>
            <div className="membership-invoices-content">
                <div className="membership-invoices-toolbar">
                    <span className="membership-invoices-toolbar-note">申请状态与电子发票文件均可追溯。</span>
                    <Button className="membership-invoice-create-button" type="primary" size="small" disabled={!eligibleOrders.length} onClick={showCreate}>
                        申请发票
                    </Button>
                </div>
                {items.length ? (
                    <div className="membership-invoice-list">
                        {items.map((item) => (
                            <article className="membership-invoice-row" key={item.id}>
                                <div className="membership-invoice-primary">
                                    <strong className="membership-invoice-title">{item.title}</strong>
                                    <span className="membership-invoice-meta">
                                        ¥{(item.amountCents / 100).toLocaleString("zh-CN")} · {new Date(item.createdAt).toLocaleString("zh-CN")}
                                    </span>
                                </div>
                                <Tag className="membership-invoice-status" color={item.status === "issued" ? "green" : item.status === "rejected" ? "red" : "gold"}>
                                    {{ pending: "待处理", issued: "已开具", rejected: "已驳回" }[item.status]}
                                </Tag>
                                {item.invoiceUrl ? (
                                    <a className="membership-invoice-download" href={item.invoiceUrl} target="_blank" rel="noreferrer">
                                        查看发票
                                    </a>
                                ) : (
                                    <span className="membership-invoice-note">{item.resolutionNote || "—"}</span>
                                )}
                            </article>
                        ))}
                    </div>
                ) : (
                    <Empty className="membership-invoices-empty" image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无开票记录" />
                )}
            </div>

            <Modal className="membership-invoice-modal" title="申请电子发票" open={open} okText="提交申请" confirmLoading={loading} onOk={() => void submit()} onCancel={() => setOpen(false)} destroyOnHidden>
                <Form className="membership-invoice-form" form={form} layout="vertical">
                    <Form.Item className="membership-invoice-order-field" name="membershipOrderId" label="已支付订单" rules={[{ required: true, message: "请选择订单" }]}>
                        <Select
                            className="membership-invoice-order-select"
                            options={eligibleOrders.map((order) => ({
                                label: `${order.orderNumber} · ¥${(order.totalPriceCents / 100).toLocaleString("zh-CN")}`,
                                value: order.id,
                            }))}
                        />
                    </Form.Item>
                    <Form.Item className="membership-invoice-title-field" name="title" label="发票抬头" rules={[{ required: true, message: "请输入发票抬头" }]}>
                        <Input className="membership-invoice-title-input" maxLength={200} />
                    </Form.Item>
                    <Form.Item className="membership-invoice-tax-field" name="taxNumber" label="纳税人识别号">
                        <Input className="membership-invoice-tax-input" maxLength={80} />
                    </Form.Item>
                    <Form.Item className="membership-invoice-email-field" name="email" label="接收邮箱" rules={[{ required: true }, { type: "email", message: "请输入有效邮箱" }]}>
                        <Input className="membership-invoice-email-input" />
                    </Form.Item>
                </Form>
            </Modal>
        </details>
    );
}
