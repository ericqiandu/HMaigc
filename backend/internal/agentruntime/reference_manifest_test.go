package agentruntime

import (
	"errors"
	"testing"
)

func TestReferenceManifestKeepsStableAssetKeysAcrossReorder(t *testing.T) {
	entries := []ReferenceManifestEntry{
		referenceManifestEntry("character-xiaoming", ReferenceMediaImage, "artifact-character", "revision-character", "resource-character", 2),
		referenceManifestEntry("voice-xiaoming", ReferenceMediaAudio, "artifact-voice", "revision-voice", "resource-voice", 1),
	}

	manifest, err := NewReferenceManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Entries[0].AssetKey; got != "voice-xiaoming" {
		t.Fatalf("first asset key = %q, want voice-xiaoming", got)
	}
	if got := manifest.Entries[1].AssetKey; got != "character-xiaoming" {
		t.Fatalf("second asset key = %q, want character-xiaoming", got)
	}

	reordered, err := NewReferenceManifest([]ReferenceManifestEntry{
		withReferenceOrdinal(entries[0], 1),
		withReferenceOrdinal(entries[1], 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Entries[0].AssetKey != "character-xiaoming" || reordered.Entries[1].AssetKey != "voice-xiaoming" {
		t.Fatalf("reordered entries = %#v", reordered.Entries)
	}
	if reordered.Entries[0].RevisionID != "revision-character" || reordered.Entries[1].RevisionID != "revision-voice" {
		t.Fatalf("reorder changed revision identity: %#v", reordered.Entries)
	}
}

func TestReferenceManifestRejectsMissingResourceURL(t *testing.T) {
	entry := referenceManifestEntry("character-xiaoming", ReferenceMediaImage, "artifact-character", "revision-character", "resource-character", 1)
	entry.ResourceURL = ""
	_, err := NewReferenceManifest([]ReferenceManifestEntry{entry})
	if !errors.Is(err, ErrReferenceManifestResourceURLMissing) {
		t.Fatalf("NewReferenceManifest() error = %v, want missing resource URL", err)
	}
}

func TestReferenceManifestRejectsDuplicateAssetKey(t *testing.T) {
	_, err := NewReferenceManifest([]ReferenceManifestEntry{
		referenceManifestEntry("character-xiaoming", ReferenceMediaImage, "artifact-character", "revision-character-1", "resource-character-1", 1),
		referenceManifestEntry("character-xiaoming", ReferenceMediaImage, "artifact-character", "revision-character-2", "resource-character-2", 2),
	})
	if !errors.Is(err, ErrReferenceManifestAssetKeyDuplicate) {
		t.Fatalf("NewReferenceManifest() error = %v, want duplicate asset key", err)
	}
}

func TestReferenceManifestRejectsIncompleteArtifactRevision(t *testing.T) {
	entry := referenceManifestEntry("character-xiaoming", ReferenceMediaImage, "artifact-character", "", "resource-character", 1)
	_, err := NewReferenceManifest([]ReferenceManifestEntry{entry})
	if !errors.Is(err, ErrReferenceManifestInvalid) {
		t.Fatalf("NewReferenceManifest() error = %v, want invalid manifest", err)
	}
}

func referenceManifestEntry(assetKey string, mediaType ReferenceMediaType, artifactID string, revisionID string, resourceID string, ordinal int) ReferenceManifestEntry {
	return ReferenceManifestEntry{
		AssetKey: assetKey, MediaType: mediaType, SemanticRole: "reference",
		ArtifactID: artifactID, RevisionID: revisionID,
		ResourceID: resourceID, ResourceURL: "/api/resources/" + resourceID + "/file",
		SourceRevision: revisionID, Ordinal: ordinal,
	}
}

func withReferenceOrdinal(entry ReferenceManifestEntry, ordinal int) ReferenceManifestEntry {
	entry.Ordinal = ordinal
	return entry
}
