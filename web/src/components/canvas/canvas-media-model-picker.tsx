import { Popover } from "antd";
import { ChevronDown } from "lucide-react";

import { isMemberModel, MemberDiamond, ModelIcon } from "@/components/model-picker-presentation";
import { cn } from "@/lib/utils";
import { catalogModelsByCapability, isModelAccessible, modelDisplayName, type AiConfig } from "@/stores/use-config-store";
import { CanvasModelSelectionMenu, type CanvasModelSelectionCapability } from "./canvas-model-selection-menu";

type CanvasMediaModelPickerProps = {
    capability: CanvasModelSelectionCapability;
    className?: string;
    config: AiConfig;
    current: string;
    open: boolean;
    pickerId: string;
    placeholder: string;
    onChange: (model: string) => void;
    onMissingConfig?: () => void;
    onOpenChange: (open: boolean) => void;
};

export function CanvasMediaModelPicker({ capability, className, config, current, open, pickerId, placeholder, onChange, onMissingConfig, onOpenChange }: CanvasMediaModelPickerProps) {
    const displayName = current ? modelDisplayName(config, current) : placeholder;
    const models = catalogModelsByCapability(config, capability);
    const selectedModels = { [capability]: current };

    return (
        <div className={cn("model-picker-root model-picker-root--content min-w-0 max-w-full", className)} onMouseDown={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()}>
            <Popover
                arrow={false}
                trigger="click"
                placement="topLeft"
                autoAdjustOverflow
                open={open}
                onOpenChange={(nextOpen) => {
                    if (nextOpen && !models.length && config.channelMode === "local") onMissingConfig?.();
                    if (nextOpen) window.dispatchEvent(new CustomEvent("model-picker-open", { detail: pickerId }));
                    onOpenChange(nextOpen);
                }}
                content={
                    <CanvasModelSelectionMenu
                        config={config}
                        value={selectedModels}
                        capabilities={[capability]}
                        modelSource="catalog"
                        onChange={(nextValue) => {
                            const model = nextValue[capability];
                            if (!model || !isModelAccessible(config, model)) return;
                            onChange(model);
                            onOpenChange(false);
                        }}
                    />
                }
                rootClassName="canvas-overlay-popover canvas-model-selection-popover"
            >
                <button type="button" className="canvas-composer-model-picker canvas-model-selection-trigger" aria-label={placeholder} aria-haspopup="dialog" aria-expanded={open} title={displayName}>
                    <span className="canvas-model-picker-label flex min-w-0 items-center gap-1.5">
                        <ModelIcon config={config} model={current} />
                        <span className="canvas-model-picker-label-copy flex min-w-0 items-center gap-1">
                            <span className="canvas-model-picker-label-text min-w-0 truncate">{displayName}</span>
                            {isMemberModel(config, current) ? <MemberDiamond /> : null}
                        </span>
                    </span>
                    <ChevronDown className="canvas-model-selection-trigger-chevron" aria-hidden="true" />
                </button>
            </Popover>
        </div>
    );
}
