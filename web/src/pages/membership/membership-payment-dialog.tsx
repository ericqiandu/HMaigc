import { Modal } from "antd";
import { useEffect, useRef, useState, type ReactElement } from "react";

import type { MembershipPlan, Team } from "@/services/api/membership";

import { PaymentCheckoutExperience } from "../payment/payment-checkout-experience";
import type { MembershipOrderLifecycle } from "../payment/membership-order-facts-domain";
import { MembershipPaymentSetup } from "./membership-payment-setup";

export type MembershipPaymentDialogProps = {
    checkoutToken: string;
    className?: string;
    creationError: string;
    onClose: () => void;
    onConfirm: () => void;
    onPlanChange: (planID: string) => void;
    onRetry: () => void;
    onSeatsChange: (seats: number) => void;
    onTeamIdChange: (teamId: string | undefined) => void;
    onTeamNameChange: (teamName: string) => void;
    open: boolean;
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

export function MembershipPaymentDialog({
    checkoutToken,
    className = "",
    creationError,
    onClose,
    onConfirm,
    onPlanChange,
    onRetry,
    onSeatsChange,
    onTeamIdChange,
    onTeamNameChange,
    open,
    openingCheckout,
    orderLifecycle,
    plan,
    planOptions,
    seats,
    submitting,
    teamId,
    teamName,
    teams,
}: MembershipPaymentDialogProps): ReactElement {
    const [checkoutWriting, setCheckoutWriting] = useState(false);
    const submitGuardRef = useRef(false);
    const observedWriteRef = useRef(false);
    const writeInFlight = submitting || openingCheckout || checkoutWriting;
    const teamDialog = plan?.audience === "team" || (orderLifecycle.kind === "frozen-ready" && orderLifecycle.facts.audience === "team");

    useEffect(() => {
        if (!open) {
            submitGuardRef.current = false;
            observedWriteRef.current = false;
            setCheckoutWriting(false);
            return;
        }
        if (writeInFlight) {
            observedWriteRef.current = true;
            return;
        }
        if (observedWriteRef.current) {
            submitGuardRef.current = false;
            observedWriteRef.current = false;
        }
    }, [open, writeInFlight]);

    const requestClose = () => {
        if (writeInFlight) return;
        onClose();
    };

    const requestConfirm = () => {
        if (writeInFlight || submitGuardRef.current) return;
        submitGuardRef.current = true;
        onConfirm();
    };

    return (
        <Modal
            className={`membership-payment-dialog ${checkoutToken ? "is-checkout" : "is-setup"} ${teamDialog ? "is-team" : "is-personal"} ${className}`}
            closable={!writeInFlight}
            destroyOnHidden
            footer={null}
            keyboard={!writeInFlight}
            maskClosable={!writeInFlight}
            onCancel={requestClose}
            open={open}
            title={null}
            width={teamDialog ? 880 : 766}
        >
            <div className="membership-payment-dialog-content">
                {checkoutToken ? (
                    <PaymentCheckoutExperience
                        initialMembershipFacts={orderLifecycle.kind === "frozen-ready" ? orderLifecycle.facts : undefined}
                        membershipPlanOptions={teamDialog ? planOptions : undefined}
                        mode="dialog"
                        onExit={() => onClose()}
                        onWriteStateChange={setCheckoutWriting}
                        selectedMembershipPlanID={teamDialog ? plan?.id : undefined}
                        teamSeatBounds={teamDialog && plan ? { maxSeats: plan.maxSeats, minSeats: plan.minSeats } : undefined}
                        token={checkoutToken}
                    />
                ) : (
                    <MembershipPaymentSetup
                        creationError={creationError}
                        onClose={requestClose}
                        onConfirm={requestConfirm}
                        onPlanChange={onPlanChange}
                        onRetry={onRetry}
                        onSeatsChange={onSeatsChange}
                        onTeamIdChange={onTeamIdChange}
                        onTeamNameChange={onTeamNameChange}
                        openingCheckout={openingCheckout}
                        orderLifecycle={orderLifecycle}
                        plan={plan}
                        planOptions={planOptions}
                        seats={seats}
                        submitting={submitting}
                        teamId={teamId}
                        teamName={teamName}
                        teams={teams}
                    />
                )}
            </div>
        </Modal>
    );
}
