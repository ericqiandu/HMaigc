import { App, Modal, Skeleton, Table, Tag } from "antd";
import { Check, Coins, Copy, Gift, ShoppingBag, UserPlus, UsersRound } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { getReferralCenter, type ReferralCenterData, type ReferralRule } from "@/services/api/referral";

import "./referral-reward-center.css";

export const OPEN_REFERRAL_CENTER_EVENT = "open-ai-canvas:open-referral-center";

export function openReferralCenter() {
    window.dispatchEvent(new CustomEvent(OPEN_REFERRAL_CENTER_EVENT));
}

export function ReferralRewardCenter() {
    const { message } = App.useApp();
    const [open, setOpen] = useState(false);
    const [loading, setLoading] = useState(false);
    const [data, setData] = useState<ReferralCenterData | null>(null);
    const [copied, setCopied] = useState(false);

    const load = useCallback(async () => {
        setLoading(true);
        try {
            setData(await getReferralCenter());
        } catch (error) {
            message.error(error instanceof Error ? error.message : "读取邀请奖励失败");
        } finally {
            setLoading(false);
        }
    }, [message]);

    useEffect(() => {
        const handleOpen = () => setOpen(true);
        window.addEventListener(OPEN_REFERRAL_CENTER_EVENT, handleOpen);
        return () => window.removeEventListener(OPEN_REFERRAL_CENTER_EVENT, handleOpen);
    }, []);

    useEffect(() => {
        if (open && data === null) void load();
    }, [data, load, open]);

    const inviteURL = useMemo(() => {
        if (!data?.inviteCode) return "";
        const url = new URL("/register", window.location.origin);
        url.searchParams.set("invite", data.inviteCode);
        return url.toString();
    }, [data?.inviteCode]);

    const copyInvite = async () => {
        if (!inviteURL || !navigator.clipboard) {
            message.error("当前浏览器不支持复制邀请链接");
            return;
        }
        try {
            await navigator.clipboard.writeText(inviteURL);
            setCopied(true);
            message.success("邀请链接已复制");
            window.setTimeout(() => setCopied(false), 1600);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "复制邀请链接失败");
        }
    };

    return (
        <>
            <button
                type="button"
                className="referral-reward-trigger grid size-10 shrink-0 place-items-center rounded-full bg-foreground/[.07] text-foreground/65 transition-colors hover:bg-foreground/[.12] hover:text-foreground"
                aria-label="打开邀请有礼"
                onClick={() => setOpen(true)}
            >
                <Gift className="referral-reward-trigger-icon size-4" aria-hidden="true" />
            </button>
            <Modal
                className="referral-reward-modal"
                width={680}
                open={open}
                centered
                footer={null}
                onCancel={() => setOpen(false)}
                title={(
                    <div className="referral-reward-title flex items-center gap-2">
                        <span className="referral-reward-title-icon grid size-8 place-items-center rounded-md bg-amber-400/12 text-amber-500">
                            <Gift className="referral-reward-title-gift size-4" aria-hidden="true" />
                        </span>
                        <span className="referral-reward-title-copy">
                            <span className="referral-reward-title-text block text-base font-semibold">邀请有礼</span>
                            <span className="referral-reward-title-caption mt-0.5 block text-[11px] font-normal text-foreground/45">邀请好友首购会员，双方获得长期积分</span>
                        </span>
                    </div>
                )}
            >
                {loading && !data ? <Skeleton className="referral-reward-loading py-5" active paragraph={{ rows: 8 }} /> : null}
                {data ? (
                    <div className="referral-reward-content">
                        {!data.program.enabled ? (
                            <div className="referral-reward-closed mb-5 flex items-center justify-between bg-amber-500/[.07] px-4 py-3 text-xs text-amber-700 dark:text-amber-300">
                                <span className="referral-reward-closed-copy">邀请活动暂未开放，历史邀请与奖励记录仍可查看。</span>
                                <Tag className="referral-reward-closed-tag m-0" color="warning">未开放</Tag>
                            </div>
                        ) : null}

                        <section className="referral-reward-metrics grid grid-cols-3 bg-foreground/[.035]">
                            <ReferralMetric icon={<UsersRound className="size-4" />} label="邀请注册人数" value={data.summary.registeredCount.toLocaleString("zh-CN")} />
                            <ReferralMetric icon={<ShoppingBag className="size-4" />} label="邀请购买人数" value={data.summary.purchasedCount.toLocaleString("zh-CN")} />
                            <ReferralMetric icon={<Coins className="size-4" />} label="累计获得积分" value={formatCredits(data.summary.earnedInviterMicrocredits)} />
                        </section>

                        <section className="referral-reward-code-section mt-6">
                            <div className="referral-reward-section-heading mb-2 flex items-center justify-between">
                                <h3 className="referral-reward-section-title text-xs font-semibold">我的邀请码</h3>
                                <span className="referral-reward-code-tip text-[11px] text-foreground/42">注册时绑定后不可更换</span>
                            </div>
                            <div className="referral-reward-code-row flex min-h-12 items-center bg-foreground/[.04] px-4">
                                <span className="referral-reward-code flex-1 font-mono text-base font-semibold tracking-[0.16em]">{data.inviteCode}</span>
                                <button type="button" className="referral-reward-copy inline-flex h-8 items-center gap-1.5 bg-foreground px-3 text-xs font-medium text-background transition-opacity hover:opacity-82 disabled:opacity-45" disabled={!data.program.enabled} onClick={() => void copyInvite()}>
                                    {copied ? <Check className="referral-reward-copy-icon size-3.5" /> : <Copy className="referral-reward-copy-icon size-3.5" />}
                                    <span className="referral-reward-copy-label">{copied ? "已复制" : "复制邀请链接"}</span>
                                </button>
                            </div>
                        </section>

                        <section className="referral-reward-rules mt-6">
                            <div className="referral-reward-section-heading mb-2 flex items-center justify-between">
                                <h3 className="referral-reward-section-title text-xs font-semibold">首购奖励</h3>
                                <span className="referral-reward-rule-tip text-[11px] text-foreground/42">每位受邀用户仅触发一次</span>
                            </div>
                            <Table<ReferralRule>
                                className="referral-reward-table"
                                rowKey="membershipPlanId"
                                size="small"
                                pagination={false}
                                dataSource={data.rules}
                                locale={{ emptyText: "运营后台尚未配置生效套餐奖励" }}
                                columns={[
                                    { title: "购买周期", dataIndex: "billingCycle", render: (cycle: ReferralRule["billingCycle"]) => cycle === "year" ? "年付" : "月付" },
                                    { title: "会员等级", dataIndex: "planName", render: (name: string, rule) => <span className="referral-reward-plan font-medium">{name}<span className="referral-reward-plan-cycle ml-1 text-[10px] font-normal text-foreground/38">{rule.billingCycle === "year" ? "年付" : "月付"}</span></span> },
                                    { title: "好友奖励", dataIndex: "inviteeRewardMicrocredits", align: "right", render: formatCredits },
                                    { title: "我的奖励", dataIndex: "inviterRewardMicrocredits", align: "right", render: formatCredits },
                                ]}
                            />
                        </section>

                        <section className="referral-reward-rules-copy mt-5 text-[11px] leading-5 text-foreground/48">
                            <h3 className="referral-reward-rules-title mb-1 font-semibold text-foreground/65">活动规则</h3>
                            <ol className="referral-reward-rules-list list-decimal space-y-0.5 pl-4">
                                <li className="referral-reward-rule-item">好友通过专属链接完成注册，并首次购买个人会员后，双方积分在订单履约时同时到账。</li>
                                <li className="referral-reward-rule-item">邀请关系一经注册绑定不可更换；续费、重复购买与退款后重购不重复发放。</li>
                                <li className="referral-reward-rule-item">团队会员不参与邀请奖励；异常注册或虚假交易可由平台取消奖励资格。</li>
                                <li className="referral-reward-rule-item">当前奖励为长期积分，实际金额以购买时后台生效的套餐规则为准。</li>
                            </ol>
                        </section>
                    </div>
                ) : null}
            </Modal>
        </>
    );
}

function ReferralMetric({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
    return (
        <div className="referral-reward-metric px-4 py-4">
            <div className="referral-reward-metric-label flex items-center gap-1.5 text-[11px] text-foreground/45">
                <span className="referral-reward-metric-icon text-foreground/40">{icon}</span>
                <span className="referral-reward-metric-label-text">{label}</span>
            </div>
            <div className="referral-reward-metric-value mt-2 text-xl font-semibold tabular-nums">{value}</div>
        </div>
    );
}

function formatCredits(value: number) {
    return (value / 1_000_000).toLocaleString("zh-CN", { maximumFractionDigits: 2 });
}
