import { Button, Input, Select } from "antd";
import { ArrowRight, LoaderCircle, ShieldCheck } from "lucide-react";
import type { ReactElement } from "react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { MembershipOrderFacts, MembershipOrderFactsSkeleton } from "../payment/membership-order-facts";
import { membershipOrderFactsFromPlan, type MembershipOrderLifecycle } from "../payment/membership-order-facts-domain";

export type MembershipPaymentSetupProps = {
    creationError: string;
    onClose: () => void;
    onConfirm: () => void;
    onPlanChange: (planID: string) => void;
    onRetry: () => void;
    onSeatsChange: (seats: number) => void;
    onTeamIdChange: (teamId: string | undefined) => void;
    onTeamNameChange: (teamName: string) => void;
    openingCheckout: boolean;
    orderLifecycle: MembershipOrderLifecycle;
    plan: MembershipPlan | null;
    planOptions: MembershipPlan[];
    seats: number;
    submitting: boolean;
    teamId?: string;
    teamName: string;
    teams: Team[];
};

export function MembershipPaymentSetup({
    creationError,
    onClose,
    onConfirm,
    onPlanChange,
    onRetry,
    onSeatsChange,
    onTeamIdChange,
    onTeamNameChange,
    openingCheckout,
    orderLifecycle,
    plan,
    planOptions,
    seats,
    submitting,
    teamId,
    teamName,
    teams,
}: MembershipPaymentSetupProps): ReactElement {
    const isTeamPlan = plan?.audience === "team";
    const appliedSeats = plan && isTeamPlan ? Math.min(plan.maxSeats, Math.max(plan.minSeats, seats)) : 1;
    const facts = orderLifecycle.kind === "frozen-ready" ? orderLifecycle.facts : orderLifecycle.kind === "preorder" && plan ? membershipOrderFactsFromPlan(plan, appliedSeats) : null;
    const writeInFlight = submitting || openingCheckout;
    const hasVisibleError = orderLifecycle.kind === "frozen-invalid" || creationError.length > 0;
    const editableTeamPlan = orderLifecycle.kind === "preorder" && plan?.audience === "team" ? plan : null;
    const teamPlan = plan?.audience === "team" ? plan : null;
    const teamSeatControl = teamPlan
        ? {
              disabled: !editableTeamPlan || writeInFlight,
              maxSeats: teamPlan.maxSeats,
              minSeats: teamPlan.minSeats,
              onChange: editableTeamPlan ? onSeatsChange : undefined,
          }
        : undefined;

    return (
        <div className={`payment-checkout-shell is-dialog membership-payment-setup ${teamPlan ? "is-team" : "is-personal"}`}>
            <div className="payment-checkout-order-surface">
                {facts ? (
                    <MembershipOrderFacts
                        facts={facts}
                        onTeamPlanChange={editableTeamPlan && !writeInFlight ? onPlanChange : undefined}
                        selectedTeamPlanID={teamPlan?.id}
                        teamPlanOptions={teamPlan ? planOptions : undefined}
                        teamSeatControl={teamSeatControl}
                    />
                ) : (
                    <MembershipOrderFactsSkeleton />
                )}
            </div>
            <aside aria-label="创建付款码" className="payment-checkout-payment-surface membership-payment-setup-action">
                {orderLifecycle.kind === "frozen-invalid" ? (
                    <div className="membership-payment-setup-error membership-payment-setup-frozen-error" role="alert">
                        <strong className="membership-payment-setup-error-title">订单冻结事实无效</strong>
                        <p className="membership-payment-setup-error-copy">{orderLifecycle.error}</p>
                        <p className="membership-payment-setup-error-note">该订单无法验证，已阻止继续打开收银台。</p>
                        <Button className="membership-payment-setup-primary" disabled={writeInFlight} onClick={onClose} type="primary">
                            关闭付款窗口
                        </Button>
                    </div>
                ) : null}
                {creationError ? (
                    <div className="membership-payment-setup-error membership-payment-setup-server-error" role="alert">
                        <strong className="membership-payment-setup-error-title">{orderLifecycle.kind === "frozen-ready" ? `订单 ${orderLifecycle.facts.orderNumber} 已创建` : "付款订单创建失败"}</strong>
                        <p className="membership-payment-setup-error-copy">{creationError}</p>
                        {orderLifecycle.kind === "frozen-ready" ? <p className="membership-payment-setup-error-note">重新打开付款码不会重复创建订单。</p> : null}
                        {orderLifecycle.kind !== "frozen-invalid" && !editableTeamPlan ? (
                            <Button className="membership-payment-setup-primary" disabled={writeInFlight} onClick={onRetry} type="primary">
                                {orderLifecycle.kind === "frozen-ready" ? "重新打开付款码" : "重试创建付款订单"}
                            </Button>
                        ) : null}
                    </div>
                ) : null}
                {!editableTeamPlan && !hasVisibleError ? (
                    <div aria-live="polite" className="membership-payment-setup-progress" role="status">
                        <LoaderCircle aria-hidden="true" className="membership-payment-setup-progress-icon" />
                        <strong className="membership-payment-setup-progress-title">{openingCheckout || orderLifecycle.kind === "frozen-ready" ? "正在打开安全收银台" : "正在创建付款订单"}</strong>
                        <p className="membership-payment-setup-progress-copy">订单与付款码均以支付服务返回的真实状态为准。</p>
                    </div>
                ) : editableTeamPlan ? (
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
                        </div>
                        <ShieldCheck aria-hidden="true" className="membership-payment-setup-confirmation-icon" />
                        <strong className="membership-payment-setup-confirmation-title">确认团队与席位配置</strong>
                        <p className="membership-payment-setup-confirmation-copy">确认后创建冻结订单，付款码生成后不能修改团队、席位或金额。</p>
                        <Button className="membership-payment-setup-primary" disabled={writeInFlight} icon={<ArrowRight aria-hidden="true" className="membership-payment-setup-primary-icon" />} onClick={onConfirm} type="primary">
                            {creationError ? "使用当前配置重试" : "确认配置并生成付款码"}
                        </Button>
                        {writeInFlight ? (
                            <div aria-live="polite" className="membership-payment-setup-progress" role="status">
                                <LoaderCircle aria-hidden="true" className="membership-payment-setup-progress-icon" />
                                <strong className="membership-payment-setup-progress-title">{openingCheckout ? "正在打开安全收银台" : "正在创建付款订单"}</strong>
                                <p className="membership-payment-setup-progress-copy">订单与付款码均以支付服务返回的真实状态为准。</p>
                            </div>
                        ) : null}
                    </div>
                ) : null}
            </aside>
        </div>
    );
}
