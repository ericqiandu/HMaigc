import { motion, useReducedMotion } from "motion/react";
import type { CSSProperties, ReactNode } from "react";

import { aceternityMotion } from "@/lib/aceternity-motion";
import { canvasThemes } from "@/lib/canvas-theme";
import { cn } from "@/lib/utils";
import { useThemeStore } from "@/stores/use-theme-store";

export type CanvasCreateCommand = {
    id: string;
    label: string;
    icon: ReactNode;
    badge?: string;
    onClick: () => void;
};

export function CanvasCreateCommandSections({ nodeCommands, resourceCommands }: { nodeCommands: CanvasCreateCommand[]; resourceCommands: CanvasCreateCommand[] }) {
    return (
        <div className="canvas-create-command-sections min-w-0 w-full max-w-full overflow-hidden">
            <h4 className="canvas-create-command-section-heading m-0 flex h-8 items-center px-2 py-0 text-xs font-medium leading-4 opacity-60">添加节点</h4>
            <CanvasCreateCommandGrid commands={nodeCommands} layout="list" />
            <h4 className="canvas-create-command-section-heading m-0 flex h-8 items-center px-2 py-0 text-xs font-medium leading-4 opacity-60">添加资源</h4>
            <CanvasCreateCommandGrid commands={resourceCommands} variant="resource" layout="list" />
        </div>
    );
}

export function CanvasCreateCommandGrid({ commands, variant = "node", layout = "grid" }: { commands: CanvasCreateCommand[]; variant?: "node" | "resource"; layout?: "grid" | "list" }) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const reducedMotion = useReducedMotion();
    return (
        <div className={cn("min-w-0 w-full max-w-full", layout === "list" ? "canvas-overlay-command-list flex flex-col gap-1" : "canvas-overlay-command-grid grid gap-1", layout === "grid" && (variant === "node" ? "grid-cols-3 sm:grid-cols-4" : "grid-cols-2"))}>
            {commands.map((command) => (
                <motion.button
                    key={command.id}
                    type="button"
                    whileHover={reducedMotion || layout === "list" ? undefined : { y: -2, scale: 1.02 }}
                    whileTap={reducedMotion ? undefined : { scale: layout === "list" ? 0.98 : 0.96 }}
                    transition={aceternityMotion.spring.dock}
                    className={cn(
                        "canvas-overlay-command group relative min-w-0 max-w-full text-center outline-none transition-colors focus-visible:ring-2",
                        layout === "list"
                            ? "canvas-overlay-command--list flex h-8 w-full items-center gap-2 px-2 text-left"
                            : variant === "node"
                              ? "flex h-12 flex-col items-center justify-center gap-1"
                              : "flex h-9 items-center justify-center gap-1.5 px-2",
                    )}
                    style={{ color: theme.node.text, "--tw-ring-color": theme.node.muted } as CSSProperties}
                    title={command.label}
                    onMouseDown={(event) => event.stopPropagation()}
                    onClick={command.onClick}
                >
                    <span className={cn("canvas-overlay-command-icon grid shrink-0 place-items-center opacity-65 transition-opacity group-hover:opacity-100", layout === "list" ? "size-3.5 [&_svg]:size-3.5" : "size-5 [&_svg]:size-3.5")}>
                        {command.icon}
                    </span>
                    <span className={cn("canvas-overlay-command-label max-w-full truncate", layout === "list" ? "min-w-0 flex-1 text-[13px] font-normal leading-5" : "text-[11px] font-medium leading-4")}>{command.label}</span>
                    {command.badge ? (
                        <span
                            className={cn("canvas-overlay-command-badge rounded border font-semibold leading-none", layout === "list" ? "shrink-0 px-1.5 py-0.5 text-[11px]" : "absolute right-1 top-1 rounded-full px-1.5 py-0.5 text-[11px]")}
                            style={{ background: theme.toolbar.activeBg, borderColor: theme.toolbar.border, color: theme.node.muted }}
                        >
                            {command.badge}
                        </span>
                    ) : null}
                </motion.button>
            ))}
        </div>
    );
}
