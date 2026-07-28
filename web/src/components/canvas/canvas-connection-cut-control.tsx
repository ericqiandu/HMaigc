import { Scissors } from "lucide-react";
import type { PointerEvent as ReactPointerEvent } from "react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import type { Position } from "@/types/canvas";

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
        <button
            className="canvas-connection-cut-control pointer-events-auto absolute z-[80] flex h-11 w-11 items-center justify-center rounded-full border shadow-lg transition-[background-color,color,transform] duration-150 hover:brightness-110 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 active:brightness-95"
            type="button"
            aria-label="断开这条连线"
            title="断开连线"
            data-canvas-no-zoom
            style={{
                left: position.x,
                top: position.y,
                color: theme.node.text,
                background: theme.spatial.elevated,
                borderColor: theme.toolbar.border,
                boxShadow: `0 10px 28px ${theme.spatial.shadow}`,
                transform: `translate(-50%, -50%) scale(${1 / viewportScale})`,
                transformOrigin: "center",
                outlineColor: theme.accent.primary,
            }}
            onPointerDown={stopPointerPropagation}
            onClick={(event) => {
                event.stopPropagation();
                onCut();
            }}
        >
            <Scissors className="canvas-connection-cut-icon h-5 w-5" aria-hidden="true" strokeWidth={1.8} />
        </button>
    );
}
