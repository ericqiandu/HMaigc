package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type ReferenceMediaType string

const (
	ReferenceMediaImage ReferenceMediaType = "image"
	ReferenceMediaVideo ReferenceMediaType = "video"
	ReferenceMediaAudio ReferenceMediaType = "audio"
)

var (
	ErrReferenceManifestInvalid            = errors.New("reference manifest is invalid")
	ErrReferenceManifestAssetKeyDuplicate  = errors.New("reference manifest asset key is duplicated")
	ErrReferenceManifestResourceURLMissing = errors.New("reference manifest resource URL is missing")
)

// ReferenceManifestEntry is a derived, immutable projection of one exact
// artifact/resource revision. AssetKey remains stable when Ordinal changes.
type ReferenceManifestEntry struct {
	AssetKey       string             `json:"assetKey"`
	SourceNodeID   string             `json:"sourceNodeId,omitempty"`
	TargetNodeID   string             `json:"targetNodeId,omitempty"`
	EdgeID         string             `json:"edgeId,omitempty"`
	MediaType      ReferenceMediaType `json:"mediaType"`
	SemanticRole   string             `json:"semanticRole"`
	Handle         string             `json:"handle,omitempty"`
	ArtifactID     string             `json:"artifactId"`
	RevisionID     string             `json:"revisionId"`
	ResourceID     string             `json:"resourceId"`
	ResourceURL    string             `json:"resourceUrl"`
	SourceRevision string             `json:"sourceRevision,omitempty"`
	Ordinal        int                `json:"ordinal"`
}

type ReferenceManifest struct {
	Entries []ReferenceManifestEntry `json:"entries"`
}

func NewReferenceManifest(entries []ReferenceManifestEntry) (ReferenceManifest, error) {
	manifest := ReferenceManifest{Entries: append([]ReferenceManifestEntry(nil), entries...)}
	if err := manifest.Validate(); err != nil {
		return ReferenceManifest{}, err
	}
	sort.SliceStable(manifest.Entries, func(left int, right int) bool {
		return manifest.Entries[left].Ordinal < manifest.Entries[right].Ordinal
	})
	return manifest, nil
}

func DecodeReferenceManifest(raw []byte) (ReferenceManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest ReferenceManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ReferenceManifest{}, fmt.Errorf("%w: %v", ErrReferenceManifestInvalid, err)
	}
	if err := ensureReferenceManifestEOF(decoder); err != nil {
		return ReferenceManifest{}, err
	}
	return NewReferenceManifest(manifest.Entries)
}

func (manifest ReferenceManifest) Validate() error {
	assetKeys := make(map[string]struct{}, len(manifest.Entries))
	ordinals := make(map[int]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, exists := assetKeys[entry.AssetKey]; exists {
			return fmt.Errorf("%w: %s", ErrReferenceManifestAssetKeyDuplicate, entry.AssetKey)
		}
		assetKeys[entry.AssetKey] = struct{}{}
		if _, exists := ordinals[entry.Ordinal]; exists {
			return fmt.Errorf("%w: duplicate ordinal %d", ErrReferenceManifestInvalid, entry.Ordinal)
		}
		ordinals[entry.Ordinal] = struct{}{}
	}
	return nil
}

func (entry ReferenceManifestEntry) Validate() error {
	if !validReferenceManifestText(entry.AssetKey, 120) ||
		!validReferenceManifestText(entry.SemanticRole, 80) ||
		!validReferenceManifestText(entry.ResourceID, 120) ||
		entry.Ordinal < 1 {
		return ErrReferenceManifestInvalid
	}
	if entry.MediaType != ReferenceMediaImage && entry.MediaType != ReferenceMediaVideo && entry.MediaType != ReferenceMediaAudio {
		return ErrReferenceManifestInvalid
	}
	if err := (ArtifactRevisionRef{ArtifactID: entry.ArtifactID, RevisionID: entry.RevisionID}).Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrReferenceManifestInvalid, err)
	}
	if strings.TrimSpace(entry.ResourceURL) == "" {
		return ErrReferenceManifestResourceURLMissing
	}
	if strings.TrimSpace(entry.ResourceURL) != entry.ResourceURL || len(entry.ResourceURL) > 2048 ||
		!validOptionalReferenceManifestText(entry.SourceNodeID, 120) ||
		!validOptionalReferenceManifestText(entry.TargetNodeID, 120) ||
		!validOptionalReferenceManifestText(entry.EdgeID, 120) ||
		!validOptionalReferenceManifestText(entry.Handle, 120) ||
		!validOptionalReferenceManifestText(entry.SourceRevision, 120) {
		return ErrReferenceManifestInvalid
	}
	return nil
}

func validReferenceManifestText(value string, maximum int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maximum
}

func validOptionalReferenceManifestText(value string, maximum int) bool {
	return value == "" || (strings.TrimSpace(value) == value && len(value) <= maximum)
}

func ensureReferenceManifestEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("%w: %v", ErrReferenceManifestInvalid, err)
	}
	return ErrReferenceManifestInvalid
}
