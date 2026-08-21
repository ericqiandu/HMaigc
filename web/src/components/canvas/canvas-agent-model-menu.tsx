import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasAgentGenerationModels } from "@/types/canvas";
import { CanvasModelSelectionMenu } from "./canvas-model-selection-menu";

export function CanvasAgentModelMenu({ config, value, onChange }: { config: AiConfig; value: CanvasAgentGenerationModels; onChange: (value: CanvasAgentGenerationModels) => void }) {
    return <CanvasModelSelectionMenu config={config} value={value} onChange={(nextValue) => onChange({ image: nextValue.image ?? value.image, video: nextValue.video ?? value.video })} />;
}
