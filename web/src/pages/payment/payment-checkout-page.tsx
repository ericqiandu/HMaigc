import { useCallback } from "react";
import { useNavigate, useParams } from "react-router";

import { PaymentCheckoutExperience, type PaymentCheckoutExitDestination } from "./payment-checkout-experience";

export default function PaymentCheckoutPage() {
    const navigate = useNavigate();
    const { token = "" } = useParams();
    const exitCheckout = useCallback(
        (destination: PaymentCheckoutExitDestination) => {
            navigate(destination);
        },
        [navigate],
    );

    return <PaymentCheckoutExperience mode="page" onExit={exitCheckout} token={token} />;
}
