import { useMemo, useState } from "react";
import { Check, Plus } from "lucide-react";

import { ModelIcon, modelCatalogEntry } from "@/components/model-picker-presentation";
import { cn } from "@/lib/utils";
import { catalogModelsByCapability, isModelAccessible, modelDisplayName, selectableModelsByCapability, type AiConfig, type ModelCapability } from "@/stores/use-config-store";
import "./canvas-model-selection-menu.css";

export type CanvasModelSelectionCapability = Extract<ModelCapability, "image" | "video" | "audio">;
export type CanvasModelSelectionValue = Partial<Record<CanvasModelSelectionCapability, string>>;

type CanvasModelSelectionMenuProps = {
    config: AiConfig;
    value: CanvasModelSelectionValue;
    capabilities?: readonly CanvasModelSelectionCapability[];
    modelSource?: "selectable" | "catalog";
    onChange: (value: CanvasModelSelectionValue) => void;
};

const DEFAULT_CAPABILITIES = ["image", "video"] as const satisfies readonly CanvasModelSelectionCapability[];

export function CanvasModelSelectionMenu({ config, value, capabilities = DEFAULT_CAPABILITIES, modelSource = "selectable", onChange }: CanvasModelSelectionMenuProps) {
    const [requestedCapability, setRequestedCapability] = useState<CanvasModelSelectionCapability>(capabilities[0] ?? "image");
    const capability = capabilities.includes(requestedCapability) ? requestedCapability : (capabilities[0] ?? "image");
    const models = useMemo(() => (modelSource === "catalog" ? catalogModelsByCapability(config, capability) : selectableModelsByCapability(config, capability)), [capability, config, modelSource]);
    const capabilityLabel = modelCapabilityLabel(capability);

    return (
        <section className="canvas-overlay-panel canvas-model-selection-menu" role="dialog" aria-label="选择模型">
            <header className="canvas-model-selection-header">
                <h3 className="canvas-model-selection-title">选择模型</h3>
            </header>
            {capabilities.length > 1 ? (
                <div className="canvas-model-selection-segments" role="radiogroup" aria-label="模型类型">
                    {capabilities.map((item) => (
                        <button
                            key={item}
                            type="button"
                            role="radio"
                            aria-checked={capability === item}
                            className={cn("canvas-model-selection-segment", capability === item && "canvas-model-selection-segment--active")}
                            onClick={() => setRequestedCapability(item)}
                        >
                            {modelCapabilityLabel(item)}
                        </button>
                    ))}
                </div>
            ) : null}
            <div className="canvas-model-selection-section-label">{capabilityLabel}</div>
            <div className="canvas-model-selection-list thin-scrollbar">
                {models.length ? (
                    models.map((model) => {
                        const selected = value[capability] === model;
                        const disabled = !isModelAccessible(config, model);
                        const presentation = modelPresentation(config, model);
                        return (
                            <button
                                key={model}
                                type="button"
                                className={cn("canvas-model-selection-row", selected && "canvas-model-selection-row--selected", disabled && "canvas-model-selection-row--disabled")}
                                aria-pressed={selected}
                                disabled={disabled}
                                title={disabled ? "当前账号暂不可用" : undefined}
                                onClick={() => onChange({ ...value, [capability]: model })}
                            >
                                <span className="canvas-model-selection-icon">
                                    <ModelIcon config={config} model={model} />
                                </span>
                                <span className="canvas-model-selection-copy">
                                    <span className="canvas-model-selection-name-line">
                                        <span className="canvas-model-selection-name">{modelDisplayName(config, model)}</span>
                                        {presentation.promotionBadge ? <span className="canvas-model-selection-badge">{presentation.promotionBadge}</span> : null}
                                    </span>
                                    {presentation.marketingCopy ? <span className="canvas-model-selection-description">{presentation.marketingCopy}</span> : null}
                                </span>
                                <span className="canvas-model-selection-action" aria-hidden="true">
                                    {selected ? <Check className="canvas-model-selection-action-icon" /> : <Plus className="canvas-model-selection-action-icon" />}
                                </span>
                            </button>
                        );
                    })
                ) : (
                    <div className="canvas-model-selection-empty" role="status">
                        当前没有可用的{capabilityLabel}模型，请联系管理员配置并完成定价。
                    </div>
                )}
            </div>
        </section>
    );
}

function modelCapabilityLabel(capability: CanvasModelSelectionCapability) {
    if (capability === "image") return "图片";
    if (capability === "video") return "视频";
    return "音频";
}

function modelPresentation(config: AiConfig, model: string) {
    const entry = modelCatalogEntry(config, model);
    return {
        marketingCopy: entry?.marketingCopy?.trim() || "",
        promotionBadge: entry?.promotionBadge?.trim() || "",
    };
}
