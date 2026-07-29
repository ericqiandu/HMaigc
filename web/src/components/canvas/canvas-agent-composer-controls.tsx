import { useState, type ReactNode } from "react";
import { Button, Popover, Tooltip } from "antd";

import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasAgentExecutionMode } from "@/types/canvas";
import type { UpdreamSkill } from "@/services/api/skills";
import { CanvasAgentModeMenu } from "./canvas-agent-mode-menu";
import { CanvasAgentModelMenu, type CanvasAgentGenerationModels } from "./canvas-agent-model-menu";
import { CanvasAgentSkillMenu } from "./canvas-agent-skill-menu";

type AgentComposerPopover = "models" | "skills" | "mode";
type AgentComposerIconVariant = "model" | "skills" | "mode";

type CanvasAgentComposerControlsProps = {
    config: AiConfig;
    disabled?: boolean;
    models: CanvasAgentGenerationModels;
    selectedSkills: UpdreamSkill[];
    executionMode: CanvasAgentExecutionMode;
    onModelsChange: (models: CanvasAgentGenerationModels) => void;
    onSkillsChange: (skills: UpdreamSkill[]) => void;
    onExecutionModeChange: (mode: CanvasAgentExecutionMode) => void;
};

export function CanvasAgentComposerControls({
    config,
    disabled,
    models,
    selectedSkills,
    executionMode,
    onModelsChange,
    onSkillsChange,
    onExecutionModeChange,
}: CanvasAgentComposerControlsProps) {
    const [activePopover, setActivePopover] = useState<AgentComposerPopover | null>(null);

    return (
        <div className="canvas-agent-composer-controls">
            <ComposerPopover
                label="选择模型"
                icon="/icons/agent-model.svg"
                iconVariant="model"
                open={activePopover === "models"}
                disabled={disabled}
                onOpenChange={(open) => setActivePopover(open ? "models" : null)}
                content={<CanvasAgentModelMenu config={config} value={models} onChange={onModelsChange} />}
            />
            <ComposerPopover
                label="Skills"
                icon="/icons/agent-skills.svg"
                iconVariant="skills"
                open={activePopover === "skills"}
                disabled={disabled}
                onOpenChange={(open) => setActivePopover(open ? "skills" : null)}
                content={<CanvasAgentSkillMenu selectedSkills={selectedSkills} onChange={onSkillsChange} />}
            />
            <ComposerPopover
                label="生成模式"
                icon={executionMode === "guided" ? "/icons/agent-mode-manual.svg" : "/icons/agent-mode-automatic.svg"}
                iconVariant="mode"
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
    open,
    disabled,
    content,
    onOpenChange,
}: {
    label: string;
    icon: string;
    iconVariant: AgentComposerIconVariant;
    open: boolean;
    disabled?: boolean;
    content: ReactNode;
    onOpenChange: (open: boolean) => void;
}) {
    return (
        <Popover
            arrow={false}
            trigger="click"
            placement="top"
            open={open}
            onOpenChange={onOpenChange}
            content={content}
            overlayClassName="canvas-agent-composer-popover"
        >
            <Tooltip title={label}>
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
            </Tooltip>
        </Popover>
    );
}
