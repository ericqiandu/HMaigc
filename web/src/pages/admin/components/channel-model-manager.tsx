import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { App, Button, Drawer, Form, Input, Popconfirm, Select, Space, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { ArrowLeft, Image as ImageIcon, LockKeyhole, Plus, RefreshCw, Search, Trash2, Upload } from "lucide-react";
import { Link } from "react-router";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { createAdminChannelModel, deleteAdminChannelModel, fetchAdminChannelModels, listAdminChannelModels, removeAdminChannelModelIcon, updateAdminChannelModel, uploadAdminChannelModelIcon, type ChannelModel } from "@/services/api/wallet";
import type { ModelChannel } from "@/stores/use-config-store";

type FormValues = {
    modelKey: string;
    displayName?: string;
    marketingCopy?: string;
    promotionBadge?: string;
    accessPolicy: ChannelModel["accessPolicy"];
    capability: ChannelModel["capability"];
    enabled: boolean;
};

export function ChannelModelManager({ channel, onClose, onChanged }: { channel: ModelChannel; onClose: () => void; onChanged: () => void | Promise<void> }) {
    const { message } = App.useApp();
    const [items, setItems] = useState<ChannelModel[]>([]);
    const [editing, setEditing] = useState<ChannelModel | null>(null);
    const [loading, setLoading] = useState(false);
    const [fetching, setFetching] = useState(false);
    const [saving, setSaving] = useState(false);
    const [iconSaving, setIconSaving] = useState(false);
    const [editorOpen, setEditorOpen] = useState(false);
    const [keyword, setKeyword] = useState("");
    const [capability, setCapability] = useState<ChannelModel["capability"] | "all">("all");
    const [status, setStatus] = useState<"all" | "enabled" | "disabled">("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [form] = Form.useForm<FormValues>();
    const iconInputRef = useRef<HTMLInputElement>(null);

    const reload = async () => {
        if (!channel) return;
        setLoading(true);
        try {
            setItems((await listAdminChannelModels(channel.id)).models);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取渠道模型失败");
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void reload();
        setEditing(null);
        setEditorOpen(false);
        setKeyword("");
        setCapability("all");
        setStatus("all");
        setPage(1);
    }, [channel.id]);

    const fetchModels = async () => {
        setFetching(true);
        try {
            // 拉取只导入缺失项；新模型仍需管理员定价并手动启用。
            const result = await fetchAdminChannelModels(channel.id);
            await reload();
            await onChanged();
            if (result.models.length === 0) message.warning("上游没有返回可用模型");
            else if (result.added > 0) message.success(`已拉取 ${result.models.length} 个模型，新增 ${result.added} 个待配置模型`);
            else message.info(`已拉取 ${result.models.length} 个模型，没有需要新增的模型`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "拉取模型失败");
        } finally {
            setFetching(false);
        }
    };

    const startCreate = () => {
        setEditing(null);
        form.setFieldsValue({ modelKey: "", displayName: "", marketingCopy: "", promotionBadge: "", accessPolicy: "authenticated", capability: capabilityFromInterface(channel?.interfaceType), enabled: true });
        setEditorOpen(true);
    };

    const startEdit = (item: ChannelModel) => {
        setEditing(item);
        form.setFieldsValue({
            modelKey: item.modelKey,
            displayName: item.displayName,
            marketingCopy: item.marketingCopy,
            promotionBadge: item.promotionBadge,
            accessPolicy: item.accessPolicy,
            capability: item.capability,
            enabled: item.enabled,
        });
        setEditorOpen(true);
    };

    const save = async () => {
        const values = await form.validateFields();
        setSaving(true);
        try {
            const sharedPayload = {
                modelKey: values.modelKey.trim(),
                displayName: values.displayName?.trim() || values.modelKey.trim(),
                marketingCopy: values.marketingCopy?.trim() || "",
                promotionBadge: values.promotionBadge?.trim() || "",
                accessPolicy: values.accessPolicy,
                capability: values.capability,
                enabled: values.enabled !== false,
            };
            if (editing) {
                await updateAdminChannelModel(channel.id, editing.id, {
                    ...sharedPayload,
                    billingMode: editing.billingMode,
                    priceStrategy: editing.priceStrategy,
                    unitPriceMicrocredits: editing.unitPriceMicrocredits,
                    priceTiers: editing.priceTiers,
                    priceConfigured: editing.priceConfigured,
                });
            } else {
                await createAdminChannelModel(channel.id, {
                    ...sharedPayload,
                    billingMode: "fixed_request",
                    priceStrategy: "flat",
                    unitPriceMicrocredits: 0,
                    priceTiers: [],
                    priceConfigured: false,
                });
            }
            await reload();
            await onChanged();
            setEditorOpen(false);
            setEditing(null);
            message.success(editing ? "模型配置已更新" : "模型已添加");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存模型失败");
        } finally {
            setSaving(false);
        }
    };

    const selectIcon = async (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        event.target.value = "";
        if (!file || !editing) return;
        if (!["image/png", "image/jpeg", "image/webp"].includes(file.type)) {
            message.error("模型图标仅支持 PNG、JPG 或 WebP 格式");
            return;
        }
        if (file.size > 1024 * 1024) {
            message.error("模型图标文件大小不能超过 1MB");
            return;
        }
        setIconSaving(true);
        try {
            const result = await uploadAdminChannelModelIcon(channel.id, editing.id, file);
            setEditing(result.model);
            await reload();
            await onChanged();
            message.success("模型图标已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "上传模型图标失败");
        } finally {
            setIconSaving(false);
        }
    };

    const removeIcon = async () => {
        if (!editing) return;
        setIconSaving(true);
        try {
            const result = await removeAdminChannelModelIcon(channel.id, editing.id);
            setEditing(result.model);
            await reload();
            await onChanged();
            message.success("已移除模型自定义图标");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "移除模型图标失败");
        } finally {
            setIconSaving(false);
        }
    };

    const remove = async (item: ChannelModel) => {
        try {
            await deleteAdminChannelModel(channel.id, item.id);
            await reload();
            await onChanged();
            message.success("模型已删除");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "删除模型失败");
        }
    };

    const columns: ColumnsType<ChannelModel> = [
        {
            title: "模型",
            render: (_, item) => (
                <div className="channel-model-identity flex min-w-0 items-center gap-2.5">
                    <span className="channel-model-icon-preview grid size-9 shrink-0 place-items-center overflow-hidden rounded-lg bg-foreground/[.06]">
                        {item.iconUrl ? <img className="channel-model-icon-image size-5 object-contain" src={item.iconUrl} alt="" /> : <ImageIcon className="channel-model-icon-fallback size-4 text-foreground/40" />}
                    </span>
                    <div className="channel-model-copy min-w-0">
                        <div className="channel-model-name truncate font-medium">{item.displayName || item.modelKey}</div>
                        <div className="channel-model-key truncate text-xs text-foreground/45">{item.modelKey}</div>
                    </div>
                </div>
            ),
        },
        {
            title: "运营展示",
            width: 190,
            render: (_, item) => (
                <div className="channel-model-presentation min-w-0">
                    {item.promotionBadge ? <Tag className="channel-model-promotion-tag" color="gold">{item.promotionBadge}</Tag> : null}
                    <div className="channel-model-marketing-copy mt-1 truncate text-xs text-foreground/45" title={item.marketingCopy || undefined}>{item.marketingCopy || "未配置推广文案"}</div>
                </div>
            ),
        },
        { title: "访问", dataIndex: "accessPolicy", width: 110, render: (value) => value === "member" ? <Tag className="channel-model-member-tag" icon={<LockKeyhole className="channel-model-member-tag-icon size-3" />} color="gold">会员专属</Tag> : <Tag className="channel-model-public-tag">全部用户</Tag> },
        { title: "能力", dataIndex: "capability", width: 90, render: capabilityLabel },
        {
            title: "计费",
            width: 240,
            render: (_, item) => {
                if (!item.priceConfigured) return <Tag color="orange">未配置价格</Tag>;
                if (item.priceStrategy !== "flat") {
                    const unit = item.capability === "image" ? "张" : item.billingMode === "per_second" ? "秒" : "次";
                    return <span className="text-xs text-foreground/70">{item.priceTiers.map((tier) => `${tier.resolution.replace("SR_", "超分 ")} ${formatCredits(tier.unitPriceMicrocredits)}`).join(" · ")} 积分/{unit}</span>;
                }
                return `${formatCredits(item.unitPriceMicrocredits)} 积分 / ${item.billingMode === "per_second" ? "秒" : "次"}`;
            },
        },
        { title: "版本", dataIndex: "priceVersion", width: 75, render: (value) => `v${value}` },
        { title: "状态", dataIndex: "enabled", width: 85, render: (enabled) => (enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>) },
        {
            title: "操作",
            width: 120,
            render: (_, item) => (
                <Space>
                    <Button size="small" onClick={() => startEdit(item)}>编辑</Button>
                    <Popconfirm title="删除模型" description="删除后模型不再显示，历史账单仍会保留。该操作不能在页面恢复。" okText="删除" cancelText="取消" onConfirm={() => void remove(item)}>
                        <Button size="small" danger title="删除模型" aria-label="删除模型" icon={<Trash2 className="size-3.5" />} />
                    </Popconfirm>
                </Space>
            ),
        },
    ];

    const filteredItems = items.filter((item) => {
        const query = keyword.trim().toLowerCase();
        if (query && !`${item.modelKey} ${item.displayName} ${item.marketingCopy} ${item.promotionBadge}`.toLowerCase().includes(query)) return false;
        if (capability !== "all" && item.capability !== capability) return false;
        if (status === "enabled" && !item.enabled) return false;
        if (status === "disabled" && item.enabled) return false;
        return true;
    });

    return (
        <div className="channel-model-manager">
            <div className="flex flex-col gap-4 border-b border-border pb-5 sm:flex-row sm:items-end sm:justify-between">
                <div className="flex min-w-0 items-start gap-3">
                    <Button aria-label="返回 AI 模型配置" icon={<ArrowLeft className="size-4" />} onClick={onClose} />
                    <div className="min-w-0">
                        <h2 className="truncate text-lg font-semibold">{channel.name} / 模型管理</h2>
                        <p className="mt-1 text-xs text-foreground/50">维护模型标识、用户侧展示、能力与启用状态；成本和积分售价统一在商业定价中管理。</p>
                    </div>
                </div>
                <Space wrap>
                    <Link className="channel-model-pricing-link" to="/admin/model-pricing"><Button>商业定价</Button></Link>
                    <Button loading={fetching} icon={<RefreshCw className="size-4" />} onClick={() => void fetchModels()}>
                        拉取模型
                    </Button>
                    <Button type="primary" icon={<Plus className="size-4" />} onClick={startCreate}>
                        新增模型
                    </Button>
                </Space>
            </div>
            <ListToolbar active={Boolean(keyword || capability !== "all" || status !== "all")} onReset={() => { setKeyword(""); setCapability("all"); setStatus("all"); setPage(1); }}>
                <Input allowClear className="app-list-search" prefix={<Search className="size-4 text-foreground/40" />} value={keyword} placeholder="搜索模型、文案或角标" onChange={(event) => { setKeyword(event.target.value); setPage(1); }} />
                <Select className="w-32" value={capability} onChange={(value) => { setCapability(value); setPage(1); }} options={[{ label: "全部能力", value: "all" }, { label: "文本", value: "text" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }, { label: "音频", value: "audio" }]} />
                <Select className="w-32" value={status} onChange={(value) => { setStatus(value); setPage(1); }} options={[{ label: "全部状态", value: "all" }, { label: "已启用", value: "enabled" }, { label: "已停用", value: "disabled" }]} />
            </ListToolbar>
            <TableSurface>
                <Table
                    className="app-data-table"
                    rowKey="id"
                    size="middle"
                    loading={loading}
                    columns={columns}
                    dataSource={filteredItems}
                    pagination={{ current: page, pageSize, total: filteredItems.length, showSizeChanger: true, pageSizeOptions: [20, 50, 100], showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 个模型`, onChange: (nextPage, nextPageSize) => { setPage(nextPageSize !== pageSize ? 1 : nextPage); setPageSize(nextPageSize); } }}
                    scroll={{ x: 950 }}
                />
            </TableSurface>
            <Drawer title={editing ? "编辑模型" : "新增模型"} open={editorOpen} size="min(520px, 100vw)" onClose={() => setEditorOpen(false)} styles={{ body: { paddingBottom: 88 } }} extra={editing ? <Button size="small" icon={<Plus className="size-3.5" />} onClick={startCreate}>新增</Button> : null}>
                <Form form={form} layout="vertical" requiredMark={false}>
                    <Form.Item name="modelKey" label="模型标识" rules={[{ required: true, message: "请输入模型标识" }]}>
                        <Input placeholder="gpt-image-2" />
                    </Form.Item>
                    <Form.Item className="channel-model-display-name-field" name="displayName" label="显示名称">
                        <Input className="channel-model-display-name-input" placeholder="不填则使用模型标识" />
                    </Form.Item>
                    <Form.Item className="channel-model-marketing-copy-field" name="marketingCopy" label="悬浮介绍文案" extra="用户把鼠标停在模型选项上时显示；留空则不显示悬浮说明。">
                        <Input className="channel-model-marketing-copy-input" maxLength={120} showCount placeholder="例如：最强视频模型，会员专属通道，支持 15 秒音画同步" />
                    </Form.Item>
                    <Form.Item className="channel-model-promotion-badge-field" name="promotionBadge" label="促销角标" extra="仅作为运营展示，不会自动修改积分价格或活动有效期；活动结束后请及时清空。">
                        <Input className="channel-model-promotion-badge-input" maxLength={12} showCount placeholder="例如：限时4折" />
                    </Form.Item>
                    <Form.Item className="channel-model-access-field" name="accessPolicy" label="使用权限" extra="会员专属模型仅允许有效个人会员或有效团队席位成员调用；服务端会在实际生成前再次校验。" rules={[{ required: true, message: "请选择使用权限" }]}>
                        <Select className="channel-model-access-select" options={[{ label: "全部登录用户", value: "authenticated" }, { label: "有效会员专属", value: "member" }]} />
                    </Form.Item>
                    <div className="channel-model-icon-editor mb-6 bg-foreground/[.035] px-4 py-4">
                        <span className="channel-model-icon-editor-label mb-3 block text-sm font-medium text-foreground/85">模型图标</span>
                        <div className="channel-model-icon-editor-row flex items-center gap-3">
                            <span className="channel-model-icon-editor-preview grid size-14 shrink-0 place-items-center overflow-hidden rounded-lg bg-background">
                                {editing?.iconUrl ? <img className="channel-model-icon-editor-image size-8 object-contain" src={editing.iconUrl} alt="" /> : <ImageIcon className="channel-model-icon-editor-fallback size-5 text-foreground/35" />}
                            </span>
                            <div className="channel-model-icon-editor-actions min-w-0">
                                <input ref={iconInputRef} className="channel-model-icon-file-input !hidden" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => void selectIcon(event)} />
                                <Space className="channel-model-icon-buttons" wrap>
                                    <Button className="channel-model-icon-upload-button" disabled={!editing} loading={iconSaving} icon={<Upload className="channel-model-icon-upload-icon size-4" />} onClick={() => iconInputRef.current?.click()}>{editing?.iconUrl ? "替换" : "上传"}</Button>
                                    {editing?.iconUrl ? <Popconfirm title="移除模型图标" description="移除后前端会按模型名称显示内置图标或通用图标。" okText="移除" cancelText="取消" onConfirm={() => void removeIcon()}><Button className="channel-model-icon-remove-button" type="text" danger loading={iconSaving}>移除</Button></Popconfirm> : null}
                                </Space>
                                <span className="channel-model-icon-help mt-2 block text-[11px] leading-4 text-foreground/45">{editing ? "PNG、JPG 或 WebP，最大 1MB，建议使用透明底方形图标。" : "请先添加模型，再上传图标。"}</span>
                            </div>
                        </div>
                    </div>
                    <Form.Item name="capability" label="能力" rules={[{ required: true }]}>
                        <Select options={[{ label: "文本", value: "text" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }, { label: "音频", value: "audio" }]} />
                    </Form.Item>
                    <Form.Item name="enabled" label="启用" valuePropName="checked">
                        <Switch />
                    </Form.Item>
                    <Button type="primary" block loading={saving} onClick={() => void save()}>{editing ? "保存修改" : "添加模型"}</Button>
                </Form>
            </Drawer>
        </div>
    );
}

function capabilityFromInterface(value?: ModelChannel["interfaceType"]): ChannelModel["capability"] {
    if (value === "openai-image" || value === "apimart-image") return "image";
    if (value === "newapi" || value === "newapi-channel-1" || value === "newapi-channel-2" || value === "xai-video" || value === "ai-open-platform-video") return "video";
    return "text";
}

function capabilityLabel(value: ChannelModel["capability"]) {
    return { text: "文本", image: "图片", video: "视频", audio: "音频" }[value];
}

function formatCredits(value: number) {
    return (value / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 6 });
}
