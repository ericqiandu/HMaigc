package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

var (
	errAgentCanvasProjectionInvalid       = errors.New("agent canvas projection facts are invalid")
	errAgentCanvasProjectionMediaNotReady = errors.New("agent canvas projection media is not ready")
)

type agentCanvasProjectionBinding struct {
	ArtifactID   string `json:"artifactId"`
	RevisionID   string `json:"revisionId"`
	NodeID       string `json:"nodeId"`
	ProjectionID string `json:"projectionId"`
}

type agentCanvasProjectionNode struct {
	ID       string                            `json:"id"`
	Type     string                            `json:"type"`
	Title    string                            `json:"title"`
	Position agentCanvasPosition               `json:"position"`
	Width    float64                           `json:"width"`
	Height   float64                           `json:"height"`
	Metadata agentCanvasProjectionNodeMetadata `json:"metadata"`
}

type agentCanvasProjectionNodeMetadata struct {
	Content               string `json:"content,omitempty"`
	ComposerContent       string `json:"composerContent,omitempty"`
	Prompt                string `json:"prompt,omitempty"`
	Status                string `json:"status"`
	GenerationMode        string `json:"generationMode,omitempty"`
	ChannelID             string `json:"channelId,omitempty"`
	Model                 string `json:"model,omitempty"`
	Size                  string `json:"size,omitempty"`
	Quality               string `json:"quality,omitempty"`
	Count                 int    `json:"count,omitempty"`
	TransparentBackground string `json:"transparentBackground,omitempty"`
	VQuality              string `json:"vquality,omitempty"`
	GenerateAudio         string `json:"generateAudio,omitempty"`
	StorageKey            string `json:"storageKey,omitempty"`
	MimeType              string `json:"mimeType,omitempty"`
	DurationMS            int64  `json:"durationMs,omitempty"`
	Seconds               string `json:"seconds,omitempty"`
	AudioVoice            string `json:"audioVoice,omitempty"`
	AudioFormat           string `json:"audioFormat,omitempty"`
	AudioSpeed            string `json:"audioSpeed,omitempty"`
	AudioVolume           string `json:"audioVolume,omitempty"`
	AudioPitch            string `json:"audioPitch,omitempty"`
	AudioEmotion          string `json:"audioEmotion,omitempty"`
	AudioLanguageBoost    string `json:"audioLanguageBoost,omitempty"`
	AudioSampleRate       string `json:"audioSampleRate,omitempty"`
	AudioBitrate          string `json:"audioBitrate,omitempty"`
	AudioChannel          string `json:"audioChannel,omitempty"`
	AudioInstructions     string `json:"audioInstructions,omitempty"`
	WorkflowKind          string `json:"workflowKind"`
	WorkflowTitle         string `json:"workflowTitle"`
	ArtifactID            string `json:"artifactId"`
	ArtifactRevisionID    string `json:"artifactRevisionId"`
	ProjectionID          string `json:"projectionId"`
}

func buildAgentCanvasProjectionPatch(
	canvasID string,
	revisions []model.AgentArtifactRevision,
	resources map[string]model.Resource,
	mediaArguments map[agentruntime.ArtifactRevisionRef]agentMediaGenerationArguments,
) (CanvasMutationPatch, []agentCanvasProjectionBinding, error) {
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" || len(revisions) == 0 || len(revisions) > 64 {
		return CanvasMutationPatch{}, nil, errAgentCanvasProjectionInvalid
	}
	ordered := append([]model.AgentArtifactRevision(nil), revisions...)
	sortArtifactRevisions(ordered)
	nodeByArtifact := make(map[string]string, len(ordered))
	revisionByArtifact := make(map[string]model.AgentArtifactRevision, len(ordered))
	upstreamByArtifact := make(map[string][]agentruntime.ArtifactRevisionRef, len(ordered))
	for _, revision := range ordered {
		if revision.ArtifactID == "" || revision.ID == "" || revision.ArtifactKey == "" || revision.Revision < 1 ||
			revision.LifecycleStatus == model.AgentArtifactRevisionStale {
			return CanvasMutationPatch{}, nil, errAgentCanvasProjectionInvalid
		}
		if _, duplicate := revisionByArtifact[revision.ArtifactID]; duplicate {
			return CanvasMutationPatch{}, nil, errAgentCanvasProjectionInvalid
		}
		upstream, err := validateAgentCanvasProjectionRevision(revision)
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		revisionByArtifact[revision.ArtifactID] = revision
		upstreamByArtifact[revision.ArtifactID] = upstream
		nodeByArtifact[revision.ArtifactID] = agentCanvasProjectionNodeID(canvasID, revision.ArtifactID)
	}

	patch := CanvasMutationPatch{
		UpsertNodes:       make([]json.RawMessage, 0, len(ordered)),
		UpsertConnections: []json.RawMessage{},
	}
	bindings := make([]agentCanvasProjectionBinding, 0, len(ordered))
	for index, revision := range ordered {
		ref := agentruntime.ArtifactRevisionRef{ArtifactID: revision.ArtifactID, RevisionID: revision.ID}
		node, err := agentCanvasProjectionNodeForRevision(canvasID, index, revision, resources, mediaArguments[ref])
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		encoded, err := json.Marshal(node)
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		patch.UpsertNodes = append(patch.UpsertNodes, encoded)
		bindings = append(bindings, agentCanvasProjectionBinding{
			ArtifactID: revision.ArtifactID, RevisionID: revision.ID, NodeID: node.ID, ProjectionID: node.Metadata.ProjectionID,
		})
	}
	seenConnections := make(map[string]struct{})
	for _, revision := range ordered {
		toNodeID := nodeByArtifact[revision.ArtifactID]
		for _, upstream := range upstreamByArtifact[revision.ArtifactID] {
			stored, selected := revisionByArtifact[upstream.ArtifactID]
			if !selected {
				continue
			}
			if stored.ID != upstream.RevisionID {
				return CanvasMutationPatch{}, nil, errAgentCanvasProjectionInvalid
			}
			fromNodeID := nodeByArtifact[upstream.ArtifactID]
			connectionID := productionCanvasStableID("artifact-edge", canvasID, upstream.ArtifactID, revision.ArtifactID)
			if _, duplicate := seenConnections[connectionID]; duplicate {
				continue
			}
			seenConnections[connectionID] = struct{}{}
			encoded, err := json.Marshal(productionCanvasConnection{ID: connectionID, FromNodeID: fromNodeID, ToNodeID: toNodeID})
			if err != nil {
				return CanvasMutationPatch{}, nil, err
			}
			patch.UpsertConnections = append(patch.UpsertConnections, encoded)
		}
	}
	return patch, bindings, nil
}

func sortArtifactRevisions(revisions []model.AgentArtifactRevision) {
	sort.Slice(revisions, func(left int, right int) bool {
		return revisions[left].ArtifactID < revisions[right].ArtifactID
	})
}

func validateAgentCanvasProjectionRevision(revision model.AgentArtifactRevision) ([]agentruntime.ArtifactRevisionRef, error) {
	var upstream []agentruntime.ArtifactRevisionRef
	if err := json.Unmarshal([]byte(revision.UpstreamRevisionsJSON), &upstream); err != nil || upstream == nil {
		return nil, errAgentCanvasProjectionInvalid
	}
	var skills []agentruntime.SkillSelection
	if err := json.Unmarshal([]byte(revision.SkillVersionsJSON), &skills); err != nil || skills == nil {
		return nil, errAgentCanvasProjectionInvalid
	}
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: revision.ArtifactKey, Kind: revision.Kind, SchemaVersion: revision.SchemaVersion,
		Payload: json.RawMessage(revision.PayloadJSON), ResourceID: revision.ResourceID,
		UpstreamRevisions: upstream, ModelRequestIdentity: revision.ModelRequestIdentity, SkillVersions: skills,
	}
	if err := agentruntime.ValidateArtifactDraft(draft); err != nil {
		return nil, errors.Join(errAgentCanvasProjectionInvalid, err)
	}
	return upstream, nil
}

func agentCanvasProjectionNodeForRevision(
	canvasID string,
	index int,
	revision model.AgentArtifactRevision,
	resources map[string]model.Resource,
	media agentMediaGenerationArguments,
) (agentCanvasProjectionNode, error) {
	projectionID := productionCanvasStableID("artifact-projection", canvasID, revision.ArtifactID)
	metadata := agentCanvasProjectionNodeMetadata{
		Status: "success", WorkflowKind: "free", WorkflowTitle: revision.ArtifactKey,
		ArtifactID: revision.ArtifactID, ArtifactRevisionID: revision.ID, ProjectionID: projectionID,
	}
	node := agentCanvasProjectionNode{
		ID: agentCanvasProjectionNodeID(canvasID, revision.ArtifactID), Type: "text", Title: revision.ArtifactKey,
		Position: agentCanvasProjectionPosition(index), Width: 440, Height: 260, Metadata: metadata,
	}
	schemaID := revision.Kind + ".v" + strconv.Itoa(revision.SchemaVersion)
	switch schemaID {
	case agentruntime.ArtifactSchemaScriptBundleV1:
		var bundle agentruntime.ScriptBundle
		if err := decodeStrictProjectionPayload(revision.PayloadJSON, &bundle); err != nil {
			return agentCanvasProjectionNode{}, err
		}
		node.Type = "script"
		node.Title = bundle.Title
		node.Width = 520
		node.Height = 360
		node.Metadata.Content = bundle.Script
		node.Metadata.ComposerContent = bundle.Script
		node.Metadata.WorkflowKind = "script"
		node.Metadata.WorkflowTitle = bundle.Title
	case agentruntime.ArtifactSchemaMediaCandidateV1:
		candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
		if err != nil || candidate.ResourceID != revision.ResourceID || media.Capability != string(candidate.MediaKind) ||
			media.GenerationModel.ChannelID == "" || media.GenerationModel.Model == "" {
			return agentCanvasProjectionNode{}, errAgentCanvasProjectionInvalid
		}
		resource, exists := resources[revision.ResourceID]
		if !exists || resource.ID != candidate.ResourceID || resource.Kind != string(candidate.MediaKind) || !agentMediaResourceReady(&resource) {
			return agentCanvasProjectionNode{}, errAgentCanvasProjectionMediaNotReady
		}
		node.Type = string(candidate.MediaKind)
		node.Width = 420
		node.Height = 236
		if candidate.MediaKind == agentruntime.ArtifactAudio {
			node.Height = 120
		}
		node.Metadata.Content = "/api/resources/" + resource.ID + "/file"
		node.Metadata.StorageKey = "resource:" + resource.ID
		node.Metadata.MimeType = resource.MimeType
		node.Metadata.DurationMS = resource.DurationMs
		node.Metadata.GenerationMode = string(candidate.MediaKind)
		node.Metadata.ChannelID = media.GenerationModel.ChannelID
		node.Metadata.Model = media.GenerationModel.Model
		node.Metadata.WorkflowKind = "shot"
		if err := applyAgentCanvasMediaParameters(&node.Metadata, media); err != nil {
			return agentCanvasProjectionNode{}, err
		}
	default:
		formatted, err := formatAgentCanvasProjectionJSON(revision.PayloadJSON)
		if err != nil {
			return agentCanvasProjectionNode{}, err
		}
		node.Metadata.Content = formatted
		node.Metadata.ComposerContent = formatted
	}
	return node, nil
}

func applyAgentCanvasMediaParameters(metadata *agentCanvasProjectionNodeMetadata, media agentMediaGenerationArguments) error {
	switch media.Capability {
	case "image":
		parameters, err := decodeAgentImageGenerationParameters(media.Parameters)
		if err != nil {
			return errAgentCanvasProjectionInvalid
		}
		metadata.Prompt = parameters.Prompt
		metadata.Size = parameters.AspectRatio
		metadata.Quality = parameters.Quality
		metadata.Count = parameters.Count
		metadata.TransparentBackground = strconv.FormatBool(parameters.TransparentBackground)
	case "video":
		parameters, err := decodeAgentVideoGenerationParameters(media.Parameters)
		if err != nil {
			return errAgentCanvasProjectionInvalid
		}
		metadata.Prompt = parameters.Prompt
		metadata.Size = parameters.AspectRatio
		metadata.VQuality = parameters.Resolution
		metadata.Seconds = strconv.Itoa(parameters.DurationSeconds)
		metadata.Count = 1
		metadata.GenerateAudio = strconv.FormatBool(parameters.GenerateAudio)
	case "audio":
		parameters, err := decodeAgentAudioGenerationParameters(media.Parameters)
		if err != nil {
			return errAgentCanvasProjectionInvalid
		}
		metadata.Prompt = parameters.Prompt
		metadata.AudioVoice = parameters.Voice
		metadata.AudioFormat = parameters.Format
		metadata.AudioSpeed = parameters.Speed
		metadata.AudioVolume = parameters.Volume
		metadata.AudioPitch = parameters.Pitch
		metadata.AudioEmotion = parameters.Emotion
		metadata.AudioLanguageBoost = parameters.LanguageBoost
		metadata.AudioSampleRate = parameters.SampleRate
		metadata.AudioBitrate = parameters.Bitrate
		metadata.AudioChannel = parameters.Channel
		metadata.AudioInstructions = parameters.Instructions
	default:
		return errAgentCanvasProjectionInvalid
	}
	return nil
}

func decodeStrictProjectionPayload[T interface{}](payload string, target *T) error {
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.Join(errAgentCanvasProjectionInvalid, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errAgentCanvasProjectionInvalid
	}
	return nil
}

func formatAgentCanvasProjectionJSON(payload string) (string, error) {
	canonical, err := canonicalAgentJSON([]byte(payload))
	if err != nil {
		return "", errors.Join(errAgentCanvasProjectionInvalid, err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, canonical, "", "  "); err != nil {
		return "", errors.Join(errAgentCanvasProjectionInvalid, err)
	}
	return formatted.String(), nil
}

func agentCanvasProjectionPosition(index int) agentCanvasPosition {
	return agentCanvasPosition{X: float64((index%3)*480 + 120), Y: float64((index/3)*340 + 120)}
}

func agentCanvasProjectionNodeID(canvasID string, artifactID string) string {
	return productionCanvasStableID("artifact-node", canvasID, artifactID)
}

func agentCanvasProjectionFailureDetails(err error) (string, agentruntime.ToolFailureClass) {
	switch {
	case errors.Is(err, errAgentCanvasProjectionMediaNotReady):
		return "canvas_projection_media_not_ready", agentruntime.ToolFailureAgentRepairable
	case errors.Is(err, errAgentCanvasProjectionInvalid):
		return "canvas_projection_invalid", agentruntime.ToolFailureAgentRepairable
	default:
		return "canvas_projection_failed", agentruntime.ToolFailureTerminal
	}
}
