import { Button, Input, InputNumber, Modal, Select } from "antd";
import { ArrowRight, Clock3, ShieldCheck } from "lucide-react";
import { useEffect, useRef } from "react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { billingCycleLabel, formatCredits, planTotalCredits, planTotalPriceCents, publicPlanName } from "./membership-formatters";

type MembershipPurchaseModalProps = {
    className?: string;
    onCancel: () => void;
    onSeatsChange: (seats: number) => void;
    onSubmit: () => void;
    onTeamIdChange: (teamId: string | undefined) => void;
    onTeamNameChange: (teamName: string) => void;
    open: boolean;
    plan: MembershipPlan | null;
    seats: number;
    submitting: boolean;
    teamId?: string;
    teamName: string;
    teams: Team[];
};

export function MembershipPurchaseModal({ className = "", onCancel, onSeatsChange, onSubmit, onTeamIdChange, onTeamNameChange, open, plan, seats, submitting, teamId, teamName, teams }: MembershipPurchaseModalProps) {
    const teamPlan = plan?.audience === "team";
    const appliedSeats = teamPlan ? seats : 1;
    const submitGuardRef = useRef(false);
    const observedSubmittingRef = useRef(false);

    useEffect(() => {
        if (!open) {
            submitGuardRef.current = false;
            observedSubmittingRef.current = false;
            return;
        }
        if (submitting) {
            observedSubmittingRef.current = true;
            return;
        }
        if (observedSubmittingRef.current) {
            submitGuardRef.current = false;
            observedSubmittingRef.current = false;
        }
    }, [open, submitting]);

    const requestCancel = () => {
        if (submitting) return;
        onCancel();
    };

    const requestSubmit = () => {
        if (submitting || submitGuardRef.current) return;
        submitGuardRef.current = true;
        onSubmit();
    };

    const formatPlanMoney = (valueCents: number): string => {
        if (!plan) return "";
        return new Intl.NumberFormat("zh-CN", {
            currency: plan.currency.trim().toUpperCase(),
            currencyDisplay: "narrowSymbol",
            maximumFractionDigits: 2,
            minimumFractionDigits: 0,
            style: "currency",
        }).format(valueCents / 100);
    };

    return (
        <Modal
            className={`membership-order-modal ${className}`}
            closable={!submitting}
            confirmLoading={submitting}
            footer={[
                <Button className="membership-order-modal-cancel" disabled={submitting} key="cancel" onClick={requestCancel}>
                    取消
                </Button>,
                <Button className="membership-order-modal-confirm" icon={<ArrowRight className="membership-order-modal-confirm-icon" />} key="confirm" loading={submitting} onClick={requestSubmit} type="primary">
                    创建订单并支付
                </Button>,
            ]}
            keyboard={!submitting}
            maskClosable={!submitting}
            onCancel={requestCancel}
            open={open}
            title={
                <div className="membership-order-modal-title">
                    <span>确认购买</span>
                    {plan ? <small>{publicPlanName(plan)}</small> : null}
                </div>
            }
        >
            {plan ? (
                <div className="membership-order-modal-content">
                    <section aria-label="购买商品" className="membership-order-product">
                        <div className="membership-order-product-copy">
                            <strong className="membership-order-product-name">{publicPlanName(plan)}</strong>
                            <span className="membership-order-product-cycle">{billingCycleLabel[plan.billingCycle]}</span>
                        </div>
                        <div className="membership-order-unit-price">
                            <strong className="membership-order-unit-price-value">{formatPlanMoney(plan.priceCents)}</strong>
                            <span className="membership-order-unit-price-suffix">
                                /{plan.billingCycle === "year" ? "年" : "月"}
                                {teamPlan ? "/席位" : ""}
                            </span>
                        </div>
                    </section>
                    <div className="membership-order-summary">
                        <span className="membership-order-summary-item">
                            <small className="membership-order-summary-label">{teamPlan ? "席位数量" : "购买周期"}</small>
                            <strong className="membership-order-summary-value">{teamPlan ? `${appliedSeats} 席位` : billingCycleLabel[plan.billingCycle]}</strong>
                        </span>
                        <span className="membership-order-summary-item">
                            <small className="membership-order-summary-label">{teamPlan ? "团队积分合计" : "到账积分"}</small>
                            <strong className="membership-order-summary-value">{formatCredits(planTotalCredits(plan, appliedSeats))} 积分</strong>
                        </span>
                    </div>
                    {teamPlan ? (
                        <div className="membership-team-fields">
                            <label className="membership-team-field">
                                <span className="membership-team-field-label">开通团队</span>
                                {teams.length ? (
                                    <Select className="membership-team-select" disabled={submitting} onChange={onTeamIdChange} options={teams.map((team) => ({ label: team.name, value: team.id }))} placeholder="选择团队" value={teamId} />
                                ) : (
                                    <Input className="membership-team-name-input" disabled={submitting} onChange={(event) => onTeamNameChange(event.target.value)} placeholder="输入新团队名称" value={teamName} />
                                )}
                            </label>
                            <label className="membership-team-field">
                                <span className="membership-team-field-label">席位数量</span>
                                <InputNumber className="membership-team-seat-input" disabled={submitting} max={plan.maxSeats} min={plan.minSeats} onChange={(value) => onSeatsChange(value ?? plan.minSeats)} value={seats} />
                            </label>
                        </div>
                    ) : null}
                    <div className="membership-order-total-price">
                        <span className="membership-order-total-price-label">应付金额</span>
                        <strong className="membership-order-total-price-value">{formatPlanMoney(planTotalPriceCents(plan, appliedSeats))}</strong>
                    </div>
                    <div className="membership-order-notices">
                        <p className="membership-order-note">
                            <Clock3 aria-hidden="true" className="membership-order-note-icon" />
                            订单创建后进入待支付状态，支付成功前不会提前发放积分或提升并发。
                        </p>
                        <p className="membership-order-note">
                            <ShieldCheck aria-hidden="true" className="membership-order-note-icon" />
                            订单按当前套餐生成权益快照，后台后续改价不会改变本次订单。
                        </p>
                    </div>
                </div>
            ) : null}
        </Modal>
    );
}
