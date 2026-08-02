import { useState, type ComponentProps, type ReactNode } from "react";
import { Button, Popover } from "antd";

import { staticAssetURL } from "@/lib/static-assets";
import type { AiConfig } from "@/stores/use-config-store";
import type {
    CanvasAgentExecutionMode,
    CanvasAgentGenerationModels,
    CanvasAgentSkillSelection,
} from "@/types/canvas";
import { CanvasAgentModeMenu } from "./canvas-agent-mode-menu";
import { CanvasAgentModelMenu } from "./canvas-agent-model-menu";
import { CanvasAgentSkillMenu } from "./canvas-agent-skill-menu";
import { CanvasAgentTooltip } from "./canvas-agent-tooltip";
import "./canvas-agent-composer-controls.css";

type AgentComposerPopover = "models" | "skills" | "mode";
type AgentComposerIconVariant = "model" | "skills" | "mode";

type CanvasAgentComposerControlsProps = {
    config: AiConfig;
    disabled?: boolean;
    models: CanvasAgentGenerationModels;
    selectedSkills: CanvasAgentSkillSelection[];
    executionMode: CanvasAgentExecutionMode;
    placement?: ComponentProps<typeof Popover>["placement"];
    onModelsChange: (models: CanvasAgentGenerationModels) => void;
    onSkillsChange: (skills: CanvasAgentSkillSelection[]) => void;
    onExecutionModeChange: (mode: CanvasAgentExecutionMode) => void;
};

export function CanvasAgentComposerControls({
    config,
    disabled,
    models,
    selectedSkills,
    executionMode,
    placement = "top",
    onModelsChange,
    onSkillsChange,
    onExecutionModeChange,
}: CanvasAgentComposerControlsProps) {
    const [activePopover, setActivePopover] = useState<AgentComposerPopover | null>(null);

    return (
        <div className="canvas-agent-composer-controls">
            <ComposerPopover
                label="选择模型"
                icon={`${staticAssetURL("/icons/agent-model.svg")}?v=2`}
                iconVariant="model"
                placement={placement}
                open={activePopover === "models"}
                disabled={disabled}
                onOpenChange={(open) => setActivePopover(open ? "models" : null)}
                content={<CanvasAgentModelMenu config={config} value={models} onChange={onModelsChange} />}
            />
            <ComposerPopover
                label="Skills"
                icon={staticAssetURL("/icons/agent-skills.svg")}
                iconVariant="skills"
                placement={placement}
                open={activePopover === "skills"}
                disabled={disabled}
                onOpenChange={(open) => setActivePopover(open ? "skills" : null)}
                content={<CanvasAgentSkillMenu selectedSkills={selectedSkills} onChange={onSkillsChange} />}
            />
            <ComposerPopover
                label="生成模式"
                icon={staticAssetURL(executionMode === "guided" ? "/icons/agent-mode-manual.svg" : "/icons/agent-mode-automatic.svg")}
                iconVariant="mode"
                placement={placement}
                open={activePopover === "mode"}
                disabled={disabled}
                onOpenChange={(open) => setActivePopover(open ? "mode" : null)}
                content={
                    <CanvasAgentModeMenu
                        value={executionMode}
                        onChange={(mode) => {
                            onExecutionModeChange(mode);
                            setActivePopover(null);
                        }}
                    />
                }
            />
        </div>
    );
}

function ComposerPopover({
    label,
    icon,
    iconVariant,
    placement,
    open,
    disabled,
    content,
    onOpenChange,
}: {
    label: string;
    icon: string;
    iconVariant: AgentComposerIconVariant;
    placement: ComponentProps<typeof Popover>["placement"];
    open: boolean;
    disabled?: boolean;
    content: ReactNode;
    onOpenChange: (open: boolean) => void;
}) {
    return (
        <CanvasAgentTooltip title={label}>
            <Popover
                arrow={false}
                trigger="click"
                placement={placement}
                open={open}
                onOpenChange={onOpenChange}
                content={content}
                rootClassName="canvas-overlay-popover canvas-agent-composer-popover"
            >
                <Button
                    type="text"
                    className={`canvas-agent-composer-tool canvas-agent-composer-picker-trigger ${open ? "canvas-agent-composer-picker-trigger--active" : ""}`}
                    disabled={disabled}
                    icon={
                        <img
                            className={`canvas-agent-composer-picker-icon canvas-agent-composer-picker-icon--${iconVariant}`}
                            src={icon}
                            alt=""
                            aria-hidden="true"
                        />
                    }
                    aria-label={label}
                    aria-expanded={open}
                />
            </Popover>
        </CanvasAgentTooltip>
    );
}
