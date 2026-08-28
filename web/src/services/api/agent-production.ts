import { array, exactObject, flag, integer, object, text } from "./strict-contract";

export const agentProductionArtifactSchemas = [
    "script_bundle.v1",
    "asset_binding.v1",
    "visual_evidence.v1",
    "character_visual_bible.v1",
    "storyboard_plan.v1",
    "camera_tree.v1",
    "first_motion_last_frame.v1",
    "media_candidate.v1",
    "visual_consistency_review.v1",
    "media_candidate_selection.v1",
    "video_plan.v1",
    "audio_plan.v1",
    "assembly_plan.v1",
] as const;

export type AgentProductionArtifactSchema = (typeof agentProductionArtifactSchemas)[number];
export type AgentStageReviewDecision = "approved" | "revision_requested" | "stopped";
export type AgentProductionStageStatus = "planned" | "running" | "awaiting_review" | "approved" | "completed" | "failed" | "stopped" | "stale";
export type AgentAssetCategory = "character" | "environment" | "wardrobe" | "prop" | "weapon" | "style" | "other";
export type AgentPublicationIntent = { publicationPurpose: string; targetCategory: AgentAssetCategory; targetBindingKey: string };
export type AgentArtifactRevisionRef = { artifactId: string; revisionId: string };
export type AgentScriptBundle = {
    title: string;
    logline: string;
    script: string;
    characters: Array<{ key: string; name: string; description: string }>;
    scenes: Array<{ key: string; name: string; description: string }>;
    props: Array<{ key: string; name: string; description: string }>;
    voiceRoles: Array<{ key: string; name: string; description: string }>;
};
export type AgentAssetBindingPayload = {
    bindingKey: string;
    scriptRevision: AgentArtifactRevisionRef;
    confirmed: boolean;
    entries: Array<{
        requirementKey: string;
        requirementKind: "character" | "scene" | "prop" | "voice_role";
        state: "matched" | "missing" | "conflict" | "choice_required";
        resourceId?: string;
        candidateResourceIds: string[];
    }>;
};
export type AgentFrameState = {
    state: string;
    static: boolean;
    evidenceRevisions?: AgentArtifactRevisionRef[];
    visibleCharacterKeys?: string[];
};
export type AgentFirstMotionLastFramePayload = {
    firstFrame: AgentFrameState;
    motion: string;
    lastFrame: AgentFrameState;
    inputRevisions?: AgentArtifactRevisionRef[];
    continuityConditions?: string[];
};
export type AgentStoryboardPlanPayload = {
    scriptRevision: AgentArtifactRevisionRef;
    assetBindingRevision: AgentArtifactRevisionRef;
    characterBibleRevision: AgentArtifactRevisionRef;
    visualEvidenceRevisions: AgentArtifactRevisionRef[];
    targetDurationMs: number;
    shots: Array<{
        shotKey: string;
        narrativePurpose: string;
        shotSize: string;
        cameraPosition: string;
        angle: string;
        composition: string;
        screenDirection: string;
        cameraMotion: string;
        onScreenAction: string;
        dialogue: Array<{ characterKey: string; text: string }>;
        sound: Array<{ cueKey: string; description: string }>;
        durationMs: number;
        transition: string;
        visibleCharacterKeys: string[];
        inputRevisions: AgentArtifactRevisionRef[];
        framePlan: AgentFirstMotionLastFramePayload;
    }>;
};
export type AgentVideoPlanPayload = {
    planKey: string;
    inputRevisions: AgentArtifactRevisionRef[];
    audioMode: "none" | "native" | "independent";
    segments: Array<{ segmentKey: string; inputRevisions: AgentArtifactRevisionRef[]; outputArtifactKey: string; generateAudio: boolean }>;
};
export type AgentAudioPlanPayload = {
    planKey: string;
    inputRevisions: AgentArtifactRevisionRef[];
    clips: Array<{ clipKey: string; voiceBindingKey: string; lineKey: string; dialogue: string; startMs: number; durationMs: number; outputArtifactKey: string }>;
};
export type AgentAssemblyPlanPayload = {
    planKey: string;
    audioMode: "none" | "native" | "independent";
    videoRevisions: AgentArtifactRevisionRef[];
    audioRevisions: AgentArtifactRevisionRef[];
    outputArtifactKey: string;
};
export type AgentMediaCandidatePayload = {
    candidateKey: string;
    mediaKind: "image" | "video" | "audio";
    providerRequestIdentity: string;
    resourceId: string;
    sourceTaskId: string;
};
export type AgentVisualConsistencyFinding = {
    dimension: "identity" | "clothing" | "scene" | "time_space" | "composition" | "screen_direction" | "frame_continuity";
    outcome: "matched" | "deviation" | "uncertain";
    description: string;
    evidenceRevisions: AgentArtifactRevisionRef[];
    confidenceBasisPoints: number;
};
export type AgentVisualConsistencyReviewPayload = {
    reviewRunId: string;
    reviewModelRecordId: string;
    reviewRequestIdentity: string;
    candidateRevisions: AgentArtifactRevisionRef[];
    confirmedReferenceRevisions: AgentArtifactRevisionRef[];
    assessments: Array<{ candidateRevision: AgentArtifactRevisionRef; visualEvidenceRevision: AgentArtifactRevisionRef; findings: AgentVisualConsistencyFinding[] }>;
    rankedCandidateRevisionIds: string[];
    uncertainties: string[];
    conflicts: string[];
    retrySuggestions: string[];
};
export type AgentArtifactPayload =
    | AgentScriptBundle
    | AgentAssetBindingPayload
    | AgentFirstMotionLastFramePayload
    | AgentStoryboardPlanPayload
    | AgentMediaCandidatePayload
    | AgentVisualConsistencyReviewPayload
    | AgentVideoPlanPayload
    | AgentAudioPlanPayload
    | AgentAssemblyPlanPayload
    | Record<string, unknown>;
export type AgentArtifactRevision = {
    artifactId: string;
    revisionId: string;
    artifactKey: string;
    revision: number;
    kind: string;
    schemaVersion: number;
    payload: AgentArtifactPayload;
    resourceId?: string;
    upstreamRevisions: AgentArtifactRevisionRef[];
    skillVersions: Array<{ dir: string; name: string; description: string; instructions: string; version: number; checksum: string }>;
    lifecycleStatus: string;
    createdAt: string;
};

export type AgentArtifactReviewContent = {
    contentType: "artifact_review";
    stageId: string;
    stageVersion: number;
    artifactId: string;
    revisionId: string;
    artifactSchema: AgentProductionArtifactSchema;
    summary: string;
};
export type AgentStageReviewResolutionContent = {
    contentType: "stage_review_resolution";
    stageId: string;
    stageVersion: number;
    revisionId: string;
    decision: AgentStageReviewDecision;
    clientRequestId: string;
    publicationIntent?: AgentPublicationIntent;
    resultStageVersion: number;
    resultStatus: AgentProductionStageStatus;
    resultReviewRevisionId?: string;
    resultUpdatedAt: string;
};
export type AgentAssetPublicationContent = {
    contentType: "asset_publication";
    publicationId: string;
    artifactRevisionId: string;
    resourceId: string;
    assetId: string;
    assetVersionId: string;
    projectAssetLinkId: string;
    representationId: string;
    publicationPurpose: string;
    targetCategory: AgentAssetCategory;
    targetBindingKey: string;
};
export type AgentAssetPublicationFailureContent = {
    contentType: "asset_publication_failed";
    publicationId: string;
    artifactRevisionId: string;
    errorCode: string;
};
export type AgentProductionTimelineContent =
    | AgentArtifactReviewContent
    | AgentStageReviewResolutionContent
    | AgentAssetPublicationContent
    | AgentAssetPublicationFailureContent;

export type AgentStageReviewInput = {
    stageVersion: number;
    revisionId: string;
    decision: AgentStageReviewDecision;
    selectedCandidateRevisionId?: string;
    clientRequestId: string;
    comment: string;
    publicationIntent?: AgentPublicationIntent;
};
export type AgentAssetPublicationResult = {
    id: string;
    artifactRevisionId: string;
    assetId: string;
    assetVersionId: string;
    projectAssetLinkId: string;
    representationId: string;
    status: "succeeded";
    replayed: boolean;
};
export type AgentStageReviewResult = {
    stage: {
        id: string;
        stageKey: string;
        specialistKey: "narrative" | "asset" | "storyboard" | "visual" | "video_assembly" | "audio";
        reviewPolicy: "required";
        costPolicy: "none" | "approval_required";
        status: AgentProductionStageStatus;
        version: number;
        reviewRevisionId?: string;
        lastErrorCode?: string;
        updatedAt: string;
    };
    artifactRevisionIds: string[];
    selectedCandidateRevisionId?: string;
    publication?: AgentAssetPublicationResult;
};
export type AgentProductionClient = {
    getArtifactRevision: (runId: string, artifactId: string, revisionId: string) => Promise<AgentArtifactRevision>;
    reviewStage: (runId: string, stageId: string, input: AgentStageReviewInput) => Promise<AgentStageReviewResult>;
};

const artifactSchemaSet = new Set<string>(agentProductionArtifactSchemas);
const categorySet = new Set<AgentAssetCategory>(["character", "environment", "wardrobe", "prop", "weapon", "style", "other"]);
const stageStatusSet = new Set<AgentProductionStageStatus>(["planned", "running", "awaiting_review", "approved", "completed", "failed", "stopped", "stale"]);
const specialistSet = new Set(["narrative", "asset", "storyboard", "visual", "video_assembly", "audio"]);

export function parseAgentProductionTimelineContent(value: unknown): AgentProductionTimelineContent {
    const source = object(value, "Agent 产物时间线");
    rejectTransientMediaLocator(source, "Agent 产物时间线");
    switch (source.contentType) {
        case "artifact_review": {
            const content = exactObject(source, "Agent 产物审核", ["contentType", "stageId", "stageVersion", "artifactId", "revisionId", "artifactSchema", "summary"]);
            return {
                contentType: "artifact_review",
                stageId: text(content.stageId, "artifactReview.stageId"),
                stageVersion: integer(content.stageVersion, "artifactReview.stageVersion"),
                artifactId: text(content.artifactId, "artifactReview.artifactId"),
                revisionId: text(content.revisionId, "artifactReview.revisionId"),
                artifactSchema: artifactSchema(content.artifactSchema),
                summary: text(content.summary, "artifactReview.summary"),
            };
        }
        case "stage_review_resolution": {
            const content = exactObject(source, "Agent 阶段审核决议", ["contentType", "stageId", "stageVersion", "revisionId", "decision", "clientRequestId", "publicationIntent", "resultStageVersion", "resultStatus", "resultReviewRevisionId", "resultUpdatedAt"]);
            const decision = reviewDecision(content.decision);
            const result: AgentStageReviewResolutionContent = {
                contentType: "stage_review_resolution",
                stageId: text(content.stageId, "stageResolution.stageId"),
                stageVersion: integer(content.stageVersion, "stageResolution.stageVersion"),
                revisionId: text(content.revisionId, "stageResolution.revisionId"),
                decision,
                clientRequestId: text(content.clientRequestId, "stageResolution.clientRequestId"),
                resultStageVersion: integer(content.resultStageVersion, "stageResolution.resultStageVersion"),
                resultStatus: stageStatus(content.resultStatus),
                resultUpdatedAt: isoInstant(content.resultUpdatedAt, "stageResolution.resultUpdatedAt"),
            };
            if (result.resultStageVersion !== result.stageVersion + 1) throw new Error("Agent 阶段审核版本事实冲突");
            if (content.publicationIntent !== undefined) result.publicationIntent = parsePublicationIntent(content.publicationIntent);
            if (content.resultReviewRevisionId !== undefined) result.resultReviewRevisionId = text(content.resultReviewRevisionId, "stageResolution.resultReviewRevisionId");
            validateResolution(result);
            return result;
        }
        case "asset_publication": {
            const content = exactObject(source, "Agent 资产入库", ["contentType", "publicationId", "artifactRevisionId", "resourceId", "assetId", "assetVersionId", "projectAssetLinkId", "representationId", "publicationPurpose", "targetCategory", "targetBindingKey"]);
            return {
                contentType: "asset_publication",
                publicationId: text(content.publicationId, "publication.publicationId"),
                artifactRevisionId: text(content.artifactRevisionId, "publication.artifactRevisionId"),
                resourceId: text(content.resourceId, "publication.resourceId"),
                assetId: text(content.assetId, "publication.assetId"),
                assetVersionId: text(content.assetVersionId, "publication.assetVersionId"),
                projectAssetLinkId: text(content.projectAssetLinkId, "publication.projectAssetLinkId"),
                representationId: text(content.representationId, "publication.representationId"),
                publicationPurpose: text(content.publicationPurpose, "publication.publicationPurpose"),
                targetCategory: assetCategory(content.targetCategory),
                targetBindingKey: text(content.targetBindingKey, "publication.targetBindingKey"),
            };
        }
        case "asset_publication_failed": {
            const content = exactObject(source, "Agent 资产入库失败", ["contentType", "publicationId", "artifactRevisionId", "errorCode"]);
            return {
                contentType: "asset_publication_failed",
                publicationId: text(content.publicationId, "publicationFailure.publicationId"),
                artifactRevisionId: text(content.artifactRevisionId, "publicationFailure.artifactRevisionId"),
                errorCode: text(content.errorCode, "publicationFailure.errorCode"),
            };
        }
        default:
            throw new Error(`不受支持的 Agent 产物时间线类型: ${String(source.contentType)}`);
    }
}

export function parseAgentArtifactRevision(value: unknown): AgentArtifactRevision {
    rejectTransientMediaLocator(value, "Agent 产物版本");
    const source = exactObject(value, "Agent 产物版本", ["artifactId", "revisionId", "artifactKey", "revision", "kind", "schemaVersion", "payload", "resourceId", "upstreamRevisions", "skillVersions", "lifecycleStatus", "createdAt"]);
    const kind = text(source.kind, "artifact.kind");
    const schemaVersion = integer(source.schemaVersion, "artifact.schemaVersion");
    const schema = artifactSchema(`${kind}.v${schemaVersion}`);
    const result: AgentArtifactRevision = {
        artifactId: text(source.artifactId, "artifact.artifactId"),
        revisionId: text(source.revisionId, "artifact.revisionId"),
        artifactKey: text(source.artifactKey, "artifact.artifactKey"),
        revision: integer(source.revision, "artifact.revision"),
        kind,
        schemaVersion,
        payload: parseArtifactPayload(schema, source.payload),
        upstreamRevisions: array(source.upstreamRevisions, "artifact.upstreamRevisions").map((item, index) => parseRevisionRef(item, `artifact.upstreamRevisions[${index}]`)),
        skillVersions: array(source.skillVersions, "artifact.skillVersions").map((item, index) => parseSkillVersion(item, index)),
        lifecycleStatus: text(source.lifecycleStatus, "artifact.lifecycleStatus"),
        createdAt: isoInstant(source.createdAt, "artifact.createdAt"),
    };
    if (source.resourceId !== undefined) result.resourceId = text(source.resourceId, "artifact.resourceId");
    rejectTransientMediaLocator(result.payload, "artifact.payload");
    return result;
}

function parseArtifactPayload(schema: AgentProductionArtifactSchema, value: unknown): AgentArtifactPayload {
    switch (schema) {
        case "script_bundle.v1": return parseScriptBundle(value);
        case "asset_binding.v1": return parseAssetBinding(value);
        case "visual_evidence.v1": return parseVisualEvidence(value);
        case "character_visual_bible.v1": return parseCharacterVisualBible(value);
        case "storyboard_plan.v1": return parseStoryboardPlan(value);
        case "camera_tree.v1": return parseCameraTree(value);
        case "first_motion_last_frame.v1": return parseFirstMotionLastFrame(value, "framePlan");
        case "media_candidate.v1": return parseMediaCandidate(value);
        case "visual_consistency_review.v1": return parseVisualConsistencyReview(value);
        case "media_candidate_selection.v1": return parseMediaCandidateSelection(value);
        case "video_plan.v1": return parseVideoPlan(value);
        case "audio_plan.v1": return parseAudioPlan(value);
        case "assembly_plan.v1": return parseAssemblyPlan(value);
    }
}

function parseAssetBinding(value: unknown): AgentAssetBindingPayload {
    const source = exactObject(value, "Agent asset_binding.v1 产物", ["bindingKey", "scriptRevision", "confirmed", "entries"]);
    return {
        bindingKey: text(source.bindingKey, "assetBinding.bindingKey"),
        scriptRevision: parseRevisionRef(source.scriptRevision, "assetBinding.scriptRevision"),
        confirmed: flag(source.confirmed, "assetBinding.confirmed"),
        entries: array(source.entries, "assetBinding.entries").map((item, index) => {
            const label = `assetBinding.entries[${index}]`;
            const entry = exactObject(item, label, ["requirementKey", "requirementKind", "state", "resourceId", "candidateResourceIds"]);
            const parsed: AgentAssetBindingPayload["entries"][number] = {
                requirementKey: text(entry.requirementKey, `${label}.requirementKey`),
                requirementKind: oneOf(entry.requirementKind, `${label}.requirementKind`, ["character", "scene", "prop", "voice_role"] as const),
                state: oneOf(entry.state, `${label}.state`, ["matched", "missing", "conflict", "choice_required"] as const),
                candidateResourceIds: stringArray(entry.candidateResourceIds, `${label}.candidateResourceIds`),
            };
            if (entry.resourceId !== undefined) parsed.resourceId = text(entry.resourceId, `${label}.resourceId`);
            return parsed;
        }),
    };
}

function parseVisualEvidence(value: unknown): Record<string, unknown> {
    const source = exactObject(value, "Agent visual_evidence.v1 产物", ["sourceRevision", "characters", "identityEvidence", "scene", "props", "spatialRelations", "shot", "actionState", "ocrText", "uncertainties", "conflicts", "confidenceBasisPoints", "visionModelRecordId", "requestIdentity"]);
    const scene = exactObject(source.scene, "visualEvidence.scene", ["key", "description"]);
    const shot = exactObject(source.shot, "visualEvidence.shot", ["shotSize", "angle", "composition", "screenDirection", "gaze", "firstFrameCondition", "lastFrameCondition"]);
    const confidenceBasisPoints = integer(source.confidenceBasisPoints, "visualEvidence.confidenceBasisPoints", true);
    if (confidenceBasisPoints > 10_000) throw new Error("visualEvidence.confidenceBasisPoints 超出范围");
    return {
        sourceRevision: parseRevisionRef(source.sourceRevision, "visualEvidence.sourceRevision"),
        characters: array(source.characters, "visualEvidence.characters").map((item, index) => {
            const label = `visualEvidence.characters[${index}]`;
            const character = exactObject(item, label, ["key", "name", "clothing", "hair", "stableFeatures"]);
            return { key: text(character.key, `${label}.key`), name: text(character.name, `${label}.name`), clothing: text(character.clothing, `${label}.clothing`), hair: text(character.hair, `${label}.hair`), stableFeatures: stringArray(character.stableFeatures, `${label}.stableFeatures`) };
        }),
        identityEvidence: array(source.identityEvidence, "visualEvidence.identityEvidence").map((item, index) => {
            const label = `visualEvidence.identityEvidence[${index}]`;
            const identity = exactObject(item, label, ["characterKey", "observations"]);
            return { characterKey: text(identity.characterKey, `${label}.characterKey`), observations: stringArray(identity.observations, `${label}.observations`) };
        }),
        scene: { key: text(scene.key, "visualEvidence.scene.key"), description: text(scene.description, "visualEvidence.scene.description") },
        props: array(source.props, "visualEvidence.props").map((item, index) => parseKeyNameDescription(item, `visualEvidence.props[${index}]`)),
        spatialRelations: array(source.spatialRelations, "visualEvidence.spatialRelations").map((item, index) => {
            const label = `visualEvidence.spatialRelations[${index}]`;
            const relation = exactObject(item, label, ["subjectKey", "relation", "objectKey"]);
            return { subjectKey: text(relation.subjectKey, `${label}.subjectKey`), relation: text(relation.relation, `${label}.relation`), objectKey: text(relation.objectKey, `${label}.objectKey`) };
        }),
        shot: {
            shotSize: text(shot.shotSize, "visualEvidence.shot.shotSize"),
            angle: text(shot.angle, "visualEvidence.shot.angle"),
            composition: text(shot.composition, "visualEvidence.shot.composition"),
            screenDirection: text(shot.screenDirection, "visualEvidence.shot.screenDirection"),
            gaze: text(shot.gaze, "visualEvidence.shot.gaze"),
            firstFrameCondition: text(shot.firstFrameCondition, "visualEvidence.shot.firstFrameCondition"),
            lastFrameCondition: text(shot.lastFrameCondition, "visualEvidence.shot.lastFrameCondition"),
        },
        actionState: text(source.actionState, "visualEvidence.actionState"),
        ocrText: stringArray(source.ocrText, "visualEvidence.ocrText"),
        uncertainties: parseVisualIssues(source.uncertainties, "visualEvidence.uncertainties"),
        conflicts: parseVisualIssues(source.conflicts, "visualEvidence.conflicts"),
        confidenceBasisPoints,
        visionModelRecordId: text(source.visionModelRecordId, "visualEvidence.visionModelRecordId"),
        requestIdentity: text(source.requestIdentity, "visualEvidence.requestIdentity"),
    };
}

function parseCharacterVisualBible(value: unknown): Record<string, unknown> {
    const source = exactObject(value, "Agent character_visual_bible.v1 产物", ["scriptRevision", "assetBindingRevision", "visualEvidenceRevisions", "referenceAssetRevisions", "characters"]);
    return {
        scriptRevision: parseRevisionRef(source.scriptRevision, "characterBible.scriptRevision"),
        assetBindingRevision: parseRevisionRef(source.assetBindingRevision, "characterBible.assetBindingRevision"),
        visualEvidenceRevisions: revisionArray(source.visualEvidenceRevisions, "characterBible.visualEvidenceRevisions"),
        referenceAssetRevisions: revisionArray(source.referenceAssetRevisions, "characterBible.referenceAssetRevisions"),
        characters: array(source.characters, "characterBible.characters").map((item, index) => parseCharacterVisualProfile(item, index)),
    };
}

function parseCharacterVisualProfile(value: unknown, index: number): Record<string, unknown> {
    const label = `characterBible.characters[${index}]`;
    const source = exactObject(value, label, ["characterKey", "canonicalName", "aliases", "staticFeatures", "dynamicFeatures", "referenceRevisions", "voiceRoleKey", "voiceAssetRevision", "unknowns", "conflicts"]);
    const result: Record<string, unknown> = {
        characterKey: text(source.characterKey, `${label}.characterKey`),
        canonicalName: text(source.canonicalName, `${label}.canonicalName`),
        aliases: stringArray(source.aliases, `${label}.aliases`),
        staticFeatures: array(source.staticFeatures, `${label}.staticFeatures`).map((item, factIndex) => {
            const factLabel = `${label}.staticFeatures[${factIndex}]`;
            const fact = exactObject(item, factLabel, ["factKey", "description", "evidenceRefs"]);
            return { factKey: text(fact.factKey, `${factLabel}.factKey`), description: text(fact.description, `${factLabel}.description`), evidenceRefs: revisionArray(fact.evidenceRefs, `${factLabel}.evidenceRefs`) };
        }),
        dynamicFeatures: array(source.dynamicFeatures, `${label}.dynamicFeatures`).map((item, featureIndex) => {
            const featureLabel = `${label}.dynamicFeatures[${featureIndex}]`;
            const feature = exactObject(item, featureLabel, ["featureKey", "scopeKey", "description", "evidenceRefs"]);
            return { featureKey: text(feature.featureKey, `${featureLabel}.featureKey`), scopeKey: text(feature.scopeKey, `${featureLabel}.scopeKey`), description: text(feature.description, `${featureLabel}.description`), evidenceRefs: revisionArray(feature.evidenceRefs, `${featureLabel}.evidenceRefs`) };
        }),
        referenceRevisions: revisionArray(source.referenceRevisions, `${label}.referenceRevisions`),
        unknowns: parseVisualIssues(source.unknowns, `${label}.unknowns`),
        conflicts: parseVisualIssues(source.conflicts, `${label}.conflicts`),
    };
    if (source.voiceRoleKey !== undefined) result.voiceRoleKey = text(source.voiceRoleKey, `${label}.voiceRoleKey`);
    if (source.voiceAssetRevision !== undefined) result.voiceAssetRevision = parseRevisionRef(source.voiceAssetRevision, `${label}.voiceAssetRevision`);
    return result;
}

function parseStoryboardPlan(value: unknown): AgentStoryboardPlanPayload {
    const source = exactObject(value, "Agent storyboard_plan.v1 产物", ["scriptRevision", "assetBindingRevision", "characterBibleRevision", "visualEvidenceRevisions", "targetDurationMs", "shots"]);
    return {
        scriptRevision: parseRevisionRef(source.scriptRevision, "storyboard.scriptRevision"),
        assetBindingRevision: parseRevisionRef(source.assetBindingRevision, "storyboard.assetBindingRevision"),
        characterBibleRevision: parseRevisionRef(source.characterBibleRevision, "storyboard.characterBibleRevision"),
        visualEvidenceRevisions: revisionArray(source.visualEvidenceRevisions, "storyboard.visualEvidenceRevisions"),
        targetDurationMs: integer(source.targetDurationMs, "storyboard.targetDurationMs"),
        shots: array(source.shots, "storyboard.shots").map((item, index) => parseCinematicShot(item, index)),
    };
}

function parseCinematicShot(value: unknown, index: number): AgentStoryboardPlanPayload["shots"][number] {
    const label = `storyboard.shots[${index}]`;
    const source = exactObject(value, label, ["shotKey", "narrativePurpose", "shotSize", "cameraPosition", "angle", "composition", "screenDirection", "cameraMotion", "onScreenAction", "dialogue", "sound", "durationMs", "transition", "visibleCharacterKeys", "inputRevisions", "framePlan"]);
    return {
        shotKey: text(source.shotKey, `${label}.shotKey`), narrativePurpose: text(source.narrativePurpose, `${label}.narrativePurpose`),
        shotSize: text(source.shotSize, `${label}.shotSize`), cameraPosition: text(source.cameraPosition, `${label}.cameraPosition`), angle: text(source.angle, `${label}.angle`),
        composition: text(source.composition, `${label}.composition`), screenDirection: text(source.screenDirection, `${label}.screenDirection`), cameraMotion: text(source.cameraMotion, `${label}.cameraMotion`),
        onScreenAction: text(source.onScreenAction, `${label}.onScreenAction`),
        dialogue: array(source.dialogue, `${label}.dialogue`).map((item, lineIndex) => {
            const lineLabel = `${label}.dialogue[${lineIndex}]`;
            const line = exactObject(item, lineLabel, ["characterKey", "text"]);
            return { characterKey: text(line.characterKey, `${lineLabel}.characterKey`), text: text(line.text, `${lineLabel}.text`) };
        }),
        sound: array(source.sound, `${label}.sound`).map((item, cueIndex) => {
            const cueLabel = `${label}.sound[${cueIndex}]`;
            const cue = exactObject(item, cueLabel, ["cueKey", "description"]);
            return { cueKey: text(cue.cueKey, `${cueLabel}.cueKey`), description: text(cue.description, `${cueLabel}.description`) };
        }),
        durationMs: integer(source.durationMs, `${label}.durationMs`), transition: text(source.transition, `${label}.transition`),
        visibleCharacterKeys: stringArray(source.visibleCharacterKeys, `${label}.visibleCharacterKeys`), inputRevisions: revisionArray(source.inputRevisions, `${label}.inputRevisions`),
        framePlan: parseFirstMotionLastFrame(source.framePlan, `${label}.framePlan`),
    };
}

function parseFirstMotionLastFrame(value: unknown, label: string): AgentFirstMotionLastFramePayload {
    const source = exactObject(value, label, ["firstFrame", "motion", "lastFrame", "inputRevisions", "continuityConditions"]);
    const result: AgentFirstMotionLastFramePayload = { firstFrame: parseFrameState(source.firstFrame, `${label}.firstFrame`), motion: text(source.motion, `${label}.motion`), lastFrame: parseFrameState(source.lastFrame, `${label}.lastFrame`) };
    if (source.inputRevisions !== undefined) result.inputRevisions = revisionArray(source.inputRevisions, `${label}.inputRevisions`);
    if (source.continuityConditions !== undefined) result.continuityConditions = stringArray(source.continuityConditions, `${label}.continuityConditions`);
    return result;
}

function parseFrameState(value: unknown, label: string): AgentFrameState {
    const source = exactObject(value, label, ["state", "static", "evidenceRevisions", "visibleCharacterKeys"]);
    const result: AgentFrameState = { state: text(source.state, `${label}.state`), static: flag(source.static, `${label}.static`) };
    if (source.evidenceRevisions !== undefined) result.evidenceRevisions = revisionArray(source.evidenceRevisions, `${label}.evidenceRevisions`);
    if (source.visibleCharacterKeys !== undefined) result.visibleCharacterKeys = stringArray(source.visibleCharacterKeys, `${label}.visibleCharacterKeys`);
    return result;
}

function parseCameraTree(value: unknown): Record<string, unknown> {
    const source = exactObject(value, "Agent camera_tree.v1 产物", ["storyboardRevision", "visualEvidenceRevisions", "shotKeys", "cameras", "relations", "missingViews"]);
    return {
        storyboardRevision: parseRevisionRef(source.storyboardRevision, "cameraTree.storyboardRevision"),
        visualEvidenceRevisions: revisionArray(source.visualEvidenceRevisions, "cameraTree.visualEvidenceRevisions"),
        shotKeys: stringArray(source.shotKeys, "cameraTree.shotKeys"),
        cameras: array(source.cameras, "cameraTree.cameras").map((item, index) => {
            const label = `cameraTree.cameras[${index}]`;
            const camera = exactObject(item, label, ["cameraKey", "parentCameraKey", "independent", "shotKeys", "subjectKeys", "shotSize", "angle", "screenDirection", "purpose"]);
            const result: Record<string, unknown> = { cameraKey: text(camera.cameraKey, `${label}.cameraKey`), independent: flag(camera.independent, `${label}.independent`), shotKeys: stringArray(camera.shotKeys, `${label}.shotKeys`), subjectKeys: stringArray(camera.subjectKeys, `${label}.subjectKeys`), shotSize: text(camera.shotSize, `${label}.shotSize`), angle: text(camera.angle, `${label}.angle`), screenDirection: text(camera.screenDirection, `${label}.screenDirection`), purpose: text(camera.purpose, `${label}.purpose`) };
            if (camera.parentCameraKey !== undefined) result.parentCameraKey = text(camera.parentCameraKey, `${label}.parentCameraKey`);
            return result;
        }),
        relations: array(source.relations, "cameraTree.relations").map((item, index) => {
            const label = `cameraTree.relations[${index}]`;
            const relation = exactObject(item, label, ["relationKey", "fromCameraKey", "toCameraKey", "relation", "evidenceRefs"]);
            return { relationKey: text(relation.relationKey, `${label}.relationKey`), fromCameraKey: text(relation.fromCameraKey, `${label}.fromCameraKey`), toCameraKey: text(relation.toCameraKey, `${label}.toCameraKey`), relation: text(relation.relation, `${label}.relation`), evidenceRefs: revisionArray(relation.evidenceRefs, `${label}.evidenceRefs`) };
        }),
        missingViews: array(source.missingViews, "cameraTree.missingViews").map((item, index) => {
            const label = `cameraTree.missingViews[${index}]`;
            const gap = exactObject(item, label, ["gapKey", "description", "relatedCameraKeys"]);
            return { gapKey: text(gap.gapKey, `${label}.gapKey`), description: text(gap.description, `${label}.description`), relatedCameraKeys: stringArray(gap.relatedCameraKeys, `${label}.relatedCameraKeys`) };
        }),
    };
}

function parseMediaCandidateSelection(value: unknown): Record<string, unknown> {
    const source = exactObject(value, "Agent media_candidate_selection.v1 产物", ["stageId", "reviewRevision", "selectedCandidateRevision", "approvedByUserId", "clientRequestId"]);
    return { stageId: text(source.stageId, "candidateSelection.stageId"), reviewRevision: parseRevisionRef(source.reviewRevision, "candidateSelection.reviewRevision"), selectedCandidateRevision: parseRevisionRef(source.selectedCandidateRevision, "candidateSelection.selectedCandidateRevision"), approvedByUserId: text(source.approvedByUserId, "candidateSelection.approvedByUserId"), clientRequestId: text(source.clientRequestId, "candidateSelection.clientRequestId") };
}

function parseVideoPlan(value: unknown): AgentVideoPlanPayload {
    const source = exactObject(value, "Agent video_plan.v1 产物", ["planKey", "inputRevisions", "audioMode", "segments"]);
    return { planKey: text(source.planKey, "videoPlan.planKey"), inputRevisions: revisionArray(source.inputRevisions, "videoPlan.inputRevisions"), audioMode: audioMode(source.audioMode, "videoPlan.audioMode"), segments: array(source.segments, "videoPlan.segments").map((item, index) => {
        const label = `videoPlan.segments[${index}]`;
        const segment = exactObject(item, label, ["segmentKey", "inputRevisions", "outputArtifactKey", "generateAudio"]);
        return { segmentKey: text(segment.segmentKey, `${label}.segmentKey`), inputRevisions: revisionArray(segment.inputRevisions, `${label}.inputRevisions`), outputArtifactKey: text(segment.outputArtifactKey, `${label}.outputArtifactKey`), generateAudio: flag(segment.generateAudio, `${label}.generateAudio`) };
    }) };
}

function parseAudioPlan(value: unknown): AgentAudioPlanPayload {
    const source = exactObject(value, "Agent audio_plan.v1 产物", ["planKey", "inputRevisions", "clips"]);
    return { planKey: text(source.planKey, "audioPlan.planKey"), inputRevisions: revisionArray(source.inputRevisions, "audioPlan.inputRevisions"), clips: array(source.clips, "audioPlan.clips").map((item, index) => {
        const label = `audioPlan.clips[${index}]`;
        const clip = exactObject(item, label, ["clipKey", "voiceBindingKey", "lineKey", "dialogue", "startMs", "durationMs", "outputArtifactKey"]);
        return { clipKey: text(clip.clipKey, `${label}.clipKey`), voiceBindingKey: text(clip.voiceBindingKey, `${label}.voiceBindingKey`), lineKey: text(clip.lineKey, `${label}.lineKey`), dialogue: text(clip.dialogue, `${label}.dialogue`), startMs: integer(clip.startMs, `${label}.startMs`, true), durationMs: integer(clip.durationMs, `${label}.durationMs`), outputArtifactKey: text(clip.outputArtifactKey, `${label}.outputArtifactKey`) };
    }) };
}

function parseAssemblyPlan(value: unknown): AgentAssemblyPlanPayload {
    const source = exactObject(value, "Agent assembly_plan.v1 产物", ["planKey", "audioMode", "videoRevisions", "audioRevisions", "outputArtifactKey"]);
    return { planKey: text(source.planKey, "assemblyPlan.planKey"), audioMode: audioMode(source.audioMode, "assemblyPlan.audioMode"), videoRevisions: revisionArray(source.videoRevisions, "assemblyPlan.videoRevisions"), audioRevisions: revisionArray(source.audioRevisions, "assemblyPlan.audioRevisions"), outputArtifactKey: text(source.outputArtifactKey, "assemblyPlan.outputArtifactKey") };
}

function parseMediaCandidate(value: unknown): AgentMediaCandidatePayload {
    const source = exactObject(value, "Agent media_candidate.v1 产物", ["candidateKey", "mediaKind", "providerRequestIdentity", "resourceId", "sourceTaskId"]);
    const mediaKind = text(source.mediaKind, "mediaCandidate.mediaKind");
    if (mediaKind !== "image" && mediaKind !== "video" && mediaKind !== "audio") throw new Error(`不受支持的候选媒体类型: ${mediaKind}`);
    return {
        candidateKey: text(source.candidateKey, "mediaCandidate.candidateKey"),
        mediaKind,
        providerRequestIdentity: text(source.providerRequestIdentity, "mediaCandidate.providerRequestIdentity"),
        resourceId: text(source.resourceId, "mediaCandidate.resourceId"),
        sourceTaskId: text(source.sourceTaskId, "mediaCandidate.sourceTaskId"),
    };
}

function parseVisualConsistencyReview(value: unknown): AgentVisualConsistencyReviewPayload {
    const source = exactObject(value, "Agent visual_consistency_review.v1 产物", [
        "reviewRunId",
        "reviewModelRecordId",
        "reviewRequestIdentity",
        "candidateRevisions",
        "confirmedReferenceRevisions",
        "assessments",
        "rankedCandidateRevisionIds",
        "uncertainties",
        "conflicts",
        "retrySuggestions",
    ]);
    return {
        reviewRunId: text(source.reviewRunId, "visualReview.reviewRunId"),
        reviewModelRecordId: text(source.reviewModelRecordId, "visualReview.reviewModelRecordId"),
        reviewRequestIdentity: text(source.reviewRequestIdentity, "visualReview.reviewRequestIdentity"),
        candidateRevisions: array(source.candidateRevisions, "visualReview.candidateRevisions").map((item, index) => parseRevisionRef(item, `visualReview.candidateRevisions[${index}]`)),
        confirmedReferenceRevisions: array(source.confirmedReferenceRevisions, "visualReview.confirmedReferenceRevisions").map((item, index) => parseRevisionRef(item, `visualReview.confirmedReferenceRevisions[${index}]`)),
        assessments: array(source.assessments, "visualReview.assessments").map((item, index) => parseVisualAssessment(item, index)),
        rankedCandidateRevisionIds: array(source.rankedCandidateRevisionIds, "visualReview.rankedCandidateRevisionIds").map((item, index) => text(item, `visualReview.rankedCandidateRevisionIds[${index}]`)),
        uncertainties: stringArray(source.uncertainties, "visualReview.uncertainties"),
        conflicts: stringArray(source.conflicts, "visualReview.conflicts"),
        retrySuggestions: stringArray(source.retrySuggestions, "visualReview.retrySuggestions"),
    };
}

function parseVisualAssessment(value: unknown, index: number): AgentVisualConsistencyReviewPayload["assessments"][number] {
    const label = `visualReview.assessments[${index}]`;
    const source = exactObject(value, label, ["candidateRevision", "visualEvidenceRevision", "findings"]);
    return {
        candidateRevision: parseRevisionRef(source.candidateRevision, `${label}.candidateRevision`),
        visualEvidenceRevision: parseRevisionRef(source.visualEvidenceRevision, `${label}.visualEvidenceRevision`),
        findings: array(source.findings, `${label}.findings`).map((item, findingIndex) => parseVisualFinding(item, `${label}.findings[${findingIndex}]`)),
    };
}

function parseVisualFinding(value: unknown, label: string): AgentVisualConsistencyFinding {
    const source = exactObject(value, label, ["dimension", "outcome", "description", "evidenceRevisions", "confidenceBasisPoints"]);
    const dimension = text(source.dimension, `${label}.dimension`);
    const dimensions = new Set<AgentVisualConsistencyFinding["dimension"]>(["identity", "clothing", "scene", "time_space", "composition", "screen_direction", "frame_continuity"]);
    if (!dimensions.has(dimension as AgentVisualConsistencyFinding["dimension"])) throw new Error(`${label}.dimension 无效`);
    const outcome = text(source.outcome, `${label}.outcome`);
    if (outcome !== "matched" && outcome !== "deviation" && outcome !== "uncertain") throw new Error(`${label}.outcome 无效`);
    const confidenceBasisPoints = integer(source.confidenceBasisPoints, `${label}.confidenceBasisPoints`, true);
    if (confidenceBasisPoints > 10_000) throw new Error(`${label}.confidenceBasisPoints 超出范围`);
    return {
        dimension: dimension as AgentVisualConsistencyFinding["dimension"],
        outcome,
        description: text(source.description, `${label}.description`),
        evidenceRevisions: array(source.evidenceRevisions, `${label}.evidenceRevisions`).map((item, revisionIndex) => parseRevisionRef(item, `${label}.evidenceRevisions[${revisionIndex}]`)),
        confidenceBasisPoints,
    };
}

function parseKeyNameDescription(value: unknown, label: string): { key: string; name: string; description: string } {
    const source = exactObject(value, label, ["key", "name", "description"]);
    return { key: text(source.key, `${label}.key`), name: text(source.name, `${label}.name`), description: text(source.description, `${label}.description`) };
}

function parseVisualIssues(value: unknown, label: string): Array<{ code: string; description: string; relatedKeys: string[] }> {
    return array(value, label).map((item, index) => {
        const issueLabel = `${label}[${index}]`;
        const issue = exactObject(item, issueLabel, ["code", "description", "relatedKeys"]);
        return { code: text(issue.code, `${issueLabel}.code`), description: text(issue.description, `${issueLabel}.description`), relatedKeys: stringArray(issue.relatedKeys, `${issueLabel}.relatedKeys`) };
    });
}

function revisionArray(value: unknown, label: string): AgentArtifactRevisionRef[] {
    return array(value, label).map((item, index) => parseRevisionRef(item, `${label}[${index}]`));
}

function oneOf<const T extends string>(value: unknown, label: string, values: readonly T[]): T {
    const parsed = text(value, label);
    if (!values.includes(parsed as T)) throw new Error(`${label} 无效`);
    return parsed as T;
}

function audioMode(value: unknown, label: string): AgentVideoPlanPayload["audioMode"] {
    return oneOf(value, label, ["none", "native", "independent"] as const);
}

function stringArray(value: unknown, label: string): string[] {
    return array(value, label).map((item, index) => text(item, `${label}[${index}]`));
}

function parseScriptBundle(value: unknown): AgentScriptBundle {
    const source = exactObject(value, "Agent script_bundle.v1 产物", ["title", "logline", "script", "characters", "scenes", "props", "voiceRoles"]);
    const parseNeed = (item: unknown, label: string) => {
        const need = exactObject(item, label, ["key", "name", "description"]);
        return { key: text(need.key, `${label}.key`), name: text(need.name, `${label}.name`), description: text(need.description, `${label}.description`) };
    };
    return {
        title: text(source.title, "script.title"),
        logline: text(source.logline, "script.logline"),
        script: text(source.script, "script.script"),
        characters: array(source.characters, "script.characters").map((item, index) => parseNeed(item, `script.characters[${index}]`)),
        scenes: array(source.scenes, "script.scenes").map((item, index) => parseNeed(item, `script.scenes[${index}]`)),
        props: array(source.props, "script.props").map((item, index) => parseNeed(item, `script.props[${index}]`)),
        voiceRoles: array(source.voiceRoles, "script.voiceRoles").map((item, index) => parseNeed(item, `script.voiceRoles[${index}]`)),
    };
}

function parseRevisionRef(value: unknown, label: string): AgentArtifactRevisionRef {
    const source = exactObject(value, label, ["artifactId", "revisionId"]);
    return { artifactId: text(source.artifactId, `${label}.artifactId`), revisionId: text(source.revisionId, `${label}.revisionId`) };
}

function parseSkillVersion(value: unknown, index: number): AgentArtifactRevision["skillVersions"][number] {
    const label = `artifact.skillVersions[${index}]`;
    const source = exactObject(value, label, ["dir", "name", "description", "instructions", "version", "checksum"]);
    const checksum = text(source.checksum, `${label}.checksum`);
    if (!/^[0-9a-f]{64}$/.test(checksum)) throw new Error(`${label}.checksum 必须是 64 位小写 SHA-256`);
    return {
        dir: text(source.dir, `${label}.dir`),
        name: text(source.name, `${label}.name`),
        description: text(source.description, `${label}.description`, true),
        instructions: text(source.instructions, `${label}.instructions`),
        version: integer(source.version, `${label}.version`, true),
        checksum,
    };
}

export function parseStageReviewResult(value: unknown): AgentStageReviewResult {
    const source = exactObject(value, "Agent 阶段审核结果", ["stage", "artifactRevisionIds", "selectedCandidateRevisionId", "publication"]);
    const stageSource = exactObject(source.stage, "Agent 阶段", ["id", "stageKey", "specialistKey", "reviewPolicy", "costPolicy", "status", "version", "reviewRevisionId", "lastErrorCode", "updatedAt"]);
    const specialistKey = text(stageSource.specialistKey, "stage.specialistKey");
    if (!specialistSet.has(specialistKey)) throw new Error(`不受支持的 Agent 专家: ${specialistKey}`);
    if (stageSource.reviewPolicy !== "required") throw new Error("Agent 审核策略无效");
    if (stageSource.costPolicy !== "none" && stageSource.costPolicy !== "approval_required") throw new Error("Agent 成本策略无效");
    const stage: AgentStageReviewResult["stage"] = {
        id: text(stageSource.id, "stage.id"),
        stageKey: text(stageSource.stageKey, "stage.stageKey"),
        specialistKey: specialistKey as AgentStageReviewResult["stage"]["specialistKey"],
        reviewPolicy: stageSource.reviewPolicy,
        costPolicy: stageSource.costPolicy,
        status: stageStatus(stageSource.status),
        version: integer(stageSource.version, "stage.version"),
        updatedAt: isoInstant(stageSource.updatedAt, "stage.updatedAt"),
    };
    if (stageSource.reviewRevisionId !== undefined) stage.reviewRevisionId = text(stageSource.reviewRevisionId, "stage.reviewRevisionId");
    if (stageSource.lastErrorCode !== undefined) stage.lastErrorCode = text(stageSource.lastErrorCode, "stage.lastErrorCode");
    const result: AgentStageReviewResult = {
        stage,
        artifactRevisionIds: array(source.artifactRevisionIds, "review.artifactRevisionIds").map((item, index) => text(item, `review.artifactRevisionIds[${index}]`)),
    };
    if (source.selectedCandidateRevisionId !== undefined) result.selectedCandidateRevisionId = text(source.selectedCandidateRevisionId, "review.selectedCandidateRevisionId");
    if (source.publication !== undefined) result.publication = parseAssetPublicationResult(source.publication);
    return result;
}

function parseAssetPublicationResult(value: unknown): AgentAssetPublicationResult {
    const source = exactObject(value, "Agent 资产发布结果", [
        "id",
        "artifactRevisionId",
        "assetId",
        "assetVersionId",
        "projectAssetLinkId",
        "representationId",
        "status",
        "replayed",
    ]);
    if (source.status !== "succeeded") throw new Error("Agent 资产发布结果必须是 succeeded");
    return {
        id: text(source.id, "publication.id"),
        artifactRevisionId: text(source.artifactRevisionId, "publication.artifactRevisionId"),
        assetId: text(source.assetId, "publication.assetId"),
        assetVersionId: text(source.assetVersionId, "publication.assetVersionId"),
        projectAssetLinkId: text(source.projectAssetLinkId, "publication.projectAssetLinkId"),
        representationId: text(source.representationId, "publication.representationId"),
        status: "succeeded",
        replayed: flag(source.replayed, "publication.replayed"),
    };
}

function parsePublicationIntent(value: unknown): AgentPublicationIntent {
    const source = exactObject(value, "Agent 资产入库意图", ["publicationPurpose", "targetCategory", "targetBindingKey"]);
    return {
        publicationPurpose: text(source.publicationPurpose, "publicationIntent.publicationPurpose"),
        targetCategory: assetCategory(source.targetCategory),
        targetBindingKey: text(source.targetBindingKey, "publicationIntent.targetBindingKey"),
    };
}

function validateResolution(content: AgentStageReviewResolutionContent): void {
    const expectedStatus = content.decision === "approved" ? "approved" : content.decision === "revision_requested" ? "running" : "stopped";
    if (content.resultStatus !== expectedStatus) throw new Error("Agent 阶段审核决议状态冲突");
    if (content.decision === "revision_requested" ? content.resultReviewRevisionId !== undefined : content.resultReviewRevisionId !== content.revisionId) {
        throw new Error("Agent 阶段审核产物版本冲突");
    }
    if (content.publicationIntent && content.decision !== "approved") throw new Error("Agent 非批准决议不能携带资产入库意图");
}

function artifactSchema(value: unknown): AgentProductionArtifactSchema {
    const schema = text(value, "artifact.schema");
    if (!artifactSchemaSet.has(schema)) throw new Error(`不受支持的 Agent 产物 schema: ${schema}`);
    return schema as AgentProductionArtifactSchema;
}

function assetCategory(value: unknown): AgentAssetCategory {
    const category = text(value, "asset.category");
    if (!categorySet.has(category as AgentAssetCategory)) throw new Error(`不受支持的资产分类: ${category}`);
    return category as AgentAssetCategory;
}

function stageStatus(value: unknown): AgentProductionStageStatus {
    const status = text(value, "stage.status");
    if (!stageStatusSet.has(status as AgentProductionStageStatus)) throw new Error(`不受支持的 Agent 阶段状态: ${status}`);
    return status as AgentProductionStageStatus;
}

function reviewDecision(value: unknown): AgentStageReviewDecision {
    if (value !== "approved" && value !== "revision_requested" && value !== "stopped") throw new Error(`不受支持的 Agent 阶段决议: ${String(value)}`);
    return value;
}

function isoInstant(value: unknown, label: string): string {
    const source = text(value, label);
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(source) || Number.isNaN(Date.parse(source))) throw new Error(`${label} 必须是 UTC ISO-8601 时间`);
    return source;
}

function rejectTransientMediaLocator(value: unknown, label: string): void {
    if (Array.isArray(value)) {
        value.forEach((item, index) => rejectTransientMediaLocator(item, `${label}[${index}]`));
        return;
    }
    if (!value || typeof value !== "object") return;
    for (const [key, nested] of Object.entries(value)) {
        if (key === "url" || key === "signedUrl") throw new Error(`${label} 不允许返回短期媒体地址字段: ${key}`);
        rejectTransientMediaLocator(nested, `${label}.${key}`);
    }
}
