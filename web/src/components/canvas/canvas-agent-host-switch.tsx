import { Globe2, Laptop } from "lucide-react";
import { Tooltip } from "antd";

export type CanvasAgentHost = "managed" | "local_codex";

export function CanvasAgentHostSwitch({ value, onChange }: { value: CanvasAgentHost; onChange: (value: CanvasAgentHost) => void }) {
    return (
        <div className="canvas-agent-host-switch" role="group" aria-label="Agent 运行位置">
            <Tooltip title="由网站托管 Agent 执行">
                <button className="canvas-agent-host-option" type="button" data-active={value === "managed"} aria-label="使用网站 Agent" aria-pressed={value === "managed"} onClick={() => onChange("managed")}>
                    <Globe2 className="canvas-agent-host-option-icon" aria-hidden="true" />
                    <span className="canvas-agent-host-option-label">网站</span>
                </button>
            </Tooltip>
            <Tooltip title="由本机 Codex 推理，工具仍通过网站审批和执行">
                <button className="canvas-agent-host-option" type="button" data-active={value === "local_codex"} aria-label="使用本机 Codex" aria-pressed={value === "local_codex"} onClick={() => onChange("local_codex")}>
                    <Laptop className="canvas-agent-host-option-icon" aria-hidden="true" />
                    <span className="canvas-agent-host-option-label">本机</span>
                </button>
            </Tooltip>
        </div>
    );
}
