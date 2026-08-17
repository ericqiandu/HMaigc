import { Sparkles, X } from "lucide-react";
import type { ReactNode } from "react";

import { ModelIcon } from "@/components/model-picker";
import { modelDisplayName, type AiConfig } from "@/stores/use-config-store";
import type { CanvasAgentGenerationModels, CanvasAgentSkillSelection } from "@/types/canvas";
import "./canvas-agent-selection-summary.css";

type CanvasAgentSelectionSummaryProps = {
    config: AiConfig;
    models: CanvasAgentGenerationModels;
    selectedSkills: CanvasAgentSkillSelection[];
    disabled?: boolean;
    onModelsChange: (models: CanvasAgentGenerationModels) => void;
    onSkillsChange: (skills: CanvasAgentSkillSelection[]) => void;
};

export function CanvasAgentSelectionSummary({ config, models, selectedSkills, disabled, onModelsChange, onSkillsChange }: CanvasAgentSelectionSummaryProps) {
    const hasSelections = Boolean(models.image || models.video || selectedSkills.length);
    if (!hasSelections) return null;

    return (
        <div className="canvas-agent-selection-summary" aria-label="本次生成已选配置">
            {models.image ? (
                <SelectionChip
                    icon={<ModelIcon config={config} model={models.image} />}
                    label={modelDisplayName(config, models.image)}
                    removeLabel={`移除图片模型 ${modelDisplayName(config, models.image)}`}
                    disabled={disabled}
                    onRemove={() => onModelsChange({ ...models, image: "" })}
                />
            ) : null}
            {models.video ? (
                <SelectionChip
                    icon={<ModelIcon config={config} model={models.video} />}
                    label={modelDisplayName(config, models.video)}
                    removeLabel={`移除视频模型 ${modelDisplayName(config, models.video)}`}
                    disabled={disabled}
                    onRemove={() => onModelsChange({ ...models, video: "" })}
                />
            ) : null}
            {selectedSkills.map((skill) => (
                <SelectionChip
                    key={skill.dir}
                    icon={<Sparkles className="canvas-agent-selection-chip-icon" strokeWidth={1.8} />}
                    label={skill.name}
                    removeLabel={`移除 Skill ${skill.name}`}
                    disabled={disabled}
                    onRemove={() => onSkillsChange(selectedSkills.filter((item) => item.dir !== skill.dir))}
                />
            ))}
        </div>
    );
}

function SelectionChip({ icon, label, removeLabel, disabled, onRemove }: { icon: ReactNode; label: string; removeLabel: string; disabled?: boolean; onRemove: () => void }) {
    return (
        <span className="canvas-agent-selection-chip" title={label}>
            <span className="canvas-agent-selection-chip-leading" aria-hidden="true">
                {icon}
            </span>
            <span className="canvas-agent-selection-chip-label">{label}</span>
            <button type="button" className="canvas-agent-selection-chip-remove" disabled={disabled} aria-label={removeLabel} onClick={onRemove}>
                <X className="canvas-agent-selection-chip-remove-icon" strokeWidth={1.8} />
            </button>
        </span>
    );
}
