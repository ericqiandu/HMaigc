import { App, Button, Drawer, Form, Input, InputNumber, Select, Segmented, Table, Tag } from "antd";
import type { FormInstance } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CircleDollarSign, Pencil, Search, Settings2, TriangleAlert } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import {
    createAdminModelPricing,
    getAdminModelPricingOperationsSetting,
    listAdminModelPricings,
    updateAdminModelPricing,
    updateAdminModelPricingOperationsSetting,
    type ModelPricing,
    type ModelPricingInput,
    type ModelPricingOperationsSetting,
} from "@/services/api/auth";
import { listAdminChannelModels, updateAdminChannelModel, type ChannelModel } from "@/services/api/wallet";
import { useAdminContext } from "../admin-context";
import { AdminPageFrame } from "../components/admin-shell";
import {
    imagePricingSpecifications,
    specificationsForStrategy,
    type PricingSpecification,
} from "./pricing-specifications";

type CommercialModel = ChannelModel & { channelName: string; pricing?: ModelPricing };
type PricingFormValues = {
    currency: string;
    billingMode: ChannelModel["billingMode"];
    priceStrategy: ChannelModel["priceStrategy"];
    unitCredits?: number;
    inputPerMillion?: number;
    outputPerMillion?: number;
    cachedPerMillion?: number;
    expectedInputTokens?: number;
    expectedOutputTokens?: number;
    expectedCachedTokens?: number;
    perRequest?: number;
    perMedia?: number;
    perVideoSecond?: number;
    tierCosts?: Record<string, number>;
    tierCredits?: Record<string, number>;
};
type SettingsFormValues = { currency: string; creditRevenue: number; targetMarginPercent: number };

const emptySetting: ModelPricingOperationsSetting = { configured: false, currency: "", creditRevenueMicros: 0, targetMarginBasisPoints: 0 };
const pricingScopeKey = (channelId: string | undefined, model: string, capability: ModelPricing["capability"]) =>
    `${(channelId || "").trim()}:${model.trim()}:${capability.trim().toLowerCase()}`;

export default function ModelPricingPage() {
    const { message } = App.useApp();
    const { references } = useAdminContext();
    const [models, setModels] = useState<CommercialModel[]>([]);
    const [setting, setSetting] = useState<ModelPricingOperationsSetting>(emptySetting);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [editing, setEditing] = useState<CommercialModel | null>(null);
    const [settingsOpen, setSettingsOpen] = useState(false);
    const [keyword, setKeyword] = useState("");
    const [capability, setCapability] = useState<ChannelModel["capability"] | "all">("all");
    const [status, setStatus] = useState<"all" | "configured" | "incomplete" | "warning">("all");
    const [pricingForm] = Form.useForm<PricingFormValues>();
    const [settingsForm] = Form.useForm<SettingsFormValues>();
    const priceStrategy = Form.useWatch("priceStrategy", pricingForm) || "flat";

    const reload = async () => {
        setLoading(true);
        try {
            const [pricingResult, settingResult, ...channelResults] = await Promise.all([
                listAdminModelPricings(),
                getAdminModelPricingOperationsSetting(),
                ...references.channels.map((channel) => listAdminChannelModels(channel.id)),
            ]);
            const pricingByModel = new Map(pricingResult.pricings.map((item) => [pricingScopeKey(item.channelId, item.model, item.capability), item]));
            const nextModels = channelResults.flatMap((result, index) => {
                const channel = references.channels[index];
                return result.models.map((model) => ({
                    ...model,
                    channelName: channel.name,
                    pricing: pricingByModel.get(pricingScopeKey(channel.id, model.modelKey, model.capability)),
                }));
            });
            setModels(nextModels);
            setSetting(settingResult.setting);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取模型商业定价失败");
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void reload();
    }, [references.channels]);

    const openPricing = (model: CommercialModel) => {
        const pricing = model.pricing;
        setEditing(model);
        pricingForm.setFieldsValue({
            currency: pricing?.currency || setting.currency,
            billingMode: model.billingMode,
            priceStrategy: model.priceStrategy,
            unitCredits: model.priceConfigured && model.priceStrategy === "flat" ? fromMicro(model.unitPriceMicrocredits) : undefined,
            inputPerMillion: optionalMoney(pricing?.inputPerMillionMicros),
            outputPerMillion: optionalMoney(pricing?.outputPerMillionMicros),
            cachedPerMillion: optionalMoney(pricing?.cachedPerMillionMicros),
            expectedInputTokens: optionalCount(pricing?.expectedInputTokens),
            expectedOutputTokens: optionalCount(pricing?.expectedOutputTokens),
            expectedCachedTokens: optionalCount(pricing?.expectedCachedTokens),
            perRequest: optionalMoney(pricing?.perRequestMicros),
            perMedia: optionalMoney(pricing?.perMediaMicros),
            perVideoSecond: optionalMoney(pricing?.perVideoSecondMicros),
            tierCosts: Object.fromEntries(pricing?.tiers.map((tier) => [tier.specification, fromMicro(tier.supplierCostMicros)]) || []),
            tierCredits: Object.fromEntries(model.priceTiers.map((tier) => [tier.resolution, fromMicro(tier.unitPriceMicrocredits)])),
        });
    };

    const openSettings = () => {
        settingsForm.setFieldsValue({
            currency: setting.currency,
            creditRevenue: fromMicro(setting.creditRevenueMicros),
            targetMarginPercent: setting.targetMarginBasisPoints / 100,
        });
        setSettingsOpen(true);
    };

    const saveSettings = async () => {
        const values = await settingsForm.validateFields();
        setSaving(true);
        try {
            const result = await updateAdminModelPricingOperationsSetting({
                currency: values.currency.trim().toUpperCase(),
                creditRevenueMicros: toMicro(values.creditRevenue),
                targetMarginBasisPoints: Math.round(values.targetMarginPercent * 100),
            });
            setSetting(result.setting);
            setSettingsOpen(false);
            message.success("商业定价基准已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存商业定价基准失败");
        } finally {
            setSaving(false);
        }
    };

    const savePricing = async () => {
        if (!editing) return;
        const values = await pricingForm.validateFields();
        const currency = values.currency.trim().toUpperCase();
        const specifications = specificationsForStrategy(values.priceStrategy);
        const incompleteSpecification = specifications.find((specification) => {
            const supplierCost = values.tierCosts?.[specification.key];
            const userCredits = values.tierCredits?.[specification.key];
            return (supplierCost !== undefined || userCredits !== undefined)
                && (supplierCost === undefined || userCredits === undefined || supplierCost <= 0 || userCredits <= 0);
        });
        if (incompleteSpecification) {
            message.error(`${incompleteSpecification.label} 的供应商成本和用户积分必须同时配置且大于 0`);
            return;
        }
        const tierInputs = specifications.flatMap((specification) => {
            const supplierCost = values.tierCosts?.[specification.key];
            const userCredits = values.tierCredits?.[specification.key];
            if (supplierCost === undefined && userCredits === undefined) return [];
            if (supplierCost === undefined || userCredits === undefined) return [];
            return [{ specification, supplierCost, userCredits }];
        });
        if (values.priceStrategy === "image_resolution" && tierInputs.length !== imagePricingSpecifications.length) {
            message.error("图片模型必须完整配置 1K、2K、4K 定价");
            return;
        }
        if (values.priceStrategy === "video_resolution" && tierInputs.length === 0) {
            message.error("视频模型至少需要配置一个基础分辨率或超分规格");
            return;
        }
        const pricingInput: ModelPricingInput = {
            channelId: editing.channelId,
            model: editing.modelKey,
            capability: editing.capability,
            currency,
            inputPerMillionMicros: toMicro(values.inputPerMillion),
            outputPerMillionMicros: toMicro(values.outputPerMillion),
            cachedPerMillionMicros: toMicro(values.cachedPerMillion),
            expectedInputTokens: values.expectedInputTokens || 0,
            expectedOutputTokens: values.expectedOutputTokens || 0,
            expectedCachedTokens: values.expectedCachedTokens || 0,
            perRequestMicros: toMicro(values.perRequest),
            perMediaMicros: toMicro(values.perMedia),
            perVideoSecondMicros: toMicro(values.perVideoSecond),
            tiers: tierInputs.map(({ specification, supplierCost }) => ({
                specification: specification.key,
                supplierCostMicros: toMicro(supplierCost),
            })),
        };
        const modelInput = {
            modelKey: editing.modelKey,
            displayName: editing.displayName,
            accessPolicy: editing.accessPolicy,
            capability: editing.capability,
            billingMode: values.billingMode,
            priceStrategy: values.priceStrategy,
            unitPriceMicrocredits: values.priceStrategy === "flat" ? toMicro(values.unitCredits) : 0,
            priceTiers: tierInputs.map(({ specification, userCredits }) => ({
                resolution: specification.key,
                unitPriceMicrocredits: toMicro(userCredits),
            })),
            priceConfigured: true,
            enabled: editing.enabled,
        };
        setSaving(true);
        try {
            const currentPricing = editing.pricing || (await listAdminModelPricings()).pricings.find((pricing) =>
                pricingScopeKey(pricing.channelId, pricing.model, pricing.capability)
                === pricingScopeKey(pricingInput.channelId, pricingInput.model, pricingInput.capability));
            if (currentPricing) await updateAdminModelPricing(currentPricing.id, pricingInput);
            else await createAdminModelPricing(pricingInput);
            try {
                await updateAdminChannelModel(editing.channelId, editing.id, modelInput);
            } catch (error) {
                message.error(error instanceof Error ? `供应商成本已保存，但积分售价保存失败：${error.message}` : "供应商成本已保存，但积分售价保存失败");
                await reload();
                return;
            }
            setEditing(null);
            await reload();
            message.success("模型成本与积分售价已同步更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存模型商业定价失败");
        } finally {
            setSaving(false);
        }
    };

    const rows = useMemo(() => models.filter((model) => {
        const query = keyword.trim().toLowerCase();
        if (query && !`${model.modelKey} ${model.displayName} ${model.channelName}`.toLowerCase().includes(query)) return false;
        if (capability !== "all" && model.capability !== capability) return false;
        const modelStatus = commercialStatus(model, setting);
        return status === "all" || status === modelStatus;
    }), [models, keyword, capability, status, setting]);

    const configuredCount = models.filter((model) => commercialStatus(model, setting) === "configured").length;
    const warningCount = models.filter((model) => commercialStatus(model, setting) === "warning").length;
    const incompleteCount = models.length - configuredCount - warningCount;
    const columns: ColumnsType<CommercialModel> = [
        { title: "模型", render: (_, model) => <div className="model-pricing-model"><div className="model-pricing-model-name font-medium">{model.displayName || model.modelKey}</div><div className="model-pricing-model-key text-xs text-foreground/45">{model.modelKey}</div></div> },
        { title: "渠道", dataIndex: "channelName", width: 150 },
        { title: "类型", dataIndex: "capability", width: 80, render: capabilityLabel },
        { title: "供应商成本", width: 180, render: (_, model) => formatCost(model) },
        { title: "用户售价", width: 180, render: (_, model) => formatCustomerPrice(model) },
        { title: "预估利润率", width: 120, render: (_, model) => formatMargin(model, setting) },
        { title: "状态", width: 110, render: (_, model) => <CommercialStatusTag status={commercialStatus(model, setting)} /> },
        { title: "操作", width: 70, align: "right", render: (_, model) => <Button className="model-pricing-edit-button" type="text" aria-label={`配置 ${model.displayName || model.modelKey}`} icon={<Pencil className="size-4" />} onClick={() => openPricing(model)} /> },
    ];

    return (
        <AdminPageFrame title="模型商业定价" description="集中维护文案、图片、视频与音频模型的供应商成本、积分售价和目标利润，所有用户调用均以这里的生效价格扣费。" actions={<Button className="model-pricing-settings-button" icon={<Settings2 className="size-4" />} onClick={openSettings}>商业参数</Button>}>
            <section className="model-pricing-metrics mb-7 grid grid-cols-2 gap-x-8 gap-y-5 border-b border-border/60 pb-7 lg:grid-cols-4">
                <Metric label="全部模型" value={models.length} detail="已接入系统目录" />
                <Metric label="定价完整" value={configuredCount} detail="成本、售价与利润可核算" />
                <Metric label="利润预警" value={warningCount} detail={setting.configured ? `低于 ${setting.targetMarginBasisPoints / 100}% 目标` : "尚未配置利润基准"} tone="warning" />
                <Metric label="待完善" value={incompleteCount} detail="缺少成本、售价或商业参数" tone="muted" />
            </section>
            {!setting.configured ? <div className="model-pricing-notice mb-5 flex items-center justify-between gap-4 bg-amber-500/8 px-4 py-3 text-sm text-amber-700 dark:text-amber-300"><span className="model-pricing-notice-copy flex items-center gap-2"><TriangleAlert className="size-4 shrink-0" />请先配置每积分收入价值和目标利润率，系统才能核算模型利润。</span><Button className="model-pricing-notice-action" size="small" onClick={openSettings}>立即配置</Button></div> : null}
            <ListToolbar active={Boolean(keyword || capability !== "all" || status !== "all")} onReset={() => { setKeyword(""); setCapability("all"); setStatus("all"); }}>
                <Input className="app-list-search" allowClear prefix={<Search className="size-4 text-foreground/40" />} placeholder="搜索模型或渠道" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
                <Select className="w-32" value={capability} onChange={setCapability} options={[{ label: "全部类型", value: "all" }, { label: "文案", value: "text" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }, { label: "音频", value: "audio" }]} />
                <Select className="w-36" value={status} onChange={setStatus} options={[{ label: "全部状态", value: "all" }, { label: "定价完整", value: "configured" }, { label: "利润预警", value: "warning" }, { label: "待完善", value: "incomplete" }]} />
            </ListToolbar>
            <TableSurface>
                <Table className="app-data-table" rowKey="id" loading={loading} columns={columns} dataSource={rows} pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (total) => `共 ${total} 个模型` }} scroll={{ x: 980 }} />
            </TableSurface>
            <PricingDrawer model={editing} form={pricingForm} strategy={priceStrategy} saving={saving} onClose={() => setEditing(null)} onSave={() => void savePricing()} />
            <Drawer className="model-pricing-settings-drawer" title="商业定价基准" open={settingsOpen} width={460} onClose={() => setSettingsOpen(false)} extra={<Button className="model-pricing-settings-save" type="primary" loading={saving} onClick={() => void saveSettings()}>保存</Button>}>
                <Form className="model-pricing-settings-form" form={settingsForm} layout="vertical" requiredMark={false}>
                    <Form.Item className="model-pricing-settings-field" name="currency" label="结算币种" rules={[{ required: true, message: "请输入币种" }, { pattern: /^[A-Za-z]{3}$/, message: "请输入三位币种代码" }]}><Input className="model-pricing-currency-input" placeholder="CNY" maxLength={3} /></Form.Item>
                    <Form.Item className="model-pricing-settings-field" name="creditRevenue" label="每 1 积分对应收入" extra="用于将积分售价换算为预计货币收入；应根据实际充值与会员套餐测算。" rules={[{ required: true, type: "number", min: 0.000001, message: "必须大于 0" }]}><InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} /></Form.Item>
                    <Form.Item className="model-pricing-settings-field" name="targetMarginPercent" label="目标毛利率（%）" rules={[{ required: true, type: "number", min: 0, max: 100, message: "请输入 0-100" }]}><InputNumber className="model-pricing-number-input w-full" min={0} max={100} precision={2} /></Form.Item>
                </Form>
            </Drawer>
        </AdminPageFrame>
    );
}

function PricingDrawer({ model, form, strategy, saving, onClose, onSave }: { model: CommercialModel | null; form: FormInstance<PricingFormValues>; strategy: ChannelModel["priceStrategy"]; saving: boolean; onClose: () => void; onSave: () => void }) {
    const capability = model?.capability;
    const billingMode = Form.useWatch("billingMode", form) || "fixed_request";
    return (
        <Drawer className="model-pricing-drawer" title={model ? `配置 ${model.displayName || model.modelKey}` : "模型商业定价"} open={Boolean(model)} width={620} onClose={onClose} extra={<Button className="model-pricing-save-button" type="primary" loading={saving} onClick={onSave}>保存并生效</Button>}>
            <Form className="model-pricing-form" form={form} layout="vertical" requiredMark={false}>
                <div className="model-pricing-section mb-7">
                    <h3 className="model-pricing-section-title mb-1 text-sm font-semibold">计费方式</h3>
                    <p className="model-pricing-section-description mb-4 text-xs leading-5 text-foreground/48">用户调用成功后，按这里配置的积分价格扣费。</p>
                    <Form.Item className="model-pricing-field" name="currency" label="供应商结算币种" rules={[{ required: true, message: "请输入币种" }, { pattern: /^[A-Za-z]{3}$/, message: "请输入三位币种代码" }]}><Input className="model-pricing-input" maxLength={3} placeholder="CNY" /></Form.Item>
                    <Form.Item className="model-pricing-field" name="billingMode" label="用户计费单位"><Segmented className="model-pricing-segmented" block options={[{ label: "按次", value: "fixed_request" }, { label: "按秒", value: "per_second", disabled: capability !== "video" }]} /></Form.Item>
                    <Form.Item className="model-pricing-field" name="priceStrategy" label="价格策略"><Segmented className="model-pricing-segmented" block options={[
                        { label: "统一价格", value: "flat" },
                        { label: "按分辨率", value: capability === "video" ? "video_resolution" : "image_resolution", disabled: capability !== "image" && capability !== "video" },
                    ]} /></Form.Item>
                </div>
                {strategy === "image_resolution" || strategy === "video_resolution"
                    ? <ResolutionPricingFields strategy={strategy} billingMode={billingMode} />
                    : <FlatPricingFields capability={capability} />}
            </Form>
        </Drawer>
    );
}

function FlatPricingFields({ capability }: { capability?: ChannelModel["capability"] }) {
    return (
        <div className="model-pricing-flat-fields">
            <h3 className="model-pricing-section-title mb-4 text-sm font-semibold">成本与积分售价</h3>
            {capability === "text" ? <>
                <div className="model-pricing-token-grid grid grid-cols-1 gap-x-4 sm:grid-cols-2"><MoneyField name="inputPerMillion" label="输入成本 / 百万 Token" /><MoneyField name="outputPerMillion" label="输出成本 / 百万 Token" /><MoneyField name="cachedPerMillion" label="缓存输入成本 / 百万 Token" /><MoneyField name="perRequest" label="固定请求成本" /></div>
                <p className="model-pricing-section-description mb-3 text-xs leading-5 text-foreground/48">填写一次典型请求的平均 Token 用量，用于将供应商 Token 单价换算为可比较的单次成本和利润率。</p>
                <div className="model-pricing-token-assumption-grid grid grid-cols-1 gap-x-4 sm:grid-cols-3"><CountField name="expectedInputTokens" label="平均输入 Token" /><CountField name="expectedOutputTokens" label="平均输出 Token" /><CountField name="expectedCachedTokens" label="平均缓存 Token" /></div>
            </> : null}
            {capability === "image" || capability === "audio" ? <MoneyField name="perMedia" label={capability === "image" ? "供应商成本 / 张" : "供应商成本 / 个音频"} /> : null}
            {capability === "video" ? <div className="model-pricing-video-grid grid grid-cols-1 gap-x-4 sm:grid-cols-2"><MoneyField name="perMedia" label="供应商成本 / 个视频" /><MoneyField name="perVideoSecond" label="供应商成本 / 秒" /></div> : null}
            <Form.Item className="model-pricing-field" name="unitCredits" label="用户消耗积分" rules={[{ required: true, type: "number", min: 0.000001, message: "积分售价必须大于 0" }]}><InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} /></Form.Item>
        </div>
    );
}

function ResolutionPricingFields({ strategy, billingMode }: { strategy: ChannelModel["priceStrategy"]; billingMode: ChannelModel["billingMode"] }) {
    const specifications = specificationsForStrategy(strategy);
    const baseSpecifications = specifications.filter((item) => item.group === "base");
    const superResolutionSpecifications = specifications.filter((item) => item.group === "super_resolution");
    const unit = strategy === "image_resolution" ? "张" : billingMode === "per_second" ? "秒" : "次";
    return (
        <div className="model-pricing-resolution-fields">
            <h3 className="model-pricing-section-title mb-1 text-sm font-semibold">分辨率定价</h3>
            <p className="model-pricing-section-description mb-4 text-xs leading-5 text-foreground/48">只配置渠道真实支持的规格；未配置的规格调用时会明确失败，避免错扣积分。</p>
            <PricingSpecificationGroup title="基础分辨率" specifications={baseSpecifications} unit={unit} required={strategy === "image_resolution"} />
            {superResolutionSpecifications.length > 0 ? <PricingSpecificationGroup title="专有超分（完整服务价）" specifications={superResolutionSpecifications} unit={unit} required={false} /> : null}
        </div>
    );
}

function PricingSpecificationGroup({ title, specifications, unit, required }: { title: string; specifications: PricingSpecification[]; unit: string; required: boolean }) {
    return <section className="model-pricing-specification-group mb-5">
        <h4 className="model-pricing-specification-title mb-1 text-xs font-medium text-foreground/55">{title}</h4>
        {specifications.map((specification) => <div key={specification.key} className="model-pricing-resolution-row grid grid-cols-[92px_1fr_1fr] items-start gap-3 border-b border-border/50 py-4">
            <span className="model-pricing-resolution-label pt-8 text-sm font-semibold">{specification.label}</span>
            <MoneyField name={["tierCosts", specification.key]} label={`供应商成本 / ${unit}`} required={required} />
            <Form.Item className="model-pricing-field" name={["tierCredits", specification.key]} label={`用户积分 / ${unit}`} rules={required ? [{ required: true, type: "number", min: 0.000001, message: "必须大于 0" }] : [{ type: "number", min: 0.000001, message: "必须大于 0" }]}>
                <InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} placeholder="未配置" />
            </Form.Item>
        </div>)}
    </section>;
}

function MoneyField({ name, label, required = false }: { name: keyof PricingFormValues | Array<string>; label: string; required?: boolean }) {
    return <Form.Item className="model-pricing-field" name={name} label={label} rules={required ? [{ required: true, type: "number", min: 0.000001, message: "成本必须大于 0" }] : [{ type: "number", min: 0, message: "成本不能小于 0" }]}><InputNumber className="model-pricing-number-input w-full" min={required ? 0.000001 : 0} precision={6} placeholder="未配置" /></Form.Item>;
}

function CountField({ name, label }: { name: keyof PricingFormValues; label: string }) {
    return <Form.Item className="model-pricing-field" name={name} label={label} rules={[{ type: "integer", min: 0, message: "请输入不小于 0 的整数" }]}><InputNumber className="model-pricing-number-input w-full" min={0} precision={0} placeholder="未配置" /></Form.Item>;
}

function Metric({ label, value, detail, tone = "default" }: { label: string; value: number; detail: string; tone?: "default" | "warning" | "muted" }) {
    return <div className="model-pricing-metric"><div className="model-pricing-metric-label flex items-center gap-1.5 text-xs text-foreground/48">{tone === "warning" ? <TriangleAlert className="size-3.5 text-amber-500" /> : <CircleDollarSign className="size-3.5" />}{label}</div><div className="model-pricing-metric-value mt-2 text-2xl font-semibold tabular-nums">{value}</div><div className="model-pricing-metric-detail mt-1 text-[11px] text-foreground/38">{detail}</div></div>;
}

function CommercialStatusTag({ status }: { status: "configured" | "warning" | "incomplete" }) {
    if (status === "configured") return <Tag className="model-pricing-status" color="success">定价完整</Tag>;
    if (status === "warning") return <Tag className="model-pricing-status" color="warning">利润预警</Tag>;
    return <Tag className="model-pricing-status">待完善</Tag>;
}

function toMicro(value?: number) { return Math.round((value || 0) * 1_000_000); }
function fromMicro(value: number) { return value / 1_000_000; }
function optionalMoney(value?: number) { return value && value > 0 ? fromMicro(value) : undefined; }
function optionalCount(value?: number) { return value && value > 0 ? value : undefined; }
function tierCost(pricing: ModelPricing | undefined, specification: string) { const value = pricing?.tiers.find((tier) => tier.specification === specification)?.supplierCostMicros; return optionalMoney(value); }
function capabilityLabel(value: ChannelModel["capability"]) { return { text: "文案", image: "图片", video: "视频", audio: "音频" }[value]; }

function commercialStatus(model: CommercialModel, setting: ModelPricingOperationsSetting): "configured" | "warning" | "incomplete" {
    const margins = model.priceStrategy === "flat"
        ? [marginPercent(model, setting)]
        : model.priceTiers.map((tier) => marginPercent(model, setting, tier.resolution));
    if (margins.length === 0 || margins.some((margin) => margin === null)) return "incomplete";
    return margins.some((margin) => Number(margin) * 10_000 < setting.targetMarginBasisPoints)
        ? "warning"
        : "configured";
}

function marginPercent(model: CommercialModel, setting: ModelPricingOperationsSetting, resolution?: string) {
    if (!setting.configured || !model.priceConfigured || !model.pricing) return null;
    const cost = comparableCost(model, resolution);
    const credits = model.priceStrategy !== "flat"
        ? model.priceTiers.find((tier) => tier.resolution === resolution)?.unitPriceMicrocredits
        : model.unitPriceMicrocredits;
    if (cost === null || !credits || credits <= 0) return null;
    const revenue = credits * setting.creditRevenueMicros / 1_000_000;
    return revenue > 0 ? (revenue - cost) / revenue : null;
}

function comparableCost(model: CommercialModel, resolution?: string) {
    const pricing = model.pricing;
    if (!pricing) return null;
    if (model.priceStrategy !== "flat") return pricing.tiers.find((tier) => tier.specification === resolution)?.supplierCostMicros ?? null;
    if (model.capability === "text") {
        const tokenCost =
            pricing.inputPerMillionMicros * pricing.expectedInputTokens / 1_000_000 +
            pricing.outputPerMillionMicros * pricing.expectedOutputTokens / 1_000_000 +
            pricing.cachedPerMillionMicros * pricing.expectedCachedTokens / 1_000_000;
        const totalCost = pricing.perRequestMicros + tokenCost;
        return totalCost > 0 ? totalCost : null;
    }
    if (model.capability === "video" && model.billingMode === "per_second") return pricing.perVideoSecondMicros > 0 ? pricing.perVideoSecondMicros : null;
    return pricing.perMediaMicros > 0 ? pricing.perMediaMicros : null;
}

function formatMargin(model: CommercialModel, setting: ModelPricingOperationsSetting) {
    if (model.priceStrategy !== "flat") {
        const specifications = specificationsForStrategy(model.priceStrategy).filter((specification) =>
            model.priceTiers.some((tier) => tier.resolution === specification.key),
        );
        if (specifications.length === 0) return <span className="model-pricing-unavailable text-xs text-foreground/40">无法核算</span>;
        const values = specifications.map((specification) => marginPercent(model, setting, specification.key));
        if (values.some((value) => value === null)) return <span className="model-pricing-unavailable text-xs text-foreground/40">无法核算</span>;
        return <span className="model-pricing-margin text-xs tabular-nums">{values.map((value, index) => `${specifications[index].label} ${(Number(value) * 100).toFixed(1)}%`).join(" · ")}</span>;
    }
    const value = marginPercent(model, setting);
    return value === null ? <span className="model-pricing-unavailable text-xs text-foreground/40">无法核算</span> : <span className="model-pricing-margin tabular-nums">{(value * 100).toFixed(1)}%</span>;
}

function formatCost(model: CommercialModel) {
    const pricing = model.pricing;
    if (!pricing) return <span className="model-pricing-unavailable text-xs text-foreground/40">未配置</span>;
    if (model.priceStrategy !== "flat") return <span className="model-pricing-cost text-xs">{pricing.tiers.map((tier) => `${tierLabel(tier.specification)} ${money(tierCost(pricing, tier.specification), pricing.currency)}`).join(" · ")}</span>;
    const value = comparableCost(model);
    return value === null ? <span className="model-pricing-unavailable text-xs text-foreground/40">缺少可比成本</span> : <span className="model-pricing-cost text-xs">{money(fromMicro(value), pricing.currency)} / {model.billingMode === "per_second" ? "秒" : "次"}</span>;
}

function formatCustomerPrice(model: CommercialModel) {
    if (!model.priceConfigured) return <span className="model-pricing-unavailable text-xs text-foreground/40">未配置</span>;
    if (model.priceStrategy !== "flat") return <span className="model-pricing-price text-xs">{model.priceTiers.map((tier) => `${tierLabel(tier.resolution)} ${fromMicro(tier.unitPriceMicrocredits)} 积分`).join(" · ")}</span>;
    return <span className="model-pricing-price text-xs">{fromMicro(model.unitPriceMicrocredits)} 积分 / {model.billingMode === "per_second" ? "秒" : "次"}</span>;
}

function money(value: number | undefined, currency: string) { return value === undefined ? "未配置" : `${currency} ${value.toLocaleString("zh-CN", { maximumFractionDigits: 6 })}`; }
function tierLabel(value: string) { return value.startsWith("SR_") ? `超分 ${value.slice(3)}` : value; }
