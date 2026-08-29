import type {
    AgentAssemblyPlanPayload,
    AgentAssemblyPlanV2Payload,
    AgentAssetBindingPayload,
    AgentArtifactReviewContent,
    AgentArtifactRevision,
    AgentAudioPlanPayload,
    AgentMediaCandidatePayload,
    AgentStoryboardPlanPayload,
    AgentVideoPlanPayload,
} from "@/services/api/agent-production";
import { resourceFileUrl } from "@/services/api/resources";

export function AgentProductionCandidatePreview({
    resourceId,
    mediaKind,
    rank,
}: {
    resourceId: string;
    mediaKind: AgentMediaCandidatePayload["mediaKind"];
    rank: number;
}) {
    const source = resourceFileUrl(resourceId);
    if (mediaKind === "image") return <img className="agent-production-candidate-media" src={source} alt={`候选 ${rank}`} loading="lazy" />;
    if (mediaKind === "video") return <video className="agent-production-candidate-media" src={source} controls preload="metadata" aria-label={`候选 ${rank}`} />;
    return <audio className="agent-production-candidate-audio" src={source} controls preload="metadata" aria-label={`候选 ${rank}`} />;
}

export function AgentProductionArtifactDetails({ schema, revision }: { schema: AgentArtifactReviewContent["artifactSchema"]; revision: AgentArtifactRevision }) {
    if (schema === "script_bundle.v1") {
        const script = revision.payload as Extract<AgentArtifactRevision["payload"], { title: string }>;
        return (
            <div className="agent-production-artifact-body">
                <strong className="agent-production-artifact-name">{script.title}</strong>
                <p className="agent-production-artifact-copy">{script.logline}</p>
                <p className="agent-production-artifact-script">{script.script}</p>
                <FactList label="角色" values={script.characters.map((item) => `${item.name}：${item.description}`)} />
                <FactList label="场景" values={script.scenes.map((item) => `${item.name}：${item.description}`)} />
            </div>
        );
    }
    if (schema === "asset_binding.v1") {
        const payload = revision.payload as AgentAssetBindingPayload;
        return (
            <div className="agent-production-artifact-body">
                <FactRow label="绑定批次" value={payload.bindingKey} />
                <FactList label="素材绑定" values={payload.entries.map((entry) => `${entry.requirementKey} · ${entry.state}${entry.resourceId ? ` · ${entry.resourceId}` : ""}`)} />
            </div>
        );
    }
    if (schema === "storyboard_plan.v1") {
        const payload = revision.payload as AgentStoryboardPlanPayload;
        return (
            <div className="agent-production-artifact-body">
                <FactRow label="目标时长" value={`${payload.targetDurationMs} ms`} />
                <FactList label="分镜" values={payload.shots.map((shot) => `${shot.shotKey} · ${shot.shotSize} · ${shot.cameraMotion} · ${shot.durationMs} ms\n${shot.onScreenAction}`)} />
            </div>
        );
    }
    if (schema === "video_plan.v1") {
        const payload = revision.payload as AgentVideoPlanPayload;
        return <PlanDetails planKey={payload.planKey} mode={payload.audioMode} label="视频片段" entries={payload.segments.map((segment) => `${segment.segmentKey} → ${segment.outputArtifactKey}`)} />;
    }
    if (schema === "audio_plan.v1") {
        const payload = revision.payload as AgentAudioPlanPayload;
        return <PlanDetails planKey={payload.planKey} label="音频片段" entries={payload.clips.map((clip) => `${clip.clipKey} · ${clip.dialogue} · ${clip.startMs}–${clip.startMs + clip.durationMs} ms`)} />;
    }
    if (schema === "assembly_plan.v1") {
        const payload = revision.payload as AgentAssemblyPlanPayload;
        return <PlanDetails planKey={payload.planKey} mode={payload.audioMode} label="装配输出" entries={[payload.outputArtifactKey]} />;
    }
    if (schema === "assembly_plan.v2") {
        const payload = revision.payload as AgentAssemblyPlanV2Payload;
        return (
            <PlanDetails
                planKey={payload.planKey}
                mode={payload.audioMode}
                label="装配片段"
                entries={[
                    ...payload.clips.map((clip) => `${clip.clipKey} · ${clip.trimStartMs}–${clip.trimEndMs} ms · ${clip.transitionToNext.kind}`),
                    `${payload.output.width}×${payload.output.height} · ${payload.output.frameRate}fps · ${payload.output.container.toUpperCase()}`,
                ]}
            />
        );
    }
    const payload = revision.payload as Record<string, unknown>;
    return (
        <div className="agent-production-artifact-body">
            {Object.entries(payload).map(([key, value]) => <FactRow key={key} label={key} value={compactFact(value)} />)}
        </div>
    );
}

function PlanDetails({ planKey, mode, label, entries }: { planKey: string; mode?: string; label: string; entries: string[] }) {
    return (
        <div className="agent-production-artifact-body">
            <FactRow label="计划" value={planKey} />
            {mode ? <FactRow label="声音模式" value={mode} /> : null}
            <FactList label={label} values={entries} />
        </div>
    );
}

function FactRow({ label, value }: { label: string; value: string }) {
    return <div className="agent-production-fact-row"><span className="agent-production-fact-label">{label}</span><span className="agent-production-fact-value">{value}</span></div>;
}

function FactList({ label, values }: { label: string; values: string[] }) {
    if (!values.length) return null;
    return <div className="agent-production-fact-list"><span className="agent-production-fact-label">{label}</span>{values.map((value, index) => <p key={`${label}-${index}`} className="agent-production-fact-value">{value}</p>)}</div>;
}

function compactFact(value: unknown): string {
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
    if (Array.isArray(value)) return `${value.length} 项`;
    if (value && typeof value === "object") return "结构化事实";
    return "—";
}
