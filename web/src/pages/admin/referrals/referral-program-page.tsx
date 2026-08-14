import { App, Button, Input, InputNumber, Modal, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Ban, RefreshCw, Save } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useBlocker } from "react-router";

import { disqualifyAdminReferral, getAdminReferralProgram, updateAdminReferralProgram, updateAdminReferralRule, type AdminReferralProgramData, type ReferralInvitation, type ReferralRule } from "@/services/api/referral";
import { AdminContentSection, AdminDataLayout, AdminMetric, AdminMetricBand } from "../components/admin-data-layout";
import { AdminPageFrame } from "../components/admin-shell";
import { AdminContentError, AdminContentSkeleton, AdminTableEmpty } from "../components/admin-ui";
import { PaginationBar } from "@/components/layout/workspace-page";

type RuleDraft = {
    inviterCredits: number;
    inviteeCredits: number;
    enabled: boolean;
};

const pageSize = 20;

export default function ReferralProgramPage() {
    const { message, modal } = App.useApp();
    const [data, setData] = useState<AdminReferralProgramData | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    const [page, setPage] = useState(1);
    const [savingProgram, setSavingProgram] = useState(false);
    const [savingPlanId, setSavingPlanId] = useState("");
    const [drafts, setDrafts] = useState<Record<string, RuleDraft>>({});
    const [disqualifyTarget, setDisqualifyTarget] = useState<ReferralInvitation | null>(null);
    const [disqualifyReason, setDisqualifyReason] = useState("");
    const [disqualifying, setDisqualifying] = useState(false);
    const loadSequence = useRef(0);
    const dirtyRulesRef = useRef(false);

    const hasDirtyRules = useMemo(() => {
        if (!data) return false;
        return data.rules.some((rule) => {
            const draft = drafts[rule.membershipPlanId];
            return draft ? ruleDraftChanged(rule, draft) : false;
        });
    }, [data, drafts]);

    useEffect(() => {
        dirtyRulesRef.current = hasDirtyRules;
    }, [hasDirtyRules]);

    const blocker = useBlocker(hasDirtyRules && !savingPlanId);

    useEffect(() => {
        if (blocker.state !== "blocked") return;
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "离开并放弃未保存的奖励规则？",
            content: "当前套餐奖励积分或启用状态尚未保存，离开后这些修改将丢失。",
            okText: "放弃修改",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => blocker.proceed(),
            onCancel: () => blocker.reset(),
        });
    }, [blocker, modal]);

    useEffect(() => {
        const beforeUnload = (event: BeforeUnloadEvent) => {
            if (!hasDirtyRules || savingPlanId) return;
            event.preventDefault();
        };
        window.addEventListener("beforeunload", beforeUnload);
        return () => window.removeEventListener("beforeunload", beforeUnload);
    }, [hasDirtyRules, savingPlanId]);

    const load = useCallback(
        async (targetPage: number) => {
            const sequence = loadSequence.current + 1;
            loadSequence.current = sequence;
            setLoading(true);
            setLoadError("");
            try {
                const next = await getAdminReferralProgram(targetPage, pageSize);
                if (sequence !== loadSequence.current) return;
                setData((current) => (dirtyRulesRef.current && current ? { ...next, rules: current.rules } : next));
                if (!dirtyRulesRef.current) {
                    setDrafts(Object.fromEntries(next.rules.map((rule) => [rule.membershipPlanId, draftFromRule(rule)])));
                }
            } catch (error) {
                if (sequence !== loadSequence.current) return;
                const description = error instanceof Error ? error.message : "读取邀请奖励配置失败";
                setLoadError(description);
                message.error(description);
            } finally {
                if (sequence === loadSequence.current) setLoading(false);
            }
        },
        [message],
    );

    useEffect(() => {
        void load(page);
    }, [load, page]);

    const confirmDiscardRules = (action: () => void) => {
        if (!hasDirtyRules) {
            action();
            return;
        }
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: "放弃未保存的奖励规则？",
            content: "刷新或切换邀请关系列表页将重新读取服务端数据，当前修改会丢失。",
            okText: "放弃并继续",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: () => {
                dirtyRulesRef.current = false;
                action();
            },
        });
    };

    const toggleProgram = async (enabled: boolean) => {
        setSavingProgram(true);
        try {
            const result = await updateAdminReferralProgram(enabled);
            setData((current) => (current ? { ...current, program: result.program } : current));
            message.success(enabled ? "邀请活动已开放" : "邀请活动已停止接收新关系");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新邀请活动状态失败");
        } finally {
            setSavingProgram(false);
        }
    };

    const saveRule = async (rule: ReferralRule) => {
        const draft = drafts[rule.membershipPlanId];
        if (!draft) return;
        setSavingPlanId(rule.membershipPlanId);
        try {
            const result = await updateAdminReferralRule(rule.membershipPlanId, {
                inviterRewardMicrocredits: toMicrocredits(draft.inviterCredits),
                inviteeRewardMicrocredits: toMicrocredits(draft.inviteeCredits),
                enabled: draft.enabled,
            });
            setData((current) =>
                current
                    ? {
                          ...current,
                          rules: current.rules.map((item) => (item.membershipPlanId === rule.membershipPlanId ? result.rule : item)),
                      }
                    : current,
            );
            setDrafts((current) => ({
                ...current,
                [rule.membershipPlanId]: draftFromRule(result.rule),
            }));
            message.success(`${rule.planName}${cycleLabel(rule.billingCycle)}奖励规则已生效`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存套餐邀请奖励失败");
        } finally {
            setSavingPlanId("");
        }
    };

    const confirmDisqualify = async () => {
        if (!disqualifyTarget) return;
        const reason = disqualifyReason.trim();
        if (!reason) {
            message.warning("请填写取消资格原因");
            return;
        }
        setDisqualifying(true);
        try {
            await disqualifyAdminReferral(disqualifyTarget.id, reason);
            setDisqualifyTarget(null);
            setDisqualifyReason("");
            message.success("已取消该邀请关系的奖励资格");
            await load(page);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "取消奖励资格失败");
        } finally {
            setDisqualifying(false);
        }
    };

    const columns = useMemo<ColumnsType<ReferralInvitation>>(
        () => [
            {
                title: "受邀用户",
                key: "invitee",
                render: (_, item) => (
                    <div className="referral-admin-user">
                        <div className="referral-admin-user-name text-xs font-medium">{item.inviteeDisplayName || item.inviteeUsername}</div>
                        <div className="referral-admin-user-id mt-0.5 text-xs text-foreground/45">@{item.inviteeUsername}</div>
                    </div>
                ),
            },
            { title: "邀请码", dataIndex: "referralCode", width: 130, render: (value: string) => <span className="referral-admin-code font-mono text-xs tracking-[0.08em]">{value}</span> },
            { title: "绑定 IP", dataIndex: "bindingIp", width: 130, render: (value?: string) => <span className="referral-admin-ip text-xs text-foreground/55">{value || "--"}</span> },
            { title: "首购套餐", dataIndex: "planName", width: 120, render: (value?: string) => <span className="referral-admin-plan text-xs">{value || "--"}</span> },
            { title: "邀请人奖励", dataIndex: "rewardedMicrocredits", width: 110, align: "right", render: (value: number) => <span className="referral-admin-reward text-xs tabular-nums">{value > 0 ? formatCredits(value) : "--"}</span> },
            {
                title: "状态",
                dataIndex: "status",
                width: 110,
                render: (status: ReferralInvitation["status"]) => {
                    const meta = status === "rewarded" ? { label: "已发奖", color: "success" } : status === "disqualified" ? { label: "已取消资格", color: "error" } : { label: "待首购", color: "processing" };
                    return (
                        <Tag className="referral-admin-status m-0" color={meta.color}>
                            {meta.label}
                        </Tag>
                    );
                },
            },
            { title: "绑定时间", dataIndex: "boundAt", width: 160, render: formatTime },
            {
                title: "操作",
                key: "actions",
                width: 110,
                align: "right",
                render: (_, item) =>
                    item.status === "eligible" ? (
                        <Button className="referral-admin-disqualify" size="small" type="text" danger icon={<Ban className="size-3.5" />} onClick={() => setDisqualifyTarget(item)}>
                            取消资格
                        </Button>
                    ) : (
                        <span className="referral-admin-no-action text-xs text-foreground/28">--</span>
                    ),
            },
        ],
        [],
    );

    return (
        <AdminPageFrame
            title="邀请奖励"
            description="管理邀请码、首购双边积分与异常邀请资格；不包含推广返佣。"
            actions={
                <Button className="referral-admin-refresh" icon={<RefreshCw className="size-4" />} loading={loading} onClick={() => confirmDiscardRules(() => void load(page))}>
                    刷新数据
                </Button>
            }
        >
            {loading && !data ? <AdminContentSkeleton rows={10} label="正在加载邀请活动" /> : null}
            {loadError ? <AdminContentError title={data ? "邀请运营数据刷新失败" : "邀请运营数据读取失败"} description={loadError} onRetry={() => void load(page)} /> : null}
            {data ? (
                <div className="referral-admin-content">
                    <AdminDataLayout>
                        <AdminMetricBand title="邀请活动概览" description="注册绑定、首购转化与已发放奖励的真实累计事实">
                            <AdminMetric label="邀请注册关系" value={data.summary.registeredCount.toLocaleString("zh-CN")} />
                            <AdminMetric label="已触发首购" value={data.summary.purchasedCount.toLocaleString("zh-CN")} />
                            <AdminMetric label="累计发放积分" value={formatCredits(data.summary.grantedTotalMicrocredits)} />
                        </AdminMetricBand>

                        <AdminContentSection
                            title="活动配置"
                            description="关闭后不再接受新邀请码绑定；已绑定关系与历史奖励保留。"
                            actions={<span className={`referral-admin-section-status ${data.program.enabled ? "is-active" : ""}`}>{data.program.enabled ? "开放中" : "未开放"}</span>}
                        >
                            <div className="referral-admin-program">
                                <div className="referral-admin-program-copy">
                                    <div className="referral-admin-program-title">允许新用户通过邀请码建立关系</div>
                                    <div className="referral-admin-program-note">邀请码只在首次注册时生效，绑定后不可更换；重新开放前必须完成全部个人付费套餐规则。</div>
                                </div>
                                <Switch className="referral-admin-program-switch" checked={data.program.enabled} loading={savingProgram} onChange={(checked) => void toggleProgram(checked)} aria-label="开放邀请活动" />
                            </div>
                        </AdminContentSection>

                        <AdminContentSection
                            title="套餐奖励"
                            description="分别配置好友与邀请人的长期积分。规则按实际购买套餐结算，团队会员不参与。"
                            actions={<span className={`referral-admin-section-status ${hasDirtyRules ? "is-dirty" : ""}`}>{hasDirtyRules ? "有未保存修改" : `${data.rules.filter((rule) => rule.enabled).length}/${data.rules.length} 已启用`}</span>}
                        >
                            <div className="referral-admin-rules divide-y divide-border/55">
                                {data.rules.map((rule) => {
                                    const draft = drafts[rule.membershipPlanId] || { inviterCredits: 0, inviteeCredits: 0, enabled: false };
                                    const dirty = ruleDraftChanged(rule, draft);
                                    return (
                                        <div key={rule.membershipPlanId} className="referral-admin-rule">
                                            <div className="referral-admin-rule-plan min-w-0">
                                                <div className="referral-admin-rule-name truncate text-sm font-medium">
                                                    {rule.planName} · {cycleLabel(rule.billingCycle)}
                                                </div>
                                                <div className="referral-admin-rule-code mt-1 truncate text-xs text-foreground/45">{rule.planCode}</div>
                                            </div>
                                            <label className="referral-admin-rule-field">
                                                <span className="referral-admin-rule-label mb-1 block text-xs text-foreground/55">好友奖励积分</span>
                                                <InputNumber
                                                    className="referral-admin-rule-input w-full"
                                                    min={0}
                                                    max={10_000_000}
                                                    precision={0}
                                                    value={draft.inviteeCredits}
                                                    onChange={(value) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, inviteeCredits: value || 0 } }))}
                                                />
                                            </label>
                                            <label className="referral-admin-rule-field">
                                                <span className="referral-admin-rule-label mb-1 block text-xs text-foreground/55">邀请人奖励积分</span>
                                                <InputNumber
                                                    className="referral-admin-rule-input w-full"
                                                    min={0}
                                                    max={10_000_000}
                                                    precision={0}
                                                    value={draft.inviterCredits}
                                                    onChange={(value) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, inviterCredits: value || 0 } }))}
                                                />
                                            </label>
                                            <div className="referral-admin-rule-enabled flex items-center gap-2">
                                                <Switch
                                                    className="referral-admin-rule-switch"
                                                    size="small"
                                                    checked={draft.enabled}
                                                    onChange={(checked) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, enabled: checked } }))}
                                                />
                                                <span className="referral-admin-rule-enabled-label text-xs text-foreground/55">{draft.enabled ? "启用" : "停用"}</span>
                                            </div>
                                            <Button
                                                className="referral-admin-rule-save"
                                                type={dirty ? "primary" : "default"}
                                                icon={<Save className="size-3.5" />}
                                                disabled={!dirty || Boolean(savingPlanId)}
                                                loading={savingPlanId === rule.membershipPlanId}
                                                onClick={() => void saveRule(rule)}
                                            >
                                                {dirty ? "保存" : "已保存"}
                                            </Button>
                                        </div>
                                    );
                                })}
                            </div>
                        </AdminContentSection>

                        <AdminContentSection title="邀请关系" description="查看绑定、首购和异常资格。绑定关系本身不可删除或改绑。" actions={<span className="referral-admin-section-status">共 {data.total.toLocaleString("zh-CN")} 条关系</span>}>
                            <Table<ReferralInvitation>
                                className="referral-admin-table app-data-table"
                                rowKey="id"
                                size="middle"
                                loading={loading}
                                columns={columns}
                                dataSource={data.invites}
                                pagination={false}
                                scroll={{ x: 980 }}
                                locale={{ emptyText: <AdminTableEmpty compact title="暂无邀请关系" description="用户绑定邀请关系后，记录会显示在这里。" /> }}
                            />
                            {data.total > pageSize ? <PaginationBar current={page} pageSize={pageSize} total={data.total} showSizeChanger={false} onChange={(nextPage) => confirmDiscardRules(() => setPage(nextPage))} /> : null}
                        </AdminContentSection>
                    </AdminDataLayout>
                </div>
            ) : null}

            <Modal
                className="admin-operation-modal referral-admin-disqualify-modal workspace-ui-scope"
                title="取消邀请奖励资格"
                open={Boolean(disqualifyTarget)}
                confirmLoading={disqualifying}
                closable={!disqualifying}
                mask={{ closable: !disqualifying }}
                keyboard={!disqualifying}
                okText="确认取消资格"
                cancelText="返回"
                okButtonProps={{ danger: true, disabled: !disqualifyReason.trim() }}
                cancelButtonProps={{ disabled: disqualifying }}
                onOk={() => void confirmDisqualify()}
                onCancel={() => {
                    if (disqualifying) return;
                    setDisqualifyTarget(null);
                    setDisqualifyReason("");
                }}
            >
                <div className="referral-admin-disqualify-content pt-2">
                    {disqualifyTarget ? (
                        <div className="referral-admin-disqualify-fact">
                            <span className="referral-admin-disqualify-fact-label">受邀用户</span>
                            <strong className="referral-admin-disqualify-fact-value">{disqualifyTarget.inviteeDisplayName || disqualifyTarget.inviteeUsername}</strong>
                            <span className="referral-admin-disqualify-fact-code">邀请码 {disqualifyTarget.referralCode}</span>
                        </div>
                    ) : null}
                    <p className="referral-admin-disqualify-description text-xs leading-5 text-foreground/55">该操作只允许在首购发奖前执行，不删除绑定事实，也不能对已发放奖励直接冲销。</p>
                    <Input.TextArea
                        className="referral-admin-disqualify-reason mt-4"
                        value={disqualifyReason}
                        maxLength={500}
                        showCount
                        autoSize={{ minRows: 3, maxRows: 6 }}
                        placeholder="填写可审计的取消原因"
                        onChange={(event) => setDisqualifyReason(event.target.value)}
                    />
                </div>
            </Modal>
        </AdminPageFrame>
    );
}

function fromMicrocredits(value: number) {
    return value / 1_000_000;
}

function toMicrocredits(value: number) {
    return Math.round(value * 1_000_000);
}

function draftFromRule(rule: ReferralRule): RuleDraft {
    return {
        inviterCredits: fromMicrocredits(rule.inviterRewardMicrocredits),
        inviteeCredits: fromMicrocredits(rule.inviteeRewardMicrocredits),
        enabled: rule.enabled,
    };
}

function ruleDraftChanged(rule: ReferralRule, draft: RuleDraft) {
    return toMicrocredits(draft.inviterCredits) !== rule.inviterRewardMicrocredits || toMicrocredits(draft.inviteeCredits) !== rule.inviteeRewardMicrocredits || draft.enabled !== rule.enabled;
}

function formatCredits(value: number) {
    return fromMicrocredits(value).toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}

function cycleLabel(cycle: ReferralRule["billingCycle"]) {
    return cycle === "year" ? "年付" : "月付";
}

function formatTime(value?: string) {
    return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "--";
}
