import { Scissors } from "lucide-react";
import type { PointerEvent as ReactPointerEvent } from "react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import type { Position } from "@/types/canvas";

import "./canvas-connection-cut-control.css";

type CanvasConnectionCutControlProps = {
    position: Position;
    viewportScale: number;
    theme: CanvasTheme;
    onCut: () => void;
};

export function CanvasConnectionCutControl({
    position,
    viewportScale,
    theme,
    onCut,
}: CanvasConnectionCutControlProps) {
    const stopPointerPropagation = (event: ReactPointerEvent<HTMLButtonElement>) => {
        event.stopPropagation();
    };

    return (
        <div
            className="canvas-connection-cut-control-anchor pointer-events-none absolute z-[80]"
            style={{
                left: position.x,
                top: position.y,
                transform: `translate3d(-50%, -50%, 0) scale(${1 / viewportScale})`,
                transformOrigin: "center",
            }}
        >
            <button
                className="canvas-connection-cut-control pointer-events-auto flex h-8 w-8 items-center justify-center rounded-full border focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2"
                type="button"
                aria-label="断开这条连线"
                title="断开连线"
                data-canvas-no-zoom
                style={{
                    color: theme.node.text,
                    background: theme.spatial.elevated,
                    borderColor: theme.toolbar.border,
                    boxShadow: `0 4px 12px ${theme.spatial.shadow}`,
                    outlineColor: theme.accent.primary,
                }}
                onPointerDown={stopPointerPropagation}
                onClick={(event) => {
                    event.stopPropagation();
                    onCut();
                }}
            >
                <Scissors className="canvas-connection-cut-icon h-3.5 w-3.5" aria-hidden="true" strokeWidth={1.9} />
            </button>
        </div>
    );
}
