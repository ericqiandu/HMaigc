import { lazy, Suspense, useCallback, useState, type KeyboardEventHandler, type PointerEventHandler } from "react";
import { PanelRightClose } from "lucide-react";
import { Button, Tooltip } from "antd";
import { motion } from "motion/react";

import { canvasThemes } from "@/lib/canvas-theme";
import { CANVAS_AGENT_DOCK_MAX_WIDTH, CANVAS_AGENT_DOCK_MIN_WIDTH } from "@/lib/canvas/canvas-agent-dock";
import type { AgentRuntimeClient, AgentRuntimeEvent, AgentRuntimeHandleStorage } from "@/services/api/agent-runtime";
import type { PlatformSkill } from "@/services/api/skills";
import type { LocalAgentAuthoritativeToolResult } from "@/services/local-agent/local-agent-bridge";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasAgentLaunchRequest, CanvasNodeData } from "@/types/canvas";

import { CanvasAgentHostSwitch, type CanvasAgentHost } from "./canvas-agent-host-switch";
import { CanvasManagedAgentWorkspace } from "./canvas-managed-agent-workspace";
import "./canvas-agent-panel.css";

const LazyCanvasLocalAgentWorkspace = lazy(async () => {
    const module = await import("./canvas-local-agent-workspace");
    return { default: module.CanvasLocalAgentWorkspace };
});

export const CANVAS_AGENT_PANEL_MOTION_MS = 240;

type CanvasAssistantPanelProps = {
    projectId: string;
    canvasRevision: number;
    selectedNodeIds: Set<string>;
    getSelectedNodes: () => CanvasNodeData[];
    activatedSkills: PlatformSkill[];
    closing: boolean;
    width: number;
    onResizeStart: PointerEventHandler<HTMLDivElement>;
    onResizeKeyDown: KeyboardEventHandler<HTMLDivElement>;
    onCollapse: () => void;
    agentLaunchRequest?: CanvasAgentLaunchRequest;
    onAgentLaunchHandled?: (launchRequestId: string) => void;
    onBeforeRun?: () => Promise<void>;
    onRuntimeEvent?: (event: AgentRuntimeEvent) => void;
    onLocalToolResult?: (result: LocalAgentAuthoritativeToolResult) => void;
    runtimeClient?: AgentRuntimeClient;
    runtimeStorage?: AgentRuntimeHandleStorage;
};

export function CanvasAssistantPanel(props: CanvasAssistantPanelProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const [host, setHost] = useState<CanvasAgentHost>("managed");
    const [localActivated, setLocalActivated] = useState(false);
    const changeHost = useCallback((next: CanvasAgentHost) => {
        if (next === "local_codex") setLocalActivated(true);
        setHost(next);
    }, []);

    return (
        <motion.div
            className="canvas-agent-runtime-layout flex shrink-0"
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: props.closing ? 0 : props.width, opacity: props.closing ? 0 : 1 }}
            transition={{ duration: CANVAS_AGENT_PANEL_MOTION_MS / 1000, ease: [0.2, 0.8, 0.2, 1] }}
            style={{ overflow: "clip", pointerEvents: props.closing ? "none" : undefined, position: "relative" }}
        >
            <div
                className="canvas-agent-resize-handle"
                role="separator"
                aria-label="调整 Agent 面板宽度"
                aria-orientation="vertical"
                aria-valuemin={CANVAS_AGENT_DOCK_MIN_WIDTH}
                aria-valuemax={CANVAS_AGENT_DOCK_MAX_WIDTH}
                aria-valuenow={props.width}
                tabIndex={0}
                onPointerDown={props.onResizeStart}
                onKeyDown={props.onResizeKeyDown}
            />
            <motion.aside
                className="canvas-agent-shell relative flex min-w-0 flex-1 flex-col overflow-hidden border"
                initial={{ x: 48 }}
                animate={{ x: props.closing ? 28 : 0 }}
                transition={{ duration: CANVAS_AGENT_PANEL_MOTION_MS / 1000, ease: [0.2, 0.8, 0.2, 1] }}
                style={{ background: theme.node.panel, color: theme.node.text, borderColor: theme.node.stroke, boxShadow: `0 24px 72px ${theme.spatial.shadow}` }}
                aria-label="画布 Agent"
            >
                <header className="canvas-agent-runtime-header canvas-agent-header">
                    <strong className="canvas-agent-header-title">Agent 画布助手</strong>
                    <div className="canvas-agent-header-center">
                        <CanvasAgentHostSwitch value={host} onChange={changeHost} />
                    </div>
                    <div className="canvas-agent-runtime-header-actions">
                        <Tooltip title="收起">
                            <Button className="canvas-agent-runtime-icon-button" type="text" icon={<PanelRightClose className="canvas-agent-runtime-button-icon" />} onClick={props.onCollapse} aria-label="收起 Agent" />
                        </Tooltip>
                    </div>
                </header>
                <div className="canvas-agent-workspace-stack">
                    <div className="canvas-agent-workspace-slot" hidden={host !== "managed"}>
                        <CanvasManagedAgentWorkspace
                            projectId={props.projectId}
                            canvasRevision={props.canvasRevision}
                            selectedNodeIds={props.selectedNodeIds}
                            getSelectedNodes={props.getSelectedNodes}
                            activatedSkills={props.activatedSkills}
                            agentLaunchRequest={host === "managed" ? props.agentLaunchRequest : undefined}
                            onAgentLaunchHandled={props.onAgentLaunchHandled}
                            onBeforeRun={props.onBeforeRun}
                            onRuntimeEvent={props.onRuntimeEvent}
                            runtimeClient={props.runtimeClient}
                            runtimeStorage={props.runtimeStorage}
                        />
                    </div>
                    {localActivated ? (
                        <div className="canvas-agent-workspace-slot" hidden={host !== "local_codex"}>
                            <Suspense
                                fallback={
                                    <div className="canvas-agent-local-loading" role="status">
                                        正在加载本机 Codex…
                                    </div>
                                }
                            >
                                <LazyCanvasLocalAgentWorkspace
                                    projectId={props.projectId}
                                    canvasRevision={props.canvasRevision}
                                    selectedNodeIds={props.selectedNodeIds}
                                    getSelectedNodes={props.getSelectedNodes}
                                    activatedSkills={props.activatedSkills}
                                    onBeforeRun={props.onBeforeRun}
                                    onToolResult={props.onLocalToolResult}
                                    runtimeClient={props.runtimeClient}
                                />
                            </Suspense>
                        </div>
                    ) : null}
                </div>
            </motion.aside>
        </motion.div>
    );
}
