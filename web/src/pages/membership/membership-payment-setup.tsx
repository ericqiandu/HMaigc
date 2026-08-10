import { Button, Input, InputNumber, Select } from "antd";
import { ArrowRight, LoaderCircle, ShieldCheck } from "lucide-react";
import type { ReactElement } from "react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { billingCycleLabel, formatCredits, planTotalCredits, planTotalPriceCents, publicPlanName } from "./membership-formatters";

export type MembershipPaymentSetupProps = {
    createdOrderNumber: string;
    creationError: string;
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

function formatPlanMoney(plan: MembershipPlan, valueCents: number): string {
    return new Intl.NumberFormat("zh-CN", {
        currency: plan.currency.trim().toUpperCase(),
        currencyDisplay: "narrowSymbol",
        maximumFractionDigits: 2,
        minimumFractionDigits: 0,
        style: "currency",
    }).format(valueCents / 100);
}

export function MembershipPaymentSetup({
    createdOrderNumber,
    creationError,
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
    const writeInFlight = submitting || openingCheckout;

    return (
        <div className="membership-payment-setup">
            <section aria-label="会员订单配置" className="membership-payment-setup-order">
                <header className="membership-payment-dialog-heading">
                    <p className="membership-payment-dialog-eyebrow">{teamPlan ? "开通团队会员" : "开通创作会员"}</p>
                    <h1 className="membership-payment-dialog-title">{plan ? publicPlanName(plan) : "恢复付款订单"}</h1>
                    {createdOrderNumber ? <p className="membership-payment-dialog-order-number">订单 {createdOrderNumber}</p> : null}
                </header>

                {plan ? (
                    <div className="membership-payment-setup-facts">
                        <section aria-label="商品信息" className="membership-payment-product">
                            <div className="membership-payment-product-copy">
                                <strong className="membership-payment-product-name">{publicPlanName(plan)}</strong>
                                <span className="membership-payment-product-cycle">{billingCycleLabel[plan.billingCycle]}</span>
                            </div>
                            <div className="membership-payment-product-price">
                                <strong className="membership-payment-product-price-value">{formatPlanMoney(plan, plan.priceCents)}</strong>
                                <span className="membership-payment-product-price-suffix">
                                    /{plan.billingCycle === "year" ? "年" : "月"}
                                    {teamPlan ? "/席位" : ""}
                                </span>
                            </div>
                        </section>

                        {teamPlan ? (
                            <section aria-label="团队购买配置" className="membership-payment-team-fields">
                                <label className="membership-payment-team-field">
                                    <span className="membership-payment-team-field-label">开通团队</span>
                                    {teams.length > 0 ? (
                                        <Select
                                            className="membership-payment-team-select"
                                            disabled={writeInFlight}
                                            onChange={onTeamIdChange}
                                            options={teams.map((team) => ({ label: team.name, value: team.id }))}
                                            placeholder="选择团队"
                                            value={teamId}
                                        />
                                    ) : (
                                        <Input
                                            className="membership-payment-team-name-input"
                                            disabled={writeInFlight}
                                            onChange={(event) => onTeamNameChange(event.target.value)}
                                            placeholder="输入新团队名称"
                                            value={teamName}
                                        />
                                    )}
                                </label>
                                <label className="membership-payment-team-field">
                                    <span className="membership-payment-team-field-label">席位数量</span>
                                    <InputNumber
                                        className="membership-payment-team-seat-input"
                                        disabled={writeInFlight}
                                        max={plan.maxSeats}
                                        min={plan.minSeats}
                                        onChange={(value) => onSeatsChange(value ?? plan.minSeats)}
                                        value={appliedSeats}
                                    />
                                </label>
                            </section>
                        ) : null}

                        <dl className="membership-payment-preview">
                            <div className="membership-payment-preview-row">
                                <dt className="membership-payment-preview-label">{teamPlan ? "席位数量" : "购买周期"}</dt>
                                <dd className="membership-payment-preview-value">{teamPlan ? `${appliedSeats} 席位` : billingCycleLabel[plan.billingCycle]}</dd>
                            </div>
                            <div className="membership-payment-preview-row">
                                <dt className="membership-payment-preview-label">{teamPlan ? "团队积分合计" : "到账积分"}</dt>
                                <dd className="membership-payment-preview-value">{formatCredits(planTotalCredits(plan, appliedSeats))} 积分</dd>
                            </div>
                            <div className="membership-payment-preview-row is-total">
                                <dt className="membership-payment-preview-label">应付金额</dt>
                                <dd className="membership-payment-preview-total">{formatPlanMoney(plan, planTotalPriceCents(plan, appliedSeats))}</dd>
                            </div>
                        </dl>
                    </div>
                ) : (
                    <div aria-label="正在恢复订单" className="membership-payment-setup-placeholder" role="status">
                        <span className="membership-payment-setup-placeholder-line" />
                        <span className="membership-payment-setup-placeholder-line is-short" />
                    </div>
                )}
            </section>

            <aside aria-label="创建付款码" className="membership-payment-setup-action">
                {creationError ? (
                    <div className="membership-payment-setup-error" role="alert">
                        <strong className="membership-payment-setup-error-title">{createdOrderNumber ? `订单 ${createdOrderNumber} 已创建` : "付款订单创建失败"}</strong>
                        <p className="membership-payment-setup-error-copy">{creationError}</p>
                        {createdOrderNumber ? <p className="membership-payment-setup-error-note">重新打开付款码不会重复创建订单。</p> : null}
                        <Button className="membership-payment-setup-primary" onClick={onRetry} type="primary">
                            {createdOrderNumber ? "重新打开付款码" : "重试创建付款订单"}
                        </Button>
                    </div>
                ) : writeInFlight || !plan || !teamPlan ? (
                    <div aria-live="polite" className="membership-payment-setup-progress" role="status">
                        <LoaderCircle aria-hidden="true" className="membership-payment-setup-progress-icon" />
                        <strong className="membership-payment-setup-progress-title">{openingCheckout ? "正在打开安全收银台" : "正在创建付款订单"}</strong>
                        <p className="membership-payment-setup-progress-copy">订单与付款码均以支付服务返回的真实状态为准。</p>
                    </div>
                ) : (
                    <div className="membership-payment-setup-confirmation">
                        <ShieldCheck aria-hidden="true" className="membership-payment-setup-confirmation-icon" />
                        <strong className="membership-payment-setup-confirmation-title">确认团队与席位配置</strong>
                        <p className="membership-payment-setup-confirmation-copy">确认后创建冻结订单，付款码生成后不能修改团队、席位或金额。</p>
                        <Button className="membership-payment-setup-primary" icon={<ArrowRight aria-hidden="true" className="membership-payment-setup-primary-icon" />} onClick={onConfirm} type="primary">
                            确认配置并生成付款码
                        </Button>
                    </div>
                )}
            </aside>
        </div>
    );
}
