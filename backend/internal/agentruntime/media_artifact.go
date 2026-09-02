package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const (
	maximumMediaGenerationCandidates = 32
	mediaGenerationOperationPrefix   = "media_generation:"
	mediaAssemblyOperationPrefix     = "media_assembly:"
)

// MediaCandidateContent is the immutable payload stored for a generated media
// candidate. These fields are the minimum commercial lineage required to
// authorize or audit a later publication.
type MediaCandidateContent struct {
	CandidateKey            string       `json:"candidateKey"`
	MediaKind               ArtifactKind `json:"mediaKind"`
	ProviderRequestIdentity string       `json:"providerRequestIdentity"`
	ResourceID              string       `json:"resourceId"`
	SourceTaskID            string       `json:"sourceTaskId"`
}

func (content MediaCandidateContent) Validate() error {
	if !validArtifactText(content.CandidateKey, 120) ||
		!validMediaArtifactKind(content.MediaKind) ||
		!validArtifactText(content.ProviderRequestIdentity, 180) ||
		!validArtifactText(content.ResourceID, 80) ||
		!validArtifactText(content.SourceTaskID, 80) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeMediaCandidateContent(payload []byte) (MediaCandidateContent, error) {
	var content MediaCandidateContent
	if err := decodeStrictMediaArtifact(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&content)
	}); err != nil || content.Validate() != nil {
		return MediaCandidateContent{}, ErrArtifactPayloadInvalid
	}
	return content, nil
}

// MediaGenerationToolResult is the durable success result of media.generate.
// It binds the exact billed task to the immutable candidate revisions created
// from that task.
type MediaGenerationToolResult struct {
	TaskID         string                `json:"taskId"`
	BillingOrderID string                `json:"billingOrderId"`
	AudioMode      MediaAudioMode        `json:"audioMode"`
	Candidates     []ArtifactRevisionRef `json:"candidates"`
}

const (
	MediaAssemblyContentType = "media_assembly"
	MediaAssemblyTaskType    = "agent_media_assembly"
)

type MediaAssemblyTaskStatus string

const (
	MediaAssemblyTaskQueued    MediaAssemblyTaskStatus = "queued"
	MediaAssemblyTaskRunning   MediaAssemblyTaskStatus = "running"
	MediaAssemblyTaskSucceeded MediaAssemblyTaskStatus = "succeeded"
	MediaAssemblyTaskFailed    MediaAssemblyTaskStatus = "failed"
	MediaAssemblyTaskCancelled MediaAssemblyTaskStatus = "cancelled"
)

type MediaAssemblyFinal struct {
	ArtifactRevision ArtifactRevisionRef `json:"artifactRevision"`
	ResourceID       string              `json:"resourceId"`
	Adopted          bool                `json:"adopted"`
}

// MediaAssemblyTimelineContent is the only user-visible assembly lifecycle
// contract. It carries persisted task facts, never synthetic percentages,
// provider reasoning or transient media locators.
type MediaAssemblyTimelineContent struct {
	ContentType   string                  `json:"contentType"`
	ToolCallID    string                  `json:"toolCallId"`
	ActionVersion int                     `json:"actionVersion"`
	TaskID        string                  `json:"taskId"`
	TaskStatus    MediaAssemblyTaskStatus `json:"taskStatus"`
	Stage         string                  `json:"stage"`
	ClipCount     int                     `json:"clipCount"`
	AudioMode     MediaAudioMode          `json:"audioMode"`
	Output        AssemblyOutputV2        `json:"output"`
	PlanRevision  ArtifactRevisionRef     `json:"planRevision"`
	Final         *MediaAssemblyFinal     `json:"final,omitempty"`
	ErrorCode     string                  `json:"errorCode,omitempty"`
}

func (content MediaAssemblyTimelineContent) Validate() error {
	if content.ContentType != MediaAssemblyContentType || !validArtifactText(content.ToolCallID, 120) ||
		content.ActionVersion < 1 || !validArtifactText(content.TaskID, 80) || !validArtifactText(content.Stage, 240) ||
		content.ClipCount < 1 || content.PlanRevision.Validate() != nil || !content.AudioMode.Valid() ||
		!validAssemblyOutputV2(content.Output, content.AudioMode) {
		return ErrArtifactPayloadInvalid
	}
	switch content.TaskStatus {
	case MediaAssemblyTaskQueued, MediaAssemblyTaskRunning:
		if content.Final != nil || content.ErrorCode != "" {
			return ErrArtifactPayloadInvalid
		}
	case MediaAssemblyTaskSucceeded:
		if content.Final == nil || content.ErrorCode != "" || content.Final.ArtifactRevision.Validate() != nil ||
			!validArtifactText(content.Final.ResourceID, 80) {
			return ErrArtifactPayloadInvalid
		}
	case MediaAssemblyTaskFailed:
		if content.Final != nil || !validArtifactText(content.ErrorCode, 120) {
			return ErrArtifactPayloadInvalid
		}
	case MediaAssemblyTaskCancelled:
		if !validArtifactText(content.ErrorCode, 120) ||
			(content.Final != nil && (content.Final.Adopted || content.Final.ArtifactRevision.Validate() != nil ||
				!validArtifactText(content.Final.ResourceID, 80))) {
			return ErrArtifactPayloadInvalid
		}
	default:
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeMediaAssemblyTimelineContent(payload []byte) (MediaAssemblyTimelineContent, error) {
	var content MediaAssemblyTimelineContent
	if err := decodeStrictMediaArtifact(payload, func(decoder *json.Decoder) error { return decoder.Decode(&content) }); err != nil || content.Validate() != nil {
		return MediaAssemblyTimelineContent{}, ErrArtifactPayloadInvalid
	}
	return content, nil
}

func (result MediaGenerationToolResult) Validate() error {
	if !validArtifactText(result.TaskID, 80) ||
		!validArtifactText(result.BillingOrderID, 80) ||
		!result.AudioMode.Valid() ||
		len(result.Candidates) == 0 ||
		len(result.Candidates) > maximumMediaGenerationCandidates ||
		validateArtifactRevisionRefs(result.Candidates) != nil {
		return ErrArtifactPayloadInvalid
	}
	return nil
}

func DecodeMediaGenerationToolResult(payload []byte) (MediaGenerationToolResult, error) {
	var result MediaGenerationToolResult
	if err := decodeStrictMediaArtifact(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&result)
	}); err != nil || result.Validate() != nil {
		return MediaGenerationToolResult{}, ErrArtifactPayloadInvalid
	}
	return result, nil
}

// MediaTaskResultResource is a generated Resource reference extracted from a
// provider Task result. Provider metadata may coexist in the result, but every
// returned media item must carry an exact persisted Resource ID.
type MediaTaskResultResource struct {
	Kind       ArtifactKind
	ResourceID string
}

func DecodeMediaTaskResultResources(payload []byte) ([]MediaTaskResultResource, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return nil, ErrArtifactPayloadInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrArtifactPayloadInvalid
	}

	resources := make([]MediaTaskResultResource, 0, 4)
	seen := make(map[string]struct{})
	for _, group := range []struct {
		plural   string
		singular string
		kind     ArtifactKind
	}{
		{plural: "images", singular: "image", kind: ArtifactImage},
		{plural: "videos", singular: "video", kind: ArtifactVideo},
		{plural: "audios", singular: "audio", kind: ArtifactAudio},
	} {
		for _, key := range []string{group.plural, group.singular} {
			raw, exists := document[key]
			if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				continue
			}
			resourceIDs, err := decodeMediaTaskResourceIDs(raw)
			if err != nil {
				return nil, ErrArtifactPayloadInvalid
			}
			for _, resourceID := range resourceIDs {
				identity := string(group.kind) + "\x00" + resourceID
				if _, duplicate := seen[identity]; duplicate {
					continue
				}
				seen[identity] = struct{}{}
				resources = append(resources, MediaTaskResultResource{Kind: group.kind, ResourceID: resourceID})
			}
		}
	}
	if len(resources) == 0 || len(resources) > maximumMediaGenerationCandidates {
		return nil, ErrArtifactPayloadInvalid
	}
	return resources, nil
}

func MediaGenerationOperationForRun(runID string) (string, error) {
	if !validArtifactText(runID, 80) {
		return "", ErrArtifactPayloadInvalid
	}
	return mediaGenerationOperationPrefix + runID, nil
}

func MediaAssemblyOperationForRun(runID string) (string, error) {
	if !validArtifactText(runID, 80) {
		return "", ErrArtifactPayloadInvalid
	}
	digest := sha256.Sum256([]byte(runID))
	return mediaAssemblyOperationPrefix + hex.EncodeToString(digest[:16]), nil
}

func decodeMediaTaskResourceIDs(payload json.RawMessage) ([]string, error) {
	type resourceReference struct {
		ResourceID string `json:"resourceId"`
	}
	trimmed := bytes.TrimSpace(payload)
	var items []resourceReference
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
	} else {
		var item resourceReference
		if err := json.Unmarshal(trimmed, &item); err != nil {
			return nil, err
		}
		items = []resourceReference{item}
	}
	if len(items) == 0 {
		return nil, ErrArtifactPayloadInvalid
	}
	resourceIDs := make([]string, 0, len(items))
	for _, item := range items {
		if !validArtifactText(item.ResourceID, 80) {
			return nil, ErrArtifactPayloadInvalid
		}
		resourceIDs = append(resourceIDs, item.ResourceID)
	}
	return resourceIDs, nil
}

func validMediaArtifactKind(kind ArtifactKind) bool {
	return kind == ArtifactImage || kind == ArtifactVideo || kind == ArtifactAudio
}

func decodeStrictMediaArtifact(payload []byte, decode func(*json.Decoder) error) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decode(decoder); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrArtifactPayloadInvalid
	}
	return nil
}
