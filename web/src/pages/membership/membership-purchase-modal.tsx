import { Input, InputNumber, Modal, Select } from "antd";
import { Clock3, ShieldCheck } from "lucide-react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { formatCredits, formatMoney, planTotalCredits, planTotalPriceCents } from "./membership-formatters";

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

export function MembershipPurchaseModal({
    className = "",
    onCancel,
    onSeatsChange,
    onSubmit,
    onTeamIdChange,
    onTeamNameChange,
    open,
    plan,
    seats,
    submitting,
    teamId,
    teamName,
    teams,
}: MembershipPurchaseModalProps) {
    const teamPlan = plan?.audience === "team";
    const appliedSeats = teamPlan ? seats : 1;

    return (
        <Modal
            className={`membership-order-modal ${className}`}
            confirmLoading={submitting}
            okText="创建待付款订单"
            onCancel={onCancel}
            onOk={onSubmit}
            open={open}
            title={`确认购买 ${plan?.name ?? ""}`}
        >
            {plan ? (
                <div className="membership-order-modal-content">
                    <div className="membership-order-summary">
                        <span className="membership-order-summary-item">
                            <small className="membership-order-summary-label">应付金额</small>
                            <strong className="membership-order-summary-value">¥{formatMoney(planTotalPriceCents(plan, appliedSeats))}</strong>
                        </span>
                        <span className="membership-order-summary-item">
                            <small className="membership-order-summary-label">到账积分</small>
                            <strong className="membership-order-summary-value">{formatCredits(planTotalCredits(plan, appliedSeats))}</strong>
                        </span>
                    </div>
                    {teamPlan ? (
                        <div className="membership-team-fields">
                            <label className="membership-team-field">
                                <span className="membership-team-field-label">开通团队</span>
                                {teams.length ? (
                                    <Select
                                        className="membership-team-select"
                                        onChange={onTeamIdChange}
                                        options={teams.map((team) => ({ label: team.name, value: team.id }))}
                                        placeholder="选择团队"
                                        value={teamId}
                                    />
                                ) : (
                                    <Input
                                        className="membership-team-name-input"
                                        onChange={(event) => onTeamNameChange(event.target.value)}
                                        placeholder="输入新团队名称"
                                        value={teamName}
                                    />
                                )}
                            </label>
                            <label className="membership-team-field">
                                <span className="membership-team-field-label">席位数量</span>
                                <InputNumber
                                    className="membership-team-seat-input"
                                    max={plan.maxSeats}
                                    min={plan.minSeats}
                                    onChange={(value) => onSeatsChange(value ?? plan.minSeats)}
                                    value={seats}
                                />
                            </label>
                        </div>
                    ) : null}
                    <div className="membership-order-notices">
                        <p className="membership-order-note">
                            <Clock3 className="membership-order-note-icon" />
                            订单创建后进入待支付状态，支付成功前不会提前发放积分或提升并发。
                        </p>
                        <p className="membership-order-note">
                            <ShieldCheck className="membership-order-note-icon" />
                            订单按当前套餐生成权益快照，后台后续改价不会改变本次订单。
                        </p>
                    </div>
                </div>
            ) : null}
        </Modal>
    );
}
