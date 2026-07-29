import type { ReactElement, ReactNode } from "react";
import { Tooltip } from "antd";

import "./canvas-agent-tooltip.css";

const TOOLTIP_CLASS_NAMES = {
    root: "canvas-agent-composer-tooltip",
    container: "canvas-agent-composer-tooltip-content",
    arrow: "canvas-agent-composer-tooltip-arrow",
} as const;

export function CanvasAgentTooltip({
    title,
    children,
}: {
    title: ReactNode;
    children: ReactElement;
}) {
    return (
        <Tooltip
            title={title}
            classNames={TOOLTIP_CLASS_NAMES}
            color="#05070b"
            trigger={["hover", "focus"]}
        >
            {children}
        </Tooltip>
    );
}
