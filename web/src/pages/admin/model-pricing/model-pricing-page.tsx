import { Alert, App, Button, Drawer, Form, Input, InputNumber, Select, Segmented, Table, Tag } from "antd";
import type { FormInstance } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil, Search, Settings2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { ListToolbar, TableSurface } from "@/components/layout/workspace-page";
import { refreshSystemChannels } from "@/lib/user-session";
import {
    createAdminModelPricing,
    getAdminModelPricingOperationsSetting,
    getAdminAgentDefaultModelSetting,
    listAdminModelPricings,
    updateAdminModelPricing,
    updateAdminModelPricingOperationsSetting,
    updateAdminAgentDefaultModelSetting,
    type AgentDefaultModelSetting,
    type ModelPricing,
    type ModelPricingInput,
    type ModelPricingOperationsSetting,
} from "@/services/api/auth";
import { listAdminChannelModels, updateAdminChannelModel, type ChannelModel } from "@/services/api/wallet";
import { useAdminContext } from "../admin-context";
import { AdminContentSection, AdminDataLayout, AdminMetric, AdminMetricBand } from "../components/admin-data-layout";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminTableEmpty, AdminTableSkeleton } from "../components/admin-ui";
import { agentDefaultModelOptions, pricingContractForModel } from "./agent-model-options";
import { imagePricingSpecifications, specificationsForModel, type PricingSpecification } from "./pricing-specifications";

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
type PricingTierInput = { specification: PricingSpecification; supplierCost: number; userCredits?: number };

const emptySetting: ModelPricingOperationsSetting = { configured: false, currency: "", creditRevenueMicros: 0, targetMarginBasisPoints: 0 };
const pricingScopeKey = (channelId: string | undefined, model: string, capability: ModelPricing["capability"]) => `${(channelId || "").trim()}:${model.trim()}:${capability.trim().toLowerCase()}`;

export default function ModelPricingPage() {
    const { message } = App.useApp();
    const { references } = useAdminContext();
    const [models, setModels] = useState<CommercialModel[]>([]);
    const [setting, setSetting] = useState<ModelPricingOperationsSetting>(emptySetting);
    const [agentSetting, setAgentSetting] = useState<AgentDefaultModelSetting | null>(null);
    const [agentModelId, setAgentModelId] = useState("");
    const [savingAgentModel, setSavingAgentModel] = useState(false);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [saving, setSaving] = useState(false);
    const [pricingDirty, setPricingDirty] = useState(false);
    const [settingsDirty, setSettingsDirty] = useState(false);
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
            const [pricingResult, settingResult, agentSettingResult, ...channelResults] = await Promise.all([
                listAdminModelPricings(),
                getAdminModelPricingOperationsSetting(),
                getAdminAgentDefaultModelSetting(),
                ...references.channels.map((channel) => listAdminChannelModels(channel.id)),
            ]);
            const pricingByModel = new Map(pricingResult.pricings.map((item) => [pricingScopeKey(item.channelId, item.model, item.capability), item]));
            const nextModels = channelResults.flatMap((result, index) => {
                const channel = references.channels[index];
                return result.models.map((model) => ({
                    ...model,
                    channelName: channel.name,
                    pricing: pricingByModel.get(pricingScopeKey(channel.id, model.modelKey, model.capability)) || pricingByModel.get(pricingScopeKey("", model.modelKey, model.capability)),
                }));
            });
            setModels(nextModels);
            setSetting(settingResult.setting);
            setAgentSetting(agentSettingResult.setting);
            setAgentModelId(agentSettingResult.setting.configured ? agentSettingResult.setting.channelModelId : "");
            setLoadError("");
        } catch (error) {
            setLoadError(error instanceof Error ? error.message : "读取模型商业定价失败");
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void reload();
    }, [references.channels]);

    const openPricing = (model: CommercialModel) => {
        const pricing = model.pricing;
        const contract = pricingContractForModel(model, pricing);
        setEditing(model);
        pricingForm.setFieldsValue({
            currency: pricing?.currency || setting.currency,
            billingMode: contract.billingMode,
            priceStrategy: contract.priceStrategy,
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
            tierCredits: Object.fromEntries(model.priceTiers.map((tier) => [pricingTierKey(tier.resolution, tier.inputVariant), fromMicro(tier.unitPriceMicrocredits)])),
        });
        setPricingDirty(contract.billingMode !== model.billingMode || contract.priceStrategy !== model.priceStrategy);
    };

    const openSettings = () => {
        settingsForm.setFieldsValue({
            currency: setting.currency,
            creditRevenue: fromMicro(setting.creditRevenueMicros),
            targetMarginPercent: setting.targetMarginBasisPoints / 100,
        });
        setSettingsDirty(false);
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
            setSettingsDirty(false);
            setSettingsOpen(false);
            message.success("商业定价基准已更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存商业定价基准失败");
        } finally {
            setSaving(false);
        }
    };

    const agentModelOptions = useMemo(() => agentDefaultModelOptions(models), [models]);

    const saveAgentModel = async () => {
        if (!agentModelId) {
            message.error("请选择已启用、已完成定价的系统文本模型");
            return;
        }
        setSavingAgentModel(true);
        try {
            const result = await updateAdminAgentDefaultModelSetting(agentModelId);
            setAgentSetting(result.setting);
            setAgentModelId(result.setting.channelModelId);
            try {
                await refreshSystemChannels();
                message.success("全站 Agent 模型已更新");
            } catch {
                message.warning("全站 Agent 模型已保存，但当前页面同步失败，请刷新后继续");
            }
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存 Agent 模型失败");
        } finally {
            setSavingAgentModel(false);
        }
    };

    const savePricing = async () => {
        if (!editing) return;
        const values = await pricingForm.validateFields();
        const currency = values.currency.trim().toUpperCase();
        const specifications = specificationsForModel({ ...editing, priceStrategy: values.priceStrategy });
        const incompleteSpecification = specifications
            .filter((specification) => specification.group === "base")
            .find((specification) => {
                const supplierCost = values.tierCosts?.[specification.key];
                const userCredits = values.tierCredits?.[specification.key];
                return userCredits !== undefined && (supplierCost === undefined || supplierCost <= 0 || userCredits <= 0);
            });
        if (incompleteSpecification) {
            message.error(`${incompleteSpecification.label} 的供应商成本和用户积分必须同时配置且大于 0`);
            return;
        }
        const tierInputs = specifications.reduce<PricingTierInput[]>((result, specification) => {
            const supplierCost = values.tierCosts?.[specification.key];
            const userCredits = values.tierCredits?.[specification.key];
            if (supplierCost === undefined && userCredits === undefined) return result;
            if (supplierCost !== undefined) result.push({ specification, supplierCost, userCredits });
            return result;
        }, []);
        if (values.priceStrategy === "image_resolution" && tierInputs.length !== imagePricingSpecifications.length) {
            message.error("图片模型必须完整配置 1K、2K、4K 定价");
            return;
        }
        const baseTierInputs = tierInputs.filter(({ specification }) => specification.group === "base");
        const baseSaleTierInputs = baseTierInputs.filter(({ userCredits }) => userCredits !== undefined);
        if (values.priceStrategy === "video_resolution" && baseTierInputs.length === 0) {
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
        const tokenPricing = values.billingMode === "token_usage" && values.priceStrategy === "token";
        const modelInput = {
            modelKey: editing.modelKey,
            displayName: editing.displayName,
            marketingCopy: editing.marketingCopy,
            promotionBadge: editing.promotionBadge,
            estimatedDurationSeconds: editing.estimatedDurationSeconds,
            brandKey: editing.brandKey,
            accessPolicy: editing.accessPolicy,
            capability: editing.capability,
            billingMode: values.billingMode,
            priceStrategy: values.priceStrategy,
            unitPriceMicrocredits: values.priceStrategy === "flat" ? toMicro(values.unitCredits) : 0,
            priceTiers: baseSaleTierInputs.map(({ specification, userCredits }) => ({
                resolution: specification.resolution || specification.key,
                inputVariant: specification.inputVariant || "standard",
                unitPriceMicrocredits: toMicro(userCredits as number),
            })),
            priceConfigured: tokenPricing
                ? pricingInput.inputPerMillionMicros > 0 && pricingInput.outputPerMillionMicros > 0 && pricingInput.expectedOutputTokens > 0
                : values.priceStrategy === "flat"
                  ? Boolean(values.unitCredits && values.unitCredits > 0)
                  : baseSaleTierInputs.length > 0,
            enabled: editing.enabled,
        };
        setSaving(true);
        try {
            const currentPricing = (await listAdminModelPricings()).pricings.find((pricing) => pricingScopeKey(pricing.channelId, pricing.model, pricing.capability) === pricingScopeKey(pricingInput.channelId, pricingInput.model, pricingInput.capability));
            if (currentPricing) await updateAdminModelPricing(currentPricing.id, pricingInput);
            else await createAdminModelPricing(pricingInput);
            try {
                if (modelInput.priceConfigured || editing.priceConfigured) await updateAdminChannelModel(editing.channelId, editing.id, modelInput);
            } catch (error) {
                message.error(error instanceof Error ? `供应商成本已保存，但积分售价保存失败：${error.message}` : "供应商成本已保存，但积分售价保存失败");
                await reload();
                return;
            }
            setEditing(null);
            setPricingDirty(false);
            await reload();
            message.success("模型成本与积分售价已同步更新");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存模型商业定价失败");
        } finally {
            setSaving(false);
        }
    };

    const rows = useMemo(
        () =>
            models.filter((model) => {
                const query = keyword.trim().toLowerCase();
                if (query && !`${model.modelKey} ${model.displayName} ${model.channelName}`.toLowerCase().includes(query)) return false;
                if (capability !== "all" && model.capability !== capability) return false;
                const modelStatus = commercialStatus(model, setting);
                return status === "all" || status === modelStatus;
            }),
        [models, keyword, capability, status, setting],
    );

    const configuredCount = models.filter((model) => commercialStatus(model, setting) === "configured").length;
    const warningCount = models.filter((model) => commercialStatus(model, setting) === "warning").length;
    const incompleteCount = models.length - configuredCount - warningCount;
    const columns: ColumnsType<CommercialModel> = [
        {
            title: "模型",
            width: 240,
            render: (_, model) => (
                <div className="model-pricing-model min-w-0">
                    <div className="model-pricing-model-name truncate font-medium" title={model.displayName || model.modelKey}>
                        {model.displayName || model.modelKey}
                    </div>
                    <div className="model-pricing-model-key truncate text-xs text-foreground/45" title={model.modelKey}>
                        {model.modelKey}
                    </div>
                </div>
            ),
        },
        { title: "渠道", dataIndex: "channelName", width: 150 },
        { title: "类型", dataIndex: "capability", width: 80, render: capabilityLabel },
        { title: "供应商成本", width: 180, render: (_, model) => formatCost(model) },
        { title: "用户售价", width: 180, render: (_, model) => formatCustomerPrice(model) },
        { title: "预估利润率", width: 120, render: (_, model) => formatMargin(model, setting) },
        { title: "状态", width: 110, render: (_, model) => <CommercialStatusTag status={commercialStatus(model, setting)} /> },
        {
            title: "操作",
            width: 70,
            align: "right",
            render: (_, model) => <Button className="model-pricing-edit-button" type="text" aria-label={`配置 ${model.displayName || model.modelKey}`} icon={<Pencil className="size-4" />} onClick={() => openPricing(model)} />,
        },
    ];

    if (loadError && models.length === 0) {
        return (
            <AdminPageFrame title="模型中心" description="集中维护文案、图片、视频与音频模型的供应商成本、积分售价和目标利润。" modelCenter>
                <AdminContentError title="模型商业定价加载失败" description={loadError} onRetry={() => void reload()} />
            </AdminPageFrame>
        );
    }

    return (
        <AdminPageFrame
            title="模型中心"
            description="集中维护文案、图片、视频与音频模型的供应商成本、积分售价和目标利润，所有用户调用均以这里的生效价格扣费。"
            modelCenter
            actions={
                <Button className="model-pricing-settings-button" icon={<Settings2 className="size-4" />} onClick={openSettings}>
                    商业参数
                </Button>
            }
        >
            {loadError ? (
                <div className="model-pricing-refresh-error mb-5">
                    <AdminContentError title="模型商业定价刷新失败" description={loadError} onRetry={() => void reload()} />
                </div>
            ) : null}
            <AdminDataLayout>
                <AdminContentSection className="model-pricing-agent-section" title="Agent 模型配置" description="全站使用唯一已启用文本模型；模型失效时明确停用，不自动降级。">
                    <div className="model-pricing-agent-setting-controls flex flex-col gap-3 sm:flex-row sm:items-center">
                        <Select
                            aria-label="全站 Agent 默认模型"
                            className="model-pricing-agent-model-select min-w-0 flex-1"
                            value={agentModelId || undefined}
                            placeholder="选择已启用并完成定价的文本模型"
                            options={agentModelOptions}
                            onChange={setAgentModelId}
                        />
                        <Button className="model-pricing-agent-model-save" type="primary" loading={savingAgentModel} disabled={!agentModelId || agentModelId === agentSetting?.channelModelId} onClick={() => void saveAgentModel()}>
                            保存 Agent 模型
                        </Button>
                    </div>
                </AdminContentSection>
                <AdminMetricBand title="商业定价概览" description="集中查看模型定价完整性与利润风险。">
                    <AdminMetric label="全部模型" value={models.length} detail="已接入系统目录" />
                    <AdminMetric label="定价完整" value={configuredCount} detail="成本、售价与利润可核算" />
                    <AdminMetric label="利润预警" value={warningCount} detail={setting.configured ? `低于 ${setting.targetMarginBasisPoints / 100}% 目标` : "尚未配置利润基准"} />
                    <AdminMetric label="待完善" value={incompleteCount} detail="缺少成本、售价或商业参数" />
                </AdminMetricBand>
                {!setting.configured ? (
                    <Alert
                        className="model-pricing-notice"
                        type="warning"
                        showIcon
                        title="商业参数尚未配置"
                        description="请先配置每积分收入价值和目标利润率，系统才能核算模型利润。"
                        action={
                            <Button className="model-pricing-notice-action" size="small" onClick={openSettings}>
                                立即配置
                            </Button>
                        }
                    />
                ) : null}
                <AdminContentSection className="model-pricing-table-section" title="模型价格表" description="维护供应商成本、用户售价和利润状态。" actions={<span className="model-pricing-result-count">共 {rows.length} 个模型</span>}>
                    <ListToolbar
                        className="model-pricing-toolbar"
                        active={Boolean(keyword || capability !== "all" || status !== "all")}
                        onReset={() => {
                            setKeyword("");
                            setCapability("all");
                            setStatus("all");
                        }}
                    >
                        <Input className="app-list-search model-pricing-search" allowClear prefix={<Search className="size-4 text-foreground/40" />} placeholder="搜索模型或渠道" value={keyword} onChange={(event) => setKeyword(event.target.value)} />
                        <Select
                            aria-label="筛选模型类型"
                            className="model-pricing-filter"
                            value={capability}
                            onChange={setCapability}
                            options={[
                                { label: "全部类型", value: "all" },
                                { label: "文案", value: "text" },
                                { label: "图片", value: "image" },
                                { label: "视频", value: "video" },
                                { label: "音频", value: "audio" },
                            ]}
                        />
                        <Select
                            aria-label="筛选定价状态"
                            className="model-pricing-filter"
                            value={status}
                            onChange={setStatus}
                            options={[
                                { label: "全部状态", value: "all" },
                                { label: "定价完整", value: "configured" },
                                { label: "利润预警", value: "warning" },
                                { label: "待完善", value: "incomplete" },
                            ]}
                        />
                    </ListToolbar>
                    <TableSurface className="model-pricing-table-surface">
                        {loading && models.length === 0 ? (
                            <AdminTableSkeleton rows={8} columns={8} />
                        ) : (
                            <Table
                                className="app-data-table model-pricing-table"
                                rowKey="id"
                                loading={loading}
                                columns={columns}
                                dataSource={rows}
                                locale={{
                                    emptyText: (
                                        <AdminTableEmpty
                                            filtered={Boolean(keyword || capability !== "all" || status !== "all")}
                                            title={models.length === 0 ? "尚未接入可定价模型" : undefined}
                                            description={models.length === 0 ? "请先在 AI 模型配置中创建渠道并添加模型。" : undefined}
                                        />
                                    ),
                                }}
                                pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [20, 50, 100], showTotal: (total, range) => `${range[0]}-${range[1]} / 共 ${total} 个模型` }}
                                scroll={{ x: 1130 }}
                            />
                        )}
                    </TableSurface>
                </AdminContentSection>
            </AdminDataLayout>
            <PricingDrawer model={editing} form={pricingForm} strategy={priceStrategy} saving={saving} dirty={pricingDirty} onDirty={() => setPricingDirty(true)} onClose={() => setEditing(null)} onSave={() => void savePricing()} />
            <Drawer
                className="admin-object-drawer model-pricing-settings-drawer"
                title="商业定价基准"
                open={settingsOpen}
                size={460}
                onClose={() => {
                    if (saving) return;
                    setSettingsOpen(false);
                }}
                extra={
                    <Button className="model-pricing-settings-save" type="primary" loading={saving} disabled={!settingsDirty} onClick={() => void saveSettings()}>
                        保存
                    </Button>
                }
            >
                <div className="model-pricing-drawer-sync-state" role="status">
                    {settingsDirty ? "有未保存变更" : "当前配置已同步"}
                </div>
                <Form className="model-pricing-settings-form" form={settingsForm} layout="vertical" requiredMark={false} onValuesChange={() => setSettingsDirty(true)}>
                    <Form.Item
                        className="model-pricing-settings-field"
                        name="currency"
                        label="结算币种"
                        rules={[
                            { required: true, message: "请输入币种" },
                            { pattern: /^[A-Za-z]{3}$/, message: "请输入三位币种代码" },
                        ]}
                    >
                        <Input className="model-pricing-currency-input" placeholder="CNY" maxLength={3} />
                    </Form.Item>
                    <Form.Item
                        className="model-pricing-settings-field"
                        name="creditRevenue"
                        label="每 1 积分对应收入"
                        extra="用于将积分售价换算为预计货币收入；应根据实际充值与会员套餐测算。"
                        rules={[{ required: true, type: "number", min: 0.000001, message: "必须大于 0" }]}
                    >
                        <InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} />
                    </Form.Item>
                    <Form.Item className="model-pricing-settings-field" name="targetMarginPercent" label="目标毛利率（%）" rules={[{ required: true, type: "number", min: 0, max: 100, message: "请输入 0-100" }]}>
                        <InputNumber className="model-pricing-number-input w-full" min={0} max={100} precision={2} />
                    </Form.Item>
                </Form>
            </Drawer>
        </AdminPageFrame>
    );
}

function PricingDrawer({
    model,
    form,
    strategy,
    saving,
    dirty,
    onDirty,
    onClose,
    onSave,
}: {
    model: CommercialModel | null;
    form: FormInstance<PricingFormValues>;
    strategy: ChannelModel["priceStrategy"];
    saving: boolean;
    dirty: boolean;
    onDirty: () => void;
    onClose: () => void;
    onSave: () => void;
}) {
    const capability = model?.capability;
    const billingMode = Form.useWatch("billingMode", form) || "fixed_request";
    return (
        <Drawer
            className="admin-object-drawer model-pricing-drawer"
            title={model ? `配置 ${model.displayName || model.modelKey}` : "模型商业定价"}
            open={Boolean(model)}
            size={620}
            onClose={() => {
                if (saving) return;
                onClose();
            }}
            extra={
                <Button className="model-pricing-save-button" type="primary" loading={saving} disabled={!dirty} onClick={onSave}>
                    保存并生效
                </Button>
            }
        >
            <div className="model-pricing-drawer-sync-state" role="status">
                {dirty ? "有未保存变更" : "当前定价已同步"}
            </div>
            <Form className="model-pricing-form" form={form} layout="vertical" requiredMark={false} onValuesChange={onDirty}>
                <div className="model-pricing-section mb-7">
                    <h3 className="model-pricing-section-title mb-1 text-sm font-semibold">计费方式</h3>
                    <p className="model-pricing-section-description mb-4 text-xs leading-5 text-foreground/48">用户调用成功后，按这里配置的积分价格扣费。</p>
                    <Form.Item
                        className="model-pricing-field"
                        name="currency"
                        label="供应商结算币种"
                        rules={[
                            { required: true, message: "请输入币种" },
                            { pattern: /^[A-Za-z]{3}$/, message: "请输入三位币种代码" },
                        ]}
                    >
                        <Input className="model-pricing-input" maxLength={3} placeholder="CNY" />
                    </Form.Item>
                    <Form.Item className="model-pricing-field" name="billingMode" label="用户计费单位">
                        <Segmented
                            className="model-pricing-segmented"
                            block
                            options={[
                                { label: "按次", value: "fixed_request" },
                                { label: "按秒", value: "per_second", disabled: capability !== "video" },
                                { label: "按 Token", value: "token_usage", disabled: capability !== "text" },
                            ]}
                        />
                    </Form.Item>
                    <Form.Item className="model-pricing-field" name="priceStrategy" label="价格策略">
                        <Segmented
                            className="model-pricing-segmented"
                            block
                            options={[
                                { label: "统一价格", value: "flat" },
                                { label: "按分辨率", value: capability === "video" ? "video_resolution" : "image_resolution", disabled: capability !== "image" && capability !== "video" },
                                { label: "Token 用量", value: "token", disabled: capability !== "text" },
                            ]}
                        />
                    </Form.Item>
                </div>
                {strategy === "image_resolution" || strategy === "video_resolution" ? (
                    <ResolutionPricingFields strategy={strategy} billingMode={billingMode} model={model} />
                ) : (
                    <>
                        <FlatPricingFields capability={capability} strategy={strategy} />
                        <SupplierOnlyPricingFields modelKey={model?.modelKey || ""} strategy={strategy} />
                    </>
                )}
            </Form>
        </Drawer>
    );
}

function FlatPricingFields({ capability, strategy }: { capability?: ChannelModel["capability"]; strategy: ChannelModel["priceStrategy"] }) {
    return (
        <div className="model-pricing-flat-fields">
            <h3 className="model-pricing-section-title mb-4 text-sm font-semibold">成本与积分售价</h3>
            {capability === "text" ? (
                <>
                    <div className="model-pricing-token-grid grid grid-cols-1 gap-x-4 sm:grid-cols-2">
                        <MoneyField name="inputPerMillion" label="输入成本 / 百万 Token" />
                        <MoneyField name="outputPerMillion" label="输出成本 / 百万 Token" />
                        <MoneyField name="cachedPerMillion" label="缓存输入成本 / 百万 Token" />
                        <MoneyField name="perRequest" label="固定请求成本" />
                    </div>
                    <p className="model-pricing-section-description mb-3 text-xs leading-5 text-foreground/48">填写一次典型请求的平均 Token 用量，用于将供应商 Token 单价换算为可比较的单次成本和利润率。</p>
                    <div className="model-pricing-token-assumption-grid grid grid-cols-1 gap-x-4 sm:grid-cols-3">
                        <CountField name="expectedInputTokens" label="平均输入 Token" />
                        <CountField name="expectedOutputTokens" label="平均输出 Token" />
                        <CountField name="expectedCachedTokens" label="平均缓存 Token" />
                    </div>
                </>
            ) : null}
            {capability === "image" || capability === "audio" ? <MoneyField name="perMedia" label={capability === "image" ? "供应商成本 / 张" : "供应商成本 / 个音频"} /> : null}
            {capability === "video" ? (
                <div className="model-pricing-video-grid grid grid-cols-1 gap-x-4 sm:grid-cols-2">
                    <MoneyField name="perMedia" label="供应商成本 / 个视频" />
                    <MoneyField name="perVideoSecond" label="供应商成本 / 秒" />
                </div>
            ) : null}
            {strategy === "token" ? (
                <p className="model-pricing-section-description text-xs leading-5 text-foreground/48">用户积分按实际上游 Token 用量自动核算，1 元等于 100 积分。</p>
            ) : (
                <Form.Item className="model-pricing-field" name="unitCredits" label="用户消耗积分" rules={[{ required: true, type: "number", min: 0.000001, message: "积分售价必须大于 0" }]}>
                    <InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} />
                </Form.Item>
            )}
        </div>
    );
}

function ResolutionPricingFields({ strategy, billingMode, model }: { strategy: ChannelModel["priceStrategy"]; billingMode: ChannelModel["billingMode"]; model: CommercialModel | null }) {
    const specifications = model ? specificationsForModel({ ...model, priceStrategy: strategy }) : [];
    const baseSpecifications = specifications.filter((item) => item.group === "base");
    const supplierOnlySpecifications = specifications.filter((item) => item.group === "supplier-only");
    const unit = strategy === "image_resolution" ? "张" : billingMode === "per_second" ? "秒" : "次";
    return (
        <div className="model-pricing-resolution-fields">
            <h3 className="model-pricing-section-title mb-1 text-sm font-semibold">分辨率定价</h3>
            <p className="model-pricing-section-description mb-4 text-xs leading-5 text-foreground/48">只配置渠道真实支持的规格；未配置的规格调用时会明确失败，避免错扣积分。</p>
            <PricingSpecificationGroup title="基础生成分辨率" specifications={baseSpecifications} unit={unit} required={strategy === "image_resolution"} />
            {supplierOnlySpecifications.length ? <PricingSpecificationGroup title="MiniMax H3 输入素材与视频再生成本" specifications={supplierOnlySpecifications} unit="秒" required={false} supplierOnly /> : null}
        </div>
    );
}

function SupplierOnlyPricingFields({ modelKey, strategy }: { modelKey: string; strategy: ChannelModel["priceStrategy"] }) {
    const specifications = specificationsForModel({ modelKey, priceStrategy: strategy }).filter((item) => item.group === "supplier-only");
    if (specifications.length === 0) return null;
    return (
        <div className="model-pricing-supplier-fields mt-5">
            <h3 className="model-pricing-section-title mb-1 text-sm font-semibold">MiniMax 官方供应商成本</h3>
            <p className="model-pricing-section-description mb-4 text-xs leading-5 text-foreground/48">供应商成本独立于用户积分售价，用于利润和调用成本核算。</p>
            <PricingSpecificationGroup title="语音模型成本" specifications={specifications} unit="万字符" required={false} supplierOnly />
        </div>
    );
}

function PricingSpecificationGroup({ title, specifications, unit, required, supplierOnly = false }: { title: string; specifications: PricingSpecification[]; unit: string; required: boolean; supplierOnly?: boolean }) {
    return (
        <section className="model-pricing-specification-group mb-5">
            <h4 className="model-pricing-specification-title mb-1 text-xs font-medium text-foreground/55">{title}</h4>
            {specifications.map((specification) => (
                <div key={specification.key} className="model-pricing-resolution-row grid grid-cols-[92px_1fr_1fr] items-start gap-3 border-b border-border/50 py-4">
                    <span className="model-pricing-resolution-label pt-8 text-sm font-semibold">{specification.label}</span>
                    <div className="model-pricing-specification-cost-field">
                        <MoneyField name={["tierCosts", specification.key]} label={`供应商成本 / ${specification.unit || unit}`} required={required} />
                        {specification.note ? <p className="model-pricing-specification-note -mt-4 text-[11px] leading-4 text-foreground/42">{specification.note}</p> : null}
                    </div>
                    {supplierOnly ? (
                        <div className="model-pricing-supplier-only-label pt-8 text-xs text-foreground/42">供应商成本项，不直接设置用户售价</div>
                    ) : (
                        <Form.Item
                            className="model-pricing-field"
                            name={["tierCredits", specification.key]}
                            label={`用户积分 / ${unit}`}
                            rules={required ? [{ required: true, type: "number", min: 0.000001, message: "必须大于 0" }] : [{ type: "number", min: 0.000001, message: "必须大于 0" }]}
                        >
                            <InputNumber className="model-pricing-number-input w-full" min={0.000001} precision={6} placeholder="未配置" />
                        </Form.Item>
                    )}
                </div>
            ))}
        </section>
    );
}

function MoneyField({ name, label, required = false }: { name: keyof PricingFormValues | Array<string>; label: string; required?: boolean }) {
    return (
        <Form.Item className="model-pricing-field" name={name} label={label} rules={required ? [{ required: true, type: "number", min: 0.000001, message: "成本必须大于 0" }] : [{ type: "number", min: 0, message: "成本不能小于 0" }]}>
            <InputNumber className="model-pricing-number-input w-full" min={required ? 0.000001 : 0} precision={6} placeholder="未配置" />
        </Form.Item>
    );
}

function CountField({ name, label }: { name: keyof PricingFormValues; label: string }) {
    return (
        <Form.Item className="model-pricing-field" name={name} label={label} rules={[{ type: "integer", min: 0, message: "请输入不小于 0 的整数" }]}>
            <InputNumber className="model-pricing-number-input w-full" min={0} precision={0} placeholder="未配置" />
        </Form.Item>
    );
}

function CommercialStatusTag({ status }: { status: "configured" | "warning" | "incomplete" }) {
    if (status === "configured")
        return (
            <Tag className="model-pricing-status" color="success">
                定价完整
            </Tag>
        );
    if (status === "warning")
        return (
            <Tag className="model-pricing-status" color="warning">
                利润预警
            </Tag>
        );
    return <Tag className="model-pricing-status">待完善</Tag>;
}

function toMicro(value?: number) {
    return Math.round((value || 0) * 1_000_000);
}
function fromMicro(value: number) {
    return value / 1_000_000;
}
function optionalMoney(value?: number) {
    return value && value > 0 ? fromMicro(value) : undefined;
}
function optionalCount(value?: number) {
    return value && value > 0 ? value : undefined;
}
function tierCost(pricing: ModelPricing | undefined, specification: string) {
    const value = pricing?.tiers.find((tier) => tier.specification === specification)?.supplierCostMicros;
    return optionalMoney(value);
}
function capabilityLabel(value: ChannelModel["capability"]) {
    return { text: "文案", image: "图片", video: "视频", audio: "音频" }[value];
}

function commercialStatus(model: CommercialModel, setting: ModelPricingOperationsSetting): "configured" | "warning" | "incomplete" {
    if (model.priceStrategy === "token") return model.priceConfigured && comparableCost(model) !== null ? (setting.targetMarginBasisPoints > 0 ? "warning" : "configured") : "incomplete";
    const margins = model.priceStrategy === "flat" ? [marginPercent(model, setting)] : model.priceTiers.map((tier) => marginPercent(model, setting, pricingTierKey(tier.resolution, tier.inputVariant)));
    if (margins.length === 0 || margins.some((margin) => margin === null)) return "incomplete";
    return margins.some((margin) => Number(margin) * 10_000 < setting.targetMarginBasisPoints) ? "warning" : "configured";
}

function marginPercent(model: CommercialModel, setting: ModelPricingOperationsSetting, resolution?: string) {
    if (!setting.configured || !model.priceConfigured || !model.pricing) return null;
    const cost = comparableCost(model, resolution);
    if (model.priceStrategy === "token") return cost === null ? null : 0;
    const credits = model.priceStrategy !== "flat" ? model.priceTiers.find((tier) => pricingTierKey(tier.resolution, tier.inputVariant) === resolution)?.unitPriceMicrocredits : model.unitPriceMicrocredits;
    if (cost === null || !credits || credits <= 0) return null;
    const revenue = (credits * setting.creditRevenueMicros) / 1_000_000;
    return revenue > 0 ? (revenue - cost) / revenue : null;
}

function comparableCost(model: CommercialModel, resolution?: string) {
    const pricing = model.pricing;
    if (!pricing) return null;
    if (model.priceStrategy !== "flat" && model.priceStrategy !== "token") return pricing.tiers.find((tier) => tier.specification === resolution)?.supplierCostMicros ?? null;
    if (model.capability === "text") {
        const tokenCost =
            (pricing.inputPerMillionMicros * pricing.expectedInputTokens) / 1_000_000 + (pricing.outputPerMillionMicros * pricing.expectedOutputTokens) / 1_000_000 + (pricing.cachedPerMillionMicros * pricing.expectedCachedTokens) / 1_000_000;
        const totalCost = pricing.perRequestMicros + tokenCost;
        return totalCost > 0 ? totalCost : null;
    }
    if (model.capability === "video" && model.billingMode === "per_second") return pricing.perVideoSecondMicros > 0 ? pricing.perVideoSecondMicros : null;
    return pricing.perMediaMicros > 0 ? pricing.perMediaMicros : null;
}

function formatMargin(model: CommercialModel, setting: ModelPricingOperationsSetting) {
    if (model.priceStrategy === "token") return model.priceConfigured ? <span className="model-pricing-margin tabular-nums">0.0%</span> : <span className="model-pricing-unavailable text-xs text-foreground/40">无法核算</span>;
    if (model.priceStrategy !== "flat") {
        const specifications = specificationsForModel(model).filter((specification) => model.priceTiers.some((tier) => pricingTierKey(tier.resolution, tier.inputVariant) === specification.key));
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
    if ((model.priceStrategy !== "flat" && model.priceStrategy !== "token") || pricing.tiers.length > 0)
        return <span className="model-pricing-cost text-xs">{pricing.tiers.map((tier) => `${tierLabel(tier.specification)} ${money(tierCost(pricing, tier.specification), pricing.currency)}`).join(" · ")}</span>;
    const value = comparableCost(model);
    return value === null ? (
        <span className="model-pricing-unavailable text-xs text-foreground/40">缺少可比成本</span>
    ) : (
        <span className="model-pricing-cost text-xs">
            {money(fromMicro(value), pricing.currency)} / {model.billingMode === "per_second" ? "秒" : "次"}
        </span>
    );
}

function formatCustomerPrice(model: CommercialModel) {
    if (!model.priceConfigured) return <span className="model-pricing-unavailable text-xs text-foreground/40">未配置</span>;
    if (model.priceStrategy === "token") return <span className="model-pricing-price text-xs">按实际上游 Token 用量</span>;
    if (model.priceStrategy !== "flat")
        return <span className="model-pricing-price text-xs">{model.priceTiers.map((tier) => `${tierLabel(pricingTierKey(tier.resolution, tier.inputVariant))} ${fromMicro(tier.unitPriceMicrocredits)} 积分`).join(" · ")}</span>;
    return (
        <span className="model-pricing-price text-xs">
            {fromMicro(model.unitPriceMicrocredits)} 积分 / {model.billingMode === "per_second" ? "秒" : "次"}
        </span>
    );
}

function money(value: number | undefined, currency: string) {
    return value === undefined ? "未配置" : `${currency} ${value.toLocaleString("zh-CN", { maximumFractionDigits: 6 })}`;
}
function tierLabel(value: string) {
    const labels: Record<string, string> = {
        INPUT_IMAGE_OVERAGE: "输入图片（超 5 张）",
        INPUT_VIDEO_768P: "输入视频·768P",
        INPUT_VIDEO_2K: "输入视频·2K",
        REGENERATE_768P_TO_2K: "再生 768P→2K",
        REGENERATE_INPUT_IMAGE_OVERAGE: "再生输入图片（超 5 张）",
        REGENERATE_INPUT_VIDEO_768P: "再生输入视频·768P",
        TEN_THOUSAND_CHARACTERS: "语音合成·万字符",
        VOICE_DESIGN_OR_CLONE: "音色设计/克隆·个",
    };
    if (value.includes("::")) {
        const [resolution, variant] = value.split("::");
        return `${resolution}·${variant === "reference_video" ? "参考视频" : "普通生成"}`;
    }
    return labels[value] || value;
}

function pricingTierKey(resolution: string, inputVariant?: string) {
    return `${resolution}::${inputVariant || "standard"}`;
}
