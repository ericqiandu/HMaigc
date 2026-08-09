import { App, Button, DatePicker, Form, Input, Switch } from "antd";
import dayjs, { type Dayjs } from "dayjs";
import { Plus, RefreshCw, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { AdminContentError, AdminContentSkeleton } from "@/pages/admin/components/admin-ui";
import { getAdminMembershipStorefront, type MembershipStorefrontSetting, updateAdminMembershipStorefront } from "@/services/api/membership";
import { generationSectionsFromForm, generationSectionsToForm, type StorefrontGenerationSectionForm } from "./membership-storefront-form-domain";

type StorefrontFormValues = Omit<MembershipStorefrontSetting, "promotion" | "generationSections"> & {
    promotion: Omit<MembershipStorefrontSetting["promotion"], "endsAt"> & { endsAt: Dayjs };
    generationSections: StorefrontGenerationSectionForm[];
};

const requiredRule = { required: true, message: "此项不能为空" } as const;

function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
    return <Button aria-label={label} className="admin-storefront-remove-button" danger icon={<Trash2 className="admin-storefront-remove-icon size-4" />} onClick={onClick} title={label} type="text" />;
}

export function MembershipStorefrontAdminPanel() {
    const { message } = App.useApp();
    const [form] = Form.useForm<StorefrontFormValues>();
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [dirty, setDirty] = useState(false);
    const [error, setError] = useState("");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const storefront = await getAdminMembershipStorefront();
            form.setFieldsValue({
                ...storefront.presentation,
                promotion: {
                    ...storefront.presentation.promotion,
                    endsAt: dayjs(storefront.presentation.promotion.endsAt),
                },
                generationSections: generationSectionsToForm(storefront.presentation.generationSections),
            });
            setDirty(false);
        } catch (loadError) {
            setError(loadError instanceof Error ? loadError.message : "会员商城配置加载失败");
        } finally {
            setLoading(false);
        }
    }, [form]);

    useEffect(() => {
        void load();
    }, [load]);

    const save = async () => {
        setSaving(true);
        try {
            const values = await form.validateFields();
            const updated = await updateAdminMembershipStorefront({
                ...values,
                promotion: { ...values.promotion, endsAt: values.promotion.endsAt.toISOString() },
                generationSections: generationSectionsFromForm(values.generationSections),
            });
            form.setFieldsValue({
                ...updated.presentation,
                promotion: { ...updated.presentation.promotion, endsAt: dayjs(updated.presentation.promotion.endsAt) },
                generationSections: generationSectionsToForm(updated.presentation.generationSections),
            });
            setDirty(false);
            message.success("会员商城展示配置已生效");
        } catch (saveError) {
            if (saveError instanceof Error) message.error(saveError.message || "会员商城配置保存失败");
        } finally {
            setSaving(false);
        }
    };

    if (loading) return <AdminContentSkeleton rows={10} label="正在加载会员商城配置" />;
    if (error) return <AdminContentError title="会员商城配置加载失败" description={error} onRetry={() => void load()} />;

    return (
        <section className="admin-storefront-panel" aria-label="会员商城展示配置">
            <header className="admin-storefront-header">
                <div className="admin-storefront-heading">
                    <strong className="admin-storefront-title">前台展示参数</strong>
                    <span className="admin-storefront-description">这里的内容直接驱动个人会员和团队会员购买页；价格、积分、席位与资源权益仍在“套餐与权益”中维护。</span>
                </div>
                <div className="admin-storefront-actions">
                    <Button className="admin-storefront-refresh" disabled={saving} icon={<RefreshCw className="admin-storefront-action-icon size-4" />} onClick={() => void load()}>
                        重新读取
                    </Button>
                    <Button className="admin-storefront-save" disabled={!dirty} icon={<Save className="admin-storefront-action-icon size-4" />} loading={saving} onClick={() => void save()} type="primary">
                        保存并生效
                    </Button>
                </div>
            </header>

            <Form className="admin-storefront-form" disabled={saving} form={form} layout="vertical" onValuesChange={() => setDirty(true)}>
                <section className="admin-storefront-section" aria-labelledby="storefront-promotion-title">
                    <h3 className="admin-storefront-section-title" id="storefront-promotion-title">
                        活动横幅与倒计时
                    </h3>
                    <div className="admin-storefront-switch-row">
                        <Form.Item className="admin-storefront-field" label="启用活动横幅" name={["promotion", "enabled"]} valuePropName="checked">
                            <Switch className="admin-storefront-switch" />
                        </Form.Item>
                    </div>
                    <div className="admin-storefront-grid">
                        <Form.Item className="admin-storefront-field" label="活动标题" name={["promotion", "title"]} rules={[requiredRule]}>
                            <Input className="admin-storefront-input" maxLength={160} />
                        </Form.Item>
                        <Form.Item className="admin-storefront-field" label="活动截止时间" name={["promotion", "endsAt"]} rules={[requiredRule]}>
                            <DatePicker className="admin-storefront-date" showTime />
                        </Form.Item>
                        <Form.Item className="admin-storefront-field" label="副标题" name={["promotion", "subtitle"]} rules={[requiredRule]}>
                            <Input className="admin-storefront-input" maxLength={120} />
                        </Form.Item>
                        <Form.Item className="admin-storefront-field" label="强调文案" name={["promotion", "subtitleHighlight"]} rules={[requiredRule]}>
                            <Input className="admin-storefront-input" maxLength={80} />
                        </Form.Item>
                    </div>
                </section>

                <section className="admin-storefront-section" aria-labelledby="storefront-copy-title">
                    <h3 className="admin-storefront-section-title" id="storefront-copy-title">
                        界面文案
                    </h3>
                    <div className="admin-storefront-grid is-compact">
                        {(
                            [
                                ["creatorTab", "个人会员页签"],
                                ["teamTab", "团队会员页签"],
                                ["yearCycle", "年付周期"],
                                ["monthCycle", "月付周期"],
                                ["creditStore", "积分超市入口"],
                                ["activityHeading", "活动标题"],
                                ["exclusiveHeading", "独家功能标题"],
                                ["generationHeading", "生成数量标题"],
                                ["faqHeading", "常见问题标题"],
                            ] as const
                        ).map(([key, label]) => (
                            <Form.Item className="admin-storefront-field" key={key} label={label} name={["copy", key]} rules={[requiredRule]}>
                                <Input className="admin-storefront-input" maxLength={40} />
                            </Form.Item>
                        ))}
                    </div>
                </section>

                <section className="admin-storefront-section" aria-labelledby="storefront-activity-title">
                    <h3 className="admin-storefront-section-title" id="storefront-activity-title">
                        限时活动
                    </h3>
                    <Form.List name="activities">
                        {(fields, { add, remove }) => (
                            <div className="admin-storefront-list">
                                {fields.map((field) => (
                                    <div className="admin-storefront-list-row is-activity" key={field.key}>
                                        <Form.Item className="admin-storefront-icon-field" name={[field.name, "icon"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" maxLength={8} placeholder="图标" />
                                        </Form.Item>
                                        <Form.Item className="admin-storefront-grow-field" name={[field.name, "text"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" maxLength={100} placeholder="活动说明" />
                                        </Form.Item>
                                        <RemoveButton label="移除活动" onClick={() => remove(field.name)} />
                                    </div>
                                ))}
                                <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => add({ icon: "✦", text: "" })}>
                                    添加活动
                                </Button>
                            </div>
                        )}
                    </Form.List>
                </section>

                <div className="admin-storefront-two-columns">
                    <StringListEditor label="公共权益" name="commonFeatures" />
                    <StringListEditor label="独家功能" name="exclusiveFeatures" />
                </div>

                <section className="admin-storefront-section" aria-labelledby="storefront-highlight-title">
                    <h3 className="admin-storefront-section-title" id="storefront-highlight-title">
                        套餐卡生成能力摘要
                    </h3>
                    <p className="admin-storefront-help">层级标识必须覆盖所有已上架套餐，例如 origin、pro、max、ultra。</p>
                    <Form.List name="planHighlights">
                        {(fields, { add, remove }) => (
                            <div className="admin-storefront-list">
                                {fields.map((field) => (
                                    <div className="admin-storefront-list-row is-highlight" key={field.key}>
                                        <Form.Item className="admin-storefront-tier-field" name={[field.name, "tier"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" maxLength={80} placeholder="层级标识" />
                                        </Form.Item>
                                        <Form.Item className="admin-storefront-grow-field" name={[field.name, "images"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" maxLength={80} placeholder="图片估算" />
                                        </Form.Item>
                                        <Form.Item className="admin-storefront-grow-field" name={[field.name, "videos"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" maxLength={80} placeholder="视频估算" />
                                        </Form.Item>
                                        <RemoveButton label="移除套餐摘要" onClick={() => remove(field.name)} />
                                    </div>
                                ))}
                                <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => add({ tier: "", images: "", videos: "" })}>
                                    添加摘要
                                </Button>
                            </div>
                        )}
                    </Form.List>
                </section>

                <section className="admin-storefront-section" aria-labelledby="storefront-matrix-title">
                    <h3 className="admin-storefront-section-title" id="storefront-matrix-title">
                        生成数量对照表
                    </h3>
                    <p className="admin-storefront-help">每个模型行按列顺序每行填写一个值；数值数量必须与列数完全一致，保存时服务端会严格校验。</p>
                    <Form.List name="generationColumns">
                        {(fields, { add, remove }) => (
                            <div className="admin-storefront-list">
                                {fields.map((field) => (
                                    <div className="admin-storefront-list-row" key={field.key}>
                                        <Form.Item className="admin-storefront-tier-field" name={[field.name, "key"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" placeholder="列标识" />
                                        </Form.Item>
                                        <Form.Item className="admin-storefront-grow-field" name={[field.name, "label"]} rules={[requiredRule]}>
                                            <Input className="admin-storefront-input" placeholder="前台列名" />
                                        </Form.Item>
                                        <RemoveButton label="移除生成数量列" onClick={() => remove(field.name)} />
                                    </div>
                                ))}
                                <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => add({ key: "", label: "" })}>
                                    添加列
                                </Button>
                            </div>
                        )}
                    </Form.List>
                    <Form.List name="generationSections">
                        {(sections, { add: addSection, remove: removeSection }) => (
                            <div className="admin-storefront-matrix-sections">
                                {sections.map((section) => (
                                    <div className="admin-storefront-matrix-section" key={section.key}>
                                        <div className="admin-storefront-list-row">
                                            <Form.Item className="admin-storefront-grow-field" label="模型分组" name={[section.name, "title"]} rules={[requiredRule]}>
                                                <Input className="admin-storefront-input" />
                                            </Form.Item>
                                            <RemoveButton label="移除模型分组" onClick={() => removeSection(section.name)} />
                                        </div>
                                        <Form.List name={[section.name, "rows"]}>
                                            {(rows, { add: addRow, remove: removeRow }) => (
                                                <div className="admin-storefront-list">
                                                    {rows.map((row) => (
                                                        <div className="admin-storefront-matrix-row" key={row.key}>
                                                            <Form.Item className="admin-storefront-matrix-model" name={[row.name, "model"]} rules={[requiredRule]}>
                                                                <Input className="admin-storefront-input" placeholder="模型名称" />
                                                            </Form.Item>
                                                            <Form.Item className="admin-storefront-icon-field" name={[row.name, "icon"]} rules={[requiredRule]}>
                                                                <Input className="admin-storefront-input" placeholder="图标" />
                                                            </Form.Item>
                                                            <Form.Item className="admin-storefront-unit-field" name={[row.name, "unit"]} rules={[requiredRule]}>
                                                                <Input className="admin-storefront-input" placeholder="单位" />
                                                            </Form.Item>
                                                            <Form.Item className="admin-storefront-grow-field" name={[row.name, "values"]} rules={[requiredRule]}>
                                                                <Input.TextArea className="admin-storefront-textarea" placeholder="每行一个值，可保留数值中的千位逗号" rows={3} />
                                                            </Form.Item>
                                                            <RemoveButton label="移除模型行" onClick={() => removeRow(row.name)} />
                                                        </div>
                                                    ))}
                                                    <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => addRow({ model: "", icon: "✦", unit: "次", values: "" })}>
                                                        添加模型行
                                                    </Button>
                                                </div>
                                            )}
                                        </Form.List>
                                    </div>
                                ))}
                                <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => addSection({ title: "", rows: [] })}>
                                    添加模型分组
                                </Button>
                            </div>
                        )}
                    </Form.List>
                    <Form.Item className="admin-storefront-field mt-4" label="生成数量说明" name="generationFootnote" rules={[requiredRule]}>
                        <Input.TextArea className="admin-storefront-textarea" maxLength={300} rows={2} />
                    </Form.Item>
                </section>

                <div className="admin-storefront-two-columns">
                    <StringListEditor label="会员规则说明" name="membershipNotes" multiline />
                    <section className="admin-storefront-section" aria-labelledby="storefront-faq-title">
                        <h3 className="admin-storefront-section-title" id="storefront-faq-title">
                            常见问题
                        </h3>
                        <Form.List name="faqs">
                            {(fields, { add, remove }) => (
                                <div className="admin-storefront-list">
                                    {fields.map((field) => (
                                        <div className="admin-storefront-faq-row" key={field.key}>
                                            <Form.Item className="admin-storefront-grow-field" name={[field.name, "question"]} rules={[requiredRule]}>
                                                <Input className="admin-storefront-input" maxLength={120} placeholder="问题" />
                                            </Form.Item>
                                            <Form.Item className="admin-storefront-grow-field" name={[field.name, "answer"]} rules={[requiredRule]}>
                                                <Input.TextArea className="admin-storefront-textarea" maxLength={1000} placeholder="答案" rows={2} />
                                            </Form.Item>
                                            <RemoveButton label="移除常见问题" onClick={() => remove(field.name)} />
                                        </div>
                                    ))}
                                    <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => add({ question: "", answer: "" })}>
                                        添加常见问题
                                    </Button>
                                </div>
                            )}
                        </Form.List>
                    </section>
                </div>
            </Form>
        </section>
    );
}

function StringListEditor({ label, name, multiline = false }: { label: string; name: "commonFeatures" | "exclusiveFeatures" | "membershipNotes"; multiline?: boolean }) {
    return (
        <section className="admin-storefront-section" aria-label={label}>
            <h3 className="admin-storefront-section-title">{label}</h3>
            <Form.List name={name}>
                {(fields, { add, remove }) => (
                    <div className="admin-storefront-list">
                        {fields.map((field) => (
                            <div className="admin-storefront-list-row" key={field.key}>
                                <Form.Item className="admin-storefront-grow-field" name={field.name} rules={[requiredRule]}>
                                    {multiline ? <Input.TextArea className="admin-storefront-textarea" maxLength={300} rows={2} /> : <Input className="admin-storefront-input" maxLength={120} />}
                                </Form.Item>
                                <RemoveButton label={`移除${label}`} onClick={() => remove(field.name)} />
                            </div>
                        ))}
                        <Button className="admin-storefront-add-button" icon={<Plus className="admin-storefront-add-icon size-4" />} onClick={() => add("")}>
                            添加{label}
                        </Button>
                    </div>
                )}
            </Form.List>
        </section>
    );
}
