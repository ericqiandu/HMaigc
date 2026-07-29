import { useMemo, useState } from "react";
import { Check, Plus } from "lucide-react";

import { ModelIcon } from "@/components/model-picker";
import { cn } from "@/lib/utils";
import {
    modelDisplayName,
    modelOptionName,
    resolveModelChannel,
    selectableModelsByCapability,
    type AiConfig,
    type ModelCapability,
} from "@/stores/use-config-store";
import type { CanvasAgentGenerationModels } from "@/types/canvas";

type AgentGenerationCapability = Extract<ModelCapability, "image" | "video">;

export function CanvasAgentModelMenu({
    config,
    value,
    onChange,
}: {
    config: AiConfig;
    value: CanvasAgentGenerationModels;
    onChange: (value: CanvasAgentGenerationModels) => void;
}) {
    const [capability, setCapability] = useState<AgentGenerationCapability>("image");
    const models = useMemo(() => selectableModelsByCapability(config, capability), [capability, config]);

    return (
        <section className="canvas-agent-picker canvas-agent-model-menu" aria-label="选择模型">
            <header className="canvas-agent-picker-header">
                <h3 className="canvas-agent-picker-title">选择模型</h3>
            </header>
            <div className="canvas-agent-picker-segments" role="radiogroup" aria-label="模型类型">
                {(["image", "video"] as const).map((item) => (
                    <button
                        key={item}
                        type="button"
                        role="radio"
                        aria-checked={capability === item}
                        className={cn("canvas-agent-picker-segment", capability === item && "canvas-agent-picker-segment--active")}
                        onClick={() => setCapability(item)}
                    >
                        {item === "image" ? "图片" : "视频"}
                    </button>
                ))}
            </div>
            <div className="canvas-agent-picker-section-label">{capability === "image" ? "图片" : "视频"}</div>
            <div className="canvas-agent-model-list thin-scrollbar">
                {models.length ? (
                    models.map((model) => {
                        const selected = value[capability] === model;
                        const presentation = modelPresentation(config, model);
                        return (
                            <button
                                key={model}
                                type="button"
                                className={cn("canvas-agent-model-row", selected && "canvas-agent-model-row--selected")}
                                aria-pressed={selected}
                                onClick={() => onChange({ ...value, [capability]: model })}
                            >
                                <span className="canvas-agent-model-icon">
                                    <ModelIcon config={config} model={model} />
                                </span>
                                <span className="canvas-agent-model-copy">
                                    <span className="canvas-agent-model-name-line">
                                        <span className="canvas-agent-model-name">{modelDisplayName(config, model)}</span>
                                        {presentation.promotionBadge ? <span className="canvas-agent-model-badge">{presentation.promotionBadge}</span> : null}
                                    </span>
                                    {presentation.marketingCopy ? <span className="canvas-agent-model-description">{presentation.marketingCopy}</span> : null}
                                </span>
                                <span className="canvas-agent-model-action" aria-hidden="true">
                                    {selected ? <Check className="canvas-agent-model-action-icon" /> : <Plus className="canvas-agent-model-action-icon" />}
                                </span>
                            </button>
                        );
                    })
                ) : (
                    <div className="canvas-agent-picker-empty" role="status">
                        当前没有可用的{capability === "image" ? "图片" : "视频"}模型，请联系管理员配置。
                    </div>
                )}
            </div>
        </section>
    );
}

function modelPresentation(config: AiConfig, model: string) {
    const channel = resolveModelChannel(config, model);
    const entry = channel.modelCosts?.find((item) => item.model === modelOptionName(model));
    return {
        marketingCopy: entry?.marketingCopy?.trim() || "",
        promotionBadge: entry?.promotionBadge?.trim() || "",
    };
}
