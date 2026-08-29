package agentruntime

import (
	"errors"
	"testing"
)

func TestValidateSpecialistRequestRejectsDifferentParentModel(t *testing.T) {
	request := validSpecialistRequestFixture()
	request.ParentModelRecordID = "other-record"

	err := ValidateSpecialistRequest(request, "parent-record", "deepseek-v4")
	if !errors.Is(err, ErrSpecialistModelInheritance) {
		t.Fatalf("ValidateSpecialistRequest() error = %v, want %v", err, ErrSpecialistModelInheritance)
	}
}

func TestValidateSpecialistRequestRejectsSkillCapabilityMismatch(t *testing.T) {
	request := validSpecialistRequestFixture()
	request.LoadedSkills[0].CapabilityManifest.Specialists = []SpecialistKey{SpecialistNarrative}

	err := ValidateSpecialistRequest(request, "parent-record", "deepseek-v4")
	if !errors.Is(err, ErrSkillCapabilityMismatch) {
		t.Fatalf("ValidateSpecialistRequest() error = %v, want %v", err, ErrSkillCapabilityMismatch)
	}
}

func TestValidateSpecialistRequestRejectsToolOutsideFrozenSkillCapabilities(t *testing.T) {
	request := validSpecialistRequestFixture()
	request.ToolAllowlist = []AgentToolName{ToolMediaGenerate}

	err := ValidateSpecialistRequest(request, "parent-record", "deepseek-v4")
	if !errors.Is(err, ErrSkillCapabilityMismatch) {
		t.Fatalf("ValidateSpecialistRequest() error = %v, want %v", err, ErrSkillCapabilityMismatch)
	}
}

func TestValidateSkillCapabilityManifestRequiresCanonicalFacts(t *testing.T) {
	manifest := SkillCapabilityManifest{
		Specialists:     []SpecialistKey{SpecialistVisual, SpecialistVisual},
		Tools:           []AgentToolName{ToolVisionAnalyze},
		ArtifactSchemas: []string{"visual_evidence.v1"},
	}

	if err := ValidateSkillCapabilityManifest(manifest); !errors.Is(err, ErrSkillCapabilityManifestInvalid) {
		t.Fatalf("ValidateSkillCapabilityManifest() error = %v, want %v", err, ErrSkillCapabilityManifestInvalid)
	}
}

func TestValidateSkillCapabilityManifestAcceptsCurrentAssemblyTool(t *testing.T) {
	manifest := SkillCapabilityManifest{
		Specialists:     []SpecialistKey{SpecialistVideoAssembly},
		Tools:           []AgentToolName{ToolMediaAssemble},
		ArtifactSchemas: []string{"assembly_plan.v2"},
	}

	if err := ValidateSkillCapabilityManifest(manifest); err != nil {
		t.Fatalf("ValidateSkillCapabilityManifest() error = %v", err)
	}
}

func TestValidateRunConfigurationRejectsAttachmentTotalSizeLimit(t *testing.T) {
	configuration := RunConfiguration{
		ExecutionMode: ExecutionGuided,
		Attachments: []ResourceAttachment{
			{ResourceID: "audio-1", Name: "对白.wav", Kind: "audio", MIMEType: "audio/wav", SizeBytes: 3 << 30, DurationMS: 60_000},
			{ResourceID: "video-1", Name: "样片.mp4", Kind: "video", MIMEType: "video/mp4", SizeBytes: 2 << 30, Width: 1920, Height: 1080, DurationMS: 60_000},
		},
	}

	if err := ValidateRunConfiguration(configuration); err == nil {
		t.Fatal("attachments above the frozen total size limit must fail")
	}
}

func validSpecialistRequestFixture() SpecialistRequest {
	return SpecialistRequest{
		SpecialistRunID:     "specialist-run-1",
		StageID:             "stage-1",
		SpecialistKey:       SpecialistVisual,
		SpecialistVersion:   1,
		ParentModelRecordID: "parent-record",
		ParentModelKey:      "deepseek-v4",
		Objective:           "分析已冻结的视觉资产并产出结构化证据",
		LoadedSkills: []SkillSelection{{
			Dir:          "visual-evidence-analysis",
			Name:         "视觉证据分析",
			Description:  "读取真实视觉资产并返回结构化证据",
			Instructions: "只基于真实视觉输入形成可追溯证据。",
			Version:      1,
			Checksum:     "f96f5010f4ae35a614425b182b060ae328eb572b16e98fde4c36af1dc816e857",
			CapabilityManifest: SkillCapabilityManifest{
				Specialists:     []SpecialistKey{SpecialistVisual},
				Tools:           []AgentToolName{ToolVisionAnalyze},
				ArtifactSchemas: []string{"visual_evidence.v1"},
			},
			SourceKind: "original", SourceRevision: "test-v1", SourceLicense: "HMaigc-Proprietary", PublishedAt: "2026-08-27T00:00:00Z",
		}},
		ToolAllowlist:        []AgentToolName{ToolVisionAnalyze},
		ExpectedOutputSchema: "visual_evidence.v1",
		ExpectedDelivery:     ExpectedDelivery{Kind: DeliveryAnswer, CompletionCriteria: []DeliveryCriterion{{Fact: DeliveryFactFinalMessage}}},
	}
}
