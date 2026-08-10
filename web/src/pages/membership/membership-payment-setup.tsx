import { Button, Input, InputNumber, Select } from "antd";
import { ArrowRight, LoaderCircle, ShieldCheck } from "lucide-react";
import type { ReactElement } from "react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { MembershipOrderFacts, MembershipOrderFactsSkeleton } from "../payment/membership-order-facts";
import { membershipOrderFactsFromPlan, type MembershipOrderFactsModel } from "../payment/membership-order-facts-domain";

export type MembershipPaymentSetupProps = {
    createdOrderNumber: string;
    creationError: string;
    frozenFacts: MembershipOrderFactsModel | null;
    frozenFactsError: string;
    onConfirm: () => void;
    onRetry: () => void;
    onSeatsChange: (seats: number) => void;
    onTeamIdChange: (teamId: string | undefined) => void;
    onTeamNameChange: (teamName: string) => void;
    openingCheckout: boolean;
    plan: MembershipPlan | null;
    seats: number;
    submitting: boolean;
    teamId?: string;
    teamName: string;
    teams: Team[];
};

export function MembershipPaymentSetup({
    createdOrderNumber,
    creationError,
    frozenFacts,
    frozenFactsError,
    onConfirm,
    onRetry,
    onSeatsChange,
    onTeamIdChange,
    onTeamNameChange,
    openingCheckout,
    plan,
    seats,
    submitting,
    teamId,
    teamName,
    teams,
}: MembershipPaymentSetupProps): ReactElement {
    const teamPlan = plan?.audience === "team";
    const appliedSeats = plan && teamPlan ? Math.min(plan.maxSeats, Math.max(plan.minSeats, seats)) : 1;
    const hasCreatedOrder = createdOrderNumber.trim().length > 0;
    const facts = hasCreatedOrder ? frozenFacts : plan ? membershipOrderFactsFromPlan(plan, appliedSeats) : null;
    const writeInFlight = submitting || openingCheckout;
    const failureMessage = frozenFactsError || creationError;

    return (
        <div className="payment-checkout-shell is-dialog membership-payment-setup">
            <div className="payment-checkout-order-surface">{facts ? <MembershipOrderFacts facts={facts} /> : <MembershipOrderFactsSkeleton />}</div>
            <aside aria-label="创建付款码" className="payment-checkout-payment-surface membership-payment-setup-action">
                {failureMessage ? (
                    <div className="membership-payment-setup-error" role="alert">
                        <strong className="membership-payment-setup-error-title">{hasCreatedOrder ? `订单 ${createdOrderNumber} 已创建` : "付款订单创建失败"}</strong>
                        <p className="membership-payment-setup-error-copy">{failureMessage}</p>
                        {createdOrderNumber ? <p className="membership-payment-setup-error-note">重新打开付款码不会重复创建订单。</p> : null}
                        <Button className="membership-payment-setup-primary" disabled={writeInFlight} onClick={onRetry} type="primary">
                            {createdOrderNumber ? "重新打开付款码" : "重试创建付款订单"}
                        </Button>
                    </div>
                ) : !plan || !teamPlan ? (
                    <div aria-live="polite" className="membership-payment-setup-progress" role="status">
                        <LoaderCircle aria-hidden="true" className="membership-payment-setup-progress-icon" />
                        <strong className="membership-payment-setup-progress-title">{openingCheckout ? "正在打开安全收银台" : "正在创建付款订单"}</strong>
                        <p className="membership-payment-setup-progress-copy">订单与付款码均以支付服务返回的真实状态为准。</p>
                    </div>
                ) : (
                    <div className="membership-payment-setup-confirmation">
                        <div aria-label="团队购买配置" className="membership-payment-team-fields">
                            <label className="membership-payment-team-field">
                                <span className="membership-payment-team-field-label">开通团队</span>
                                {teams.length > 0 ? (
                                    <Select className="membership-payment-team-select" disabled={writeInFlight} onChange={onTeamIdChange} options={teams.map((team) => ({ label: team.name, value: team.id }))} placeholder="选择团队" value={teamId} />
                                ) : (
                                    <Input className="membership-payment-team-name-input" disabled={writeInFlight} onChange={(event) => onTeamNameChange(event.target.value)} placeholder="输入新团队名称" value={teamName} />
                                )}
                            </label>
                            <label className="membership-payment-team-field">
                                <span className="membership-payment-team-field-label">席位数量</span>
                                <InputNumber className="membership-payment-team-seat-input" disabled={writeInFlight} max={plan.maxSeats} min={plan.minSeats} onChange={(value) => onSeatsChange(value ?? plan.minSeats)} value={appliedSeats} />
                            </label>
                        </div>
                        <ShieldCheck aria-hidden="true" className="membership-payment-setup-confirmation-icon" />
                        <strong className="membership-payment-setup-confirmation-title">确认团队与席位配置</strong>
                        <p className="membership-payment-setup-confirmation-copy">确认后创建冻结订单，付款码生成后不能修改团队、席位或金额。</p>
                        <Button className="membership-payment-setup-primary" disabled={writeInFlight} icon={<ArrowRight aria-hidden="true" className="membership-payment-setup-primary-icon" />} onClick={onConfirm} type="primary">
                            确认配置并生成付款码
                        </Button>
                        {writeInFlight ? (
                            <div aria-live="polite" className="membership-payment-setup-progress" role="status">
                                <LoaderCircle aria-hidden="true" className="membership-payment-setup-progress-icon" />
                                <strong className="membership-payment-setup-progress-title">{openingCheckout ? "正在打开安全收银台" : "正在创建付款订单"}</strong>
                                <p className="membership-payment-setup-progress-copy">订单与付款码均以支付服务返回的真实状态为准。</p>
                            </div>
                        ) : null}
                    </div>
                )}
            </aside>
        </div>
    );
}
