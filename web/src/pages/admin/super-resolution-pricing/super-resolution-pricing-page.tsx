import { Button, InputNumber, message, Segmented, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Save, Sparkles } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";

import { TableSurface } from "@/components/layout/workspace-page";
import { AdminContentSection, AdminDataLayout, AdminFilterSection, AdminMetric, AdminMetricBand } from "@/pages/admin/components/admin-data-layout";
import { AdminPageFrame } from "@/pages/admin/components/admin-shell";
import { AdminContentError } from "@/pages/admin/components/admin-ui";
import { listAdminSuperResolutionPricing, replaceAdminSuperResolutionPricing, type SuperResolutionPricingRule } from "@/services/api/auth";

const microScale = 1_000_000;

export default function SuperResolutionPricingPage() {
    const [rules, setRules] = useState<SuperResolutionPricingRule[]>([]);
    const [edition, setEdition] = useState<SuperResolutionPricingRule["edition"]>("standard");
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [dirty, setDirty] = useState(false);
    const [error, setError] = useState("");

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const result = await listAdminSuperResolutionPricing();
            setRules(result.rules);
            setDirty(false);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "读取超分定价失败");
        } finally {
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        void load();
    }, [load]);

    const updateRule = useCallback((id: string, patch: Partial<SuperResolutionPricingRule>) => {
        setRules((current) => current.map((rule) => (rule.id === id ? { ...rule, ...patch } : rule)));
        setDirty(true);
    }, []);

    const save = useCallback(async () => {
        setSaving(true);
        try {
            const result = await replaceAdminSuperResolutionPricing(rules);
            setRules(result.rules);
            setDirty(false);
            message.success("超分定价已保存并生效");
        } catch (reason) {
            message.error(reason instanceof Error ? reason.message : "保存超分定价失败");
        } finally {
            setSaving(false);
        }
    }, [rules]);

    const visibleRules = useMemo(() => rules.filter((rule) => rule.edition === edition), [edition, rules]);
    const factsAvailable = !loading || rules.length > 0;
    const configuredRuleCount = rules.filter((rule) => rule.priceConfigured).length;
    const enabledRuleCount = rules.filter((rule) => rule.enabled).length;
    const columns = useMemo<ColumnsType<SuperResolutionPricingRule>>(
        () => [
            { title: "输出分辨率", dataIndex: "resolution", width: 130, render: (value: string) => <strong className="super-resolution-pricing-resolution">{value}</strong> },
            {
                title: "输出帧率",
                width: 170,
                render: (_, rule) => (
                    <span className="super-resolution-pricing-fps tabular-nums">
                        {rule.fpsMinExclusive === 0 ? "≤" : `>${rule.fpsMinExclusive} 且 ≤`} {rule.fpsMaxInclusive} fps
                    </span>
                ),
            },
            { title: "供应商成本 / 秒", width: 190, render: (_, rule) => <span className="super-resolution-pricing-cost tabular-nums">{formatMoney(rule.supplierCostMinMicros, rule.supplierCostMaxMicros, rule.currency)}</span> },
            {
                title: "用户积分 / 秒",
                width: 220,
                render: (_, rule) => (
                    <InputNumber
                        className="super-resolution-pricing-price w-full"
                        min={0.000001}
                        precision={6}
                        placeholder="未配置"
                        value={rule.unitPriceMicrocredits > 0 ? rule.unitPriceMicrocredits / microScale : null}
                        onChange={(value) => updateRule(rule.id, { unitPriceMicrocredits: Math.round(Number(value || 0) * microScale), priceConfigured: Number(value || 0) > 0 })}
                    />
                ),
            },
            {
                title: "状态",
                width: 110,
                render: (_, rule) =>
                    rule.priceConfigured ? (
                        <Tag className="super-resolution-pricing-status" color="success">
                            可计费
                        </Tag>
                    ) : (
                        <Tag className="super-resolution-pricing-status" color="warning">
                            待定价
                        </Tag>
                    ),
            },
            { title: "启用", width: 80, align: "center", render: (_, rule) => <Switch className="super-resolution-pricing-enabled" checked={rule.enabled} onChange={(enabled) => updateRule(rule.id, { enabled })} /> },
        ],
        [updateRule],
    );

    if (error && rules.length === 0) {
        return (
            <AdminPageFrame title="超分定价" description="独立维护视频超分增强的供应商成本与用户积分售价。">
                <AdminContentError title="超分定价加载失败" description={error} onRetry={() => void load()} />
            </AdminPageFrame>
        );
    }

    return (
        <AdminPageFrame
            title="超分定价"
            description="超分是独立视频后处理服务。基础视频生成费与这里的超分附加费分别核算，并在同一账单中留痕。"
            actions={
                <Button className="super-resolution-pricing-save" type="primary" icon={<Save className="size-4" />} loading={saving} disabled={!dirty || rules.length !== 30} onClick={() => void save()}>
                    保存并生效
                </Button>
            }
        >
            <AdminDataLayout>
                <AdminMetricBand title="超分规格概览" description="集中查看规格完整性、启用状态和用户定价进度。">
                    <AdminMetric label="全部规格" value={factsAvailable ? `${rules.length} 条` : "—"} detail="标准版与专业版" />
                    <AdminMetric label="已定价" value={factsAvailable ? `${configuredRuleCount} 条` : "—"} detail="已填写用户积分" />
                    <AdminMetric label="已启用" value={factsAvailable ? `${enabledRuleCount} 条` : "—"} detail="允许进入执行流程" />
                    <AdminMetric label="当前版本" value={edition === "standard" ? "标准版" : "专业版"} detail={factsAvailable ? `${visibleRules.length} 条规格` : "正在读取规格"} />
                </AdminMetricBand>
                <AdminFilterSection label="超分版本筛选">
                    <div className="super-resolution-pricing-filter-content">
                        <div className="super-resolution-pricing-summary-copy">
                            <Sparkles className="super-resolution-pricing-summary-icon size-5" />
                            <div className="super-resolution-pricing-summary-text">
                                <strong className="super-resolution-pricing-summary-title">字节画质增强 · 按输出视频时长计费</strong>
                                <p className="super-resolution-pricing-summary-description">供应商成本已按参考表初始化；只有填写用户积分售价后，该规格才允许执行。</p>
                            </div>
                        </div>
                        <Segmented
                            className="super-resolution-pricing-edition"
                            value={edition}
                            onChange={(value) => setEdition(value as SuperResolutionPricingRule["edition"])}
                            options={[
                                { label: "标准版", value: "standard" },
                                { label: "专业版", value: "professional" },
                            ]}
                        />
                    </div>
                </AdminFilterSection>
                <AdminContentSection title="超分价格表" description="逐规格维护供应商参考成本、用户积分售价与启用状态。" actions={<span className="admin-model-billing-content-count">共 {visibleRules.length} 条规格</span>}>
                    {error ? (
                        <div className="super-resolution-pricing-refresh-error">
                            <AdminContentError title="刷新失败" description={error} onRetry={() => void load()} />
                        </div>
                    ) : null}
                    <TableSurface className="super-resolution-pricing-table-surface">
                        <Table className="app-data-table super-resolution-pricing-table" rowKey="id" columns={columns} dataSource={visibleRules} loading={loading} pagination={false} scroll={{ x: 900 }} />
                    </TableSurface>
                </AdminContentSection>
            </AdminDataLayout>
        </AdminPageFrame>
    );
}

function formatMoney(minMicros: number, maxMicros: number, currency: string) {
    const min = minMicros / microScale;
    const max = maxMicros / microScale;
    const value = min === max ? min.toString() : `${min}–${max}`;
    return `${currency} ${value}`;
}
