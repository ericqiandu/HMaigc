import { ArrowUp, LoaderCircle, Square } from "lucide-react";
import { Button } from "antd";
import type { ReactElement } from "react";

import { cn } from "@/lib/utils";
import "./canvas-submit-button.css";

export type CanvasSubmitButtonState = "ready" | "loading" | "stop";

type CanvasSubmitButtonProps = {
    state: CanvasSubmitButtonState;
    disabled?: boolean;
    ariaLabel: string;
    onClick: () => void;
    className?: string;
};

export function CanvasSubmitButton({ state, disabled = false, ariaLabel, onClick, className }: CanvasSubmitButtonProps): ReactElement {
    return (
        <Button
            type="text"
            className={cn("canvas-submit-button", `canvas-submit-button--${state}`, className)}
            disabled={disabled}
            aria-label={ariaLabel}
            icon={submitIcon(state)}
            onClick={onClick}
        />
    );
}

function submitIcon(state: CanvasSubmitButtonState): ReactElement {
    if (state === "loading") return <LoaderCircle className="canvas-submit-button-icon animate-spin" strokeWidth={1.8} aria-hidden="true" />;
    if (state === "stop") return <Square className="canvas-submit-button-icon canvas-submit-button-stop-icon" strokeWidth={1.8} aria-hidden="true" />;
    return <ArrowUp className="canvas-submit-button-icon" strokeWidth={1.8} aria-hidden="true" />;
}
