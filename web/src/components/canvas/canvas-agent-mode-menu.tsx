import { Check, Cpu, Hand } from "lucide-react";

import { cn } from "@/lib/utils";
import type { CanvasAgentExecutionMode } from "@/types/canvas";

const modes: Array<{ value: CanvasAgentExecutionMode; label: string; description: string }> = [
    { value: "guided", label: "手动模式", description: "Agent 在每次生成前询问" },
    { value: "automatic", label: "自动模式", description: "Agent 完全自动生成" },
];

export function CanvasAgentModeMenu({
    value,
    onChange,
}: {
    value: CanvasAgentExecutionMode;
    onChange: (value: CanvasAgentExecutionMode) => void;
}) {
    return (
        <section className="canvas-overlay-panel canvas-agent-picker canvas-agent-mode-menu" aria-label="生成模式">
            {modes.map((mode) => {
                const selected = value === mode.value;
                return (
                    <button
                        key={mode.value}
                        type="button"
                        className={cn("canvas-agent-mode-row", selected && "canvas-agent-mode-row--selected")}
                        aria-pressed={selected}
                        onClick={() => onChange(mode.value)}
                    >
                        {mode.value === "guided"
                            ? <Hand className="canvas-agent-mode-icon" strokeWidth={1.8} aria-hidden="true" />
                            : <Cpu className="canvas-agent-mode-icon" strokeWidth={1.8} aria-hidden="true" />}
                        <span className="canvas-agent-mode-copy">
                            <span className="canvas-agent-mode-name">{mode.label}</span>
                            <span className="canvas-agent-mode-description">{mode.description}</span>
                        </span>
                        {selected ? <Check className="canvas-agent-mode-check" aria-hidden="true" /> : null}
                    </button>
                );
            })}
        </section>
    );
}
