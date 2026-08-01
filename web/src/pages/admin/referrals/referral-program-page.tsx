import { App, Button, Input, InputNumber, Modal, Pagination, Skeleton, Switch, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Ban, Coins, Gift, Save, ShoppingBag, UserPlus, UsersRound } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import {
    disqualifyAdminReferral,
    getAdminReferralProgram,
    updateAdminReferralProgram,
    updateAdminReferralRule,
    type AdminReferralProgramData,
    type ReferralInvitation,
    type ReferralRule,
} from "@/services/api/referral";
import { AdminPageFrame } from "../components/admin-shell";
import { SettingsSectionCard } from "../components/admin-ui";

type RuleDraft = {
    inviterCredits: number;
    inviteeCredits: number;
    enabled: boolean;
};

const pageSize = 20;

export default function ReferralProgramPage() {
    const { message } = App.useApp();
    const [data, setData] = useState<AdminReferralProgramData | null>(null);
    const [loading, setLoading] = useState(true);
    const [page, setPage] = useState(1);
    const [savingProgram, setSavingProgram] = useState(false);
    const [savingPlanId, setSavingPlanId] = useState("");
    const [drafts, setDrafts] = useState<Record<string, RuleDraft>>({});
    const [disqualifyTarget, setDisqualifyTarget] = useState<ReferralInvitation | null>(null);
    const [disqualifyReason, setDisqualifyReason] = useState("");
    const [disqualifying, setDisqualifying] = useState(false);

    const load = useCallback(async (targetPage: number) => {
        setLoading(true);
        try {
            const next = await getAdminReferralProgram(targetPage, pageSize);
            setData(next);
            setDrafts(Object.fromEntries(next.rules.map((rule) => [rule.membershipPlanId, {
                inviterCredits: fromMicrocredits(rule.inviterRewardMicrocredits),
                inviteeCredits: fromMicrocredits(rule.inviteeRewardMicrocredits),
                enabled: rule.enabled,
            }])));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取邀请奖励配置失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => {
        void load(page);
    }, [load, page]);

    const toggleProgram = async (enabled: boolean) => {
        setSavingProgram(true);
        try {
            const result = await updateAdminReferralProgram(enabled);
            setData((current) => current ? { ...current, program: result.program } : current);
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
            setData((current) => current ? {
                ...current,
                rules: current.rules.map((item) => item.membershipPlanId === rule.membershipPlanId ? result.rule : item),
            } : current);
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

    const columns = useMemo<ColumnsType<ReferralInvitation>>(() => [
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
        { title: "邀请码", dataIndex: "referralCode", render: (value: string) => <span className="referral-admin-code font-mono text-xs tracking-[0.08em]">{value}</span> },
        { title: "绑定 IP", dataIndex: "bindingIp", render: (value?: string) => <span className="referral-admin-ip text-xs text-foreground/55">{value || "--"}</span> },
        { title: "首购套餐", dataIndex: "planName", render: (value?: string) => <span className="referral-admin-plan text-xs">{value || "--"}</span> },
        { title: "邀请人奖励", dataIndex: "rewardedMicrocredits", align: "right", render: (value: number) => <span className="referral-admin-reward text-xs tabular-nums">{value > 0 ? formatCredits(value) : "--"}</span> },
        {
            title: "状态",
            dataIndex: "status",
            render: (status: ReferralInvitation["status"]) => {
                const meta = status === "rewarded" ? { label: "已发奖", color: "success" } : status === "disqualified" ? { label: "已取消资格", color: "error" } : { label: "待首购", color: "processing" };
                return <Tag className="referral-admin-status m-0" color={meta.color}>{meta.label}</Tag>;
            },
        },
        { title: "绑定时间", dataIndex: "boundAt", render: formatTime },
        {
            title: "操作",
            key: "actions",
            align: "right",
            render: (_, item) => item.status === "eligible" ? (
                <Button className="referral-admin-disqualify" size="small" type="text" danger icon={<Ban className="size-3.5" />} onClick={() => setDisqualifyTarget(item)}>取消资格</Button>
            ) : <span className="referral-admin-no-action text-xs text-foreground/28">--</span>,
        },
    ], []);

    return (
        <AdminPageFrame title="邀请有礼" description="管理邀请码、首购双边积分与异常邀请资格；不包含推广返佣。">
            {loading && !data ? <Skeleton className="referral-admin-loading py-12" active paragraph={{ rows: 10 }} /> : null}
            {data ? (
                <div className="referral-admin-content space-y-5">
                    <section className="referral-admin-overview grid gap-px overflow-hidden sm:grid-cols-3" aria-label="邀请活动概览">
                        <AdminMetric icon={<UsersRound className="size-4" />} label="邀请注册关系" value={data.summary.registeredCount.toLocaleString("zh-CN")} />
                        <AdminMetric icon={<ShoppingBag className="size-4" />} label="已触发首购" value={data.summary.purchasedCount.toLocaleString("zh-CN")} />
                        <AdminMetric icon={<Coins className="size-4" />} label="累计发放积分" value={formatCredits(data.summary.grantedTotalMicrocredits)} />
                    </section>

                    <SettingsSectionCard
                        icon={<Gift className="size-4" />}
                        title="活动状态"
                        description="关闭后不再接受新邀请码绑定；已绑定关系与历史奖励保留。重新开放前必须完成全部个人付费套餐规则。"
                        status={{ label: data.program.enabled ? "开放中" : "未开放", color: data.program.enabled ? "success" : "default" }}
                    >
                        <div className="referral-admin-program flex items-center justify-between px-6 py-5">
                            <div className="referral-admin-program-copy">
                                <div className="referral-admin-program-title text-sm font-medium">允许新用户通过邀请码建立关系</div>
                                <div className="referral-admin-program-note mt-1 text-xs text-foreground/48">邀请码只在首次注册时生效，绑定后不可更换。</div>
                            </div>
                            <Switch className="referral-admin-program-switch" checked={data.program.enabled} loading={savingProgram} onChange={(checked) => void toggleProgram(checked)} aria-label="开放邀请活动" />
                        </div>
                    </SettingsSectionCard>

                    <SettingsSectionCard
                        icon={<Coins className="size-4" />}
                        title="套餐奖励规则"
                        description="分别配置好友与邀请人的长期积分。规则按实际购买套餐结算，团队会员不参与。"
                        status={`${data.rules.filter((rule) => rule.enabled).length}/${data.rules.length} 已启用`}
                    >
                        <div className="referral-admin-rules divide-y divide-border/55">
                            {data.rules.map((rule) => {
                                const draft = drafts[rule.membershipPlanId] || { inviterCredits: 0, inviteeCredits: 0, enabled: false };
                                return (
                                    <div key={rule.membershipPlanId} className="referral-admin-rule grid items-center gap-4 px-6 py-4 lg:grid-cols-[minmax(150px,1fr)_160px_160px_86px_92px]">
                                        <div className="referral-admin-rule-plan min-w-0">
                                            <div className="referral-admin-rule-name truncate text-sm font-medium">{rule.planName} · {cycleLabel(rule.billingCycle)}</div>
                                            <div className="referral-admin-rule-code mt-1 truncate text-xs text-foreground/45">{rule.planCode}</div>
                                        </div>
                                        <label className="referral-admin-rule-field">
                                            <span className="referral-admin-rule-label mb-1 block text-xs text-foreground/55">好友奖励积分</span>
                                            <InputNumber className="referral-admin-rule-input w-full" min={0} max={10_000_000} precision={0} value={draft.inviteeCredits} onChange={(value) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, inviteeCredits: value || 0 } }))} />
                                        </label>
                                        <label className="referral-admin-rule-field">
                                            <span className="referral-admin-rule-label mb-1 block text-xs text-foreground/55">邀请人奖励积分</span>
                                            <InputNumber className="referral-admin-rule-input w-full" min={0} max={10_000_000} precision={0} value={draft.inviterCredits} onChange={(value) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, inviterCredits: value || 0 } }))} />
                                        </label>
                                        <div className="referral-admin-rule-enabled flex items-center gap-2">
                                            <Switch className="referral-admin-rule-switch" size="small" checked={draft.enabled} onChange={(checked) => setDrafts((current) => ({ ...current, [rule.membershipPlanId]: { ...draft, enabled: checked } }))} />
                                            <span className="referral-admin-rule-enabled-label text-xs text-foreground/55">{draft.enabled ? "启用" : "停用"}</span>
                                        </div>
                                        <Button className="referral-admin-rule-save" icon={<Save className="size-3.5" />} loading={savingPlanId === rule.membershipPlanId} onClick={() => void saveRule(rule)}>保存</Button>
                                    </div>
                                );
                            })}
                        </div>
                    </SettingsSectionCard>

                    <SettingsSectionCard
                        icon={<UserPlus className="size-4" />}
                        title="邀请关系"
                        description="查看绑定、首购和异常资格。绑定关系本身不可删除或改绑。"
                        status={`${data.total.toLocaleString("zh-CN")} 条`}
                    >
                        <Table<ReferralInvitation>
                            className="referral-admin-table"
                            rowKey="id"
                            size="middle"
                            columns={columns}
                            dataSource={data.invites}
                            pagination={false}
                            scroll={{ x: 1040 }}
                            locale={{ emptyText: "暂无邀请关系" }}
                        />
                        {data.total > pageSize ? (
                            <div className="referral-admin-pagination flex justify-end px-6 py-4">
                                <Pagination className="referral-admin-pagination-control" current={page} pageSize={pageSize} total={data.total} showSizeChanger={false} onChange={setPage} />
                            </div>
                        ) : null}
                    </SettingsSectionCard>
                </div>
            ) : null}

            <Modal className="referral-admin-disqualify-modal" title="取消邀请奖励资格" open={Boolean(disqualifyTarget)} confirmLoading={disqualifying} okText="确认取消资格" cancelText="返回" okButtonProps={{ danger: true }} onOk={() => void confirmDisqualify()} onCancel={() => { setDisqualifyTarget(null); setDisqualifyReason(""); }}>
                <div className="referral-admin-disqualify-content pt-2">
                    <p className="referral-admin-disqualify-description text-xs leading-5 text-foreground/55">该操作只允许在首购发奖前执行，不删除绑定事实，也不能对已发放奖励直接冲销。</p>
                    <Input.TextArea className="referral-admin-disqualify-reason mt-4" value={disqualifyReason} maxLength={500} showCount autoSize={{ minRows: 3, maxRows: 6 }} placeholder="填写可审计的取消原因" onChange={(event) => setDisqualifyReason(event.target.value)} />
                </div>
            </Modal>
        </AdminPageFrame>
    );
}

function AdminMetric({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
    return (
        <div className="referral-admin-metric px-5 py-4">
            <div className="referral-admin-metric-label flex items-center gap-1.5">
                <span className="referral-admin-metric-icon">{icon}</span>
                <span className="referral-admin-metric-label-text">{label}</span>
            </div>
            <div className="referral-admin-metric-value mt-2 tabular-nums">{value}</div>
        </div>
    );
}

function fromMicrocredits(value: number) {
    return value / 1_000_000;
}

function toMicrocredits(value: number) {
    return Math.round(value * 1_000_000);
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
