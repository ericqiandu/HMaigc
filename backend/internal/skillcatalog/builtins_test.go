package skillcatalog

import (
	"reflect"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

const viMaxPinnedRevision = "05a48943878312d88fe5a016c12a9654940ecc43"

var viMaxPinnedSourceURL = "https://github.com/HKUDS/ViMax/tree/" + viMaxPinnedRevision

func expectedViMaxDerivedSkillManifests() map[string]agentruntime.SkillCapabilityManifest {
	return map[string]agentruntime.SkillCapabilityManifest{
		"camera-tree-continuity": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistStoryboard, agentruntime.SpecialistVisual},
			Tools:           []agentruntime.AgentToolName{agentruntime.ToolAssetsRead},
			ArtifactSchemas: []string{"camera_tree.v1"},
		},
		"character-visual-bible": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistAsset, agentruntime.SpecialistVisual},
			Tools:           []agentruntime.AgentToolName{agentruntime.ToolAssetsRead},
			ArtifactSchemas: []string{"character_visual_bible.v1"},
		},
		"first-motion-last-frame": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistStoryboard, agentruntime.SpecialistVideoAssembly, agentruntime.SpecialistVisual},
			Tools:           []agentruntime.AgentToolName{agentruntime.ToolAssetsRead},
			ArtifactSchemas: []string{"first_motion_last_frame.v1"},
		},
		"storyboard-cinematic-language": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistStoryboard},
			Tools:           []agentruntime.AgentToolName{},
			ArtifactSchemas: []string{"storyboard_plan.v1"},
		},
		"visual-consistency-review": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistVisual},
			Tools:           []agentruntime.AgentToolName{agentruntime.ToolAssetsRead},
			ArtifactSchemas: []string{"visual_consistency_review.v1"},
		},
		"visual-evidence-analysis": {
			Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistAsset, agentruntime.SpecialistStoryboard, agentruntime.SpecialistVideoAssembly, agentruntime.SpecialistVisual},
			Tools:           []agentruntime.AgentToolName{agentruntime.ToolAssetsRead},
			ArtifactSchemas: []string{"visual_evidence.v1"},
		},
	}
}

func TestBuiltinsExposeValidatedFirstPartySkills(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 13 {
		t.Fatalf("builtin skills = %d, want 13", len(skills))
	}
	previousDir := ""
	for _, skill := range skills {
		if skill.Dir <= previousDir || len(skill.Checksum) != 64 {
			t.Fatalf("invalid first-party skill: %#v", skill)
		}
		if skill.CapabilityManifestJSON == "" || agentruntime.ValidateSkillCapabilityManifest(skill.CapabilityManifest) != nil {
			t.Fatalf("invalid first-party skill capability manifest: %#v", skill)
		}
		if skill.SourceKind == "original" && (skill.SourceURL != "" || skill.SourceLicense != "HMaigc-Proprietary") {
			t.Fatalf("invalid original skill provenance: %#v", skill)
		}
		if skill.SourceKind == "adapted" && (skill.SourceURL == "" || skill.SourceLicense == "") {
			t.Fatalf("invalid adapted skill provenance: %#v", skill)
		}
		previousDir = skill.Dir
	}
}

func TestBuiltinsAuthorizeOnlyAtomicCapabilities(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[agentruntime.AgentToolName]struct{}{
		agentruntime.ToolCanvasRead: {}, agentruntime.ToolCanvasApplyOps: {},
		agentruntime.ToolAssetsRead: {}, agentruntime.ToolAssetsPublish: {},
		agentruntime.ToolMediaGenerate: {}, agentruntime.ToolSkillsLoad: {},
	}
	for _, skill := range skills {
		for _, tool := range skill.CapabilityManifest.Tools {
			if _, found := allowed[tool]; !found {
				t.Fatalf("skill %s authorizes retired capability %q", skill.Dir, tool)
			}
		}
	}
}

func TestBuiltinsPublishGovernedVideoPromptSpecialist(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	byDir := make(map[string]BuiltinSkill, len(skills))
	for _, skill := range skills {
		byDir[skill.Dir] = skill
	}

	skill, exists := byDir["video-prompt-specialist"]
	if !exists {
		t.Fatal("missing governed video-prompt-specialist skill")
	}
	expectedManifest := agentruntime.SkillCapabilityManifest{
		Specialists:     []agentruntime.SpecialistKey{agentruntime.SpecialistVideoAssembly},
		Tools:           []agentruntime.AgentToolName{agentruntime.ToolMediaGenerate},
		ArtifactSchemas: []string{agentruntime.ArtifactSchemaVideoPlanV1},
	}
	if skill.Version != 1 || skill.SourceKind != "original" || skill.SourceURL != "" ||
		skill.SourceRevision != "hmaigc-v1" || skill.SourceLicense != "HMaigc-Proprietary" ||
		skill.Checksum != "eb2dacb60370b62cbef45760cafbf164c5fc920358c546ee9dcb1ed73f7045c7" ||
		!reflect.DeepEqual(skill.CapabilityManifest, expectedManifest) {
		t.Fatalf("unexpected video prompt specialist facts: %#v", skill)
	}
	for _, section := range []string{"## 输入", "## 输出", "## 证据要求", "## 禁止假设", "## Revision 规则", "## 失败行为", "## 方法"} {
		if !strings.Contains(skill.Instructions, section) {
			t.Errorf("video prompt specialist missing contract section %s", section)
		}
	}
}

func TestBuiltinsPublishGovernedViMaxDerivedSkills(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	byDir := make(map[string]BuiltinSkill, len(skills))
	for _, skill := range skills {
		byDir[skill.Dir] = skill
	}

	requiredSections := []string{
		"## 输入", "## 输出", "## 证据要求", "## 禁止假设", "## Revision 规则", "## 失败行为",
	}
	for dir, expectedManifest := range expectedViMaxDerivedSkillManifests() {
		skill, exists := byDir[dir]
		if !exists {
			t.Errorf("missing governed ViMax-derived skill %s", dir)
			continue
		}
		expectedVersion := 2
		if dir == "storyboard-cinematic-language" {
			expectedVersion = 1
		}
		if skill.Version != expectedVersion || skill.SourceKind != "adapted" || skill.SourceURL != viMaxPinnedSourceURL ||
			skill.SourceRevision != viMaxPinnedRevision || skill.SourceLicense != "MIT" {
			t.Errorf("unexpected ViMax provenance for %s: %#v", dir, skill)
		}
		if len(skill.Checksum) != 64 || skill.CapabilityManifestJSON == "" ||
			!reflect.DeepEqual(skill.CapabilityManifest, expectedManifest) {
			t.Errorf("unexpected governed facts for %s: %#v", dir, skill)
		}
		for _, section := range requiredSections {
			if !strings.Contains(skill.Instructions, section) {
				t.Errorf("ViMax-derived skill %s missing contract section %s", dir, section)
			}
		}
	}
}

func TestValidateManifestSkillRequiresImmutableProvenance(t *testing.T) {
	base := manifestSkill{
		Dir: "governed-skill", Version: 1, Name: "受治理技能", Description: "用于验证不可变来源事实。",
		Categories: []string{"治理"}, Visibility: "public", SourceKind: "adapted",
		SourceURL: "https://github.com/example/upstream/tree/revision", SourceRevision: "revision", SourceLicense: "MIT",
		Changelog: "发布受治理技能", CapabilityManifest: agentruntime.SkillCapabilityManifest{
			Specialists: []agentruntime.SpecialistKey{agentruntime.SpecialistVisual}, ArtifactSchemas: []string{"visual_evidence.v1"},
		},
	}
	tests := []struct {
		name   string
		mutate func(*manifestSkill)
	}{
		{name: "adapted source revision is required", mutate: func(skill *manifestSkill) { skill.SourceRevision = " " }},
		{name: "original source URL is forbidden", mutate: func(skill *manifestSkill) {
			skill.SourceKind = "original"
			skill.SourceURL = "https://example.com/not-original"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			skill := base
			test.mutate(&skill)
			if err := validateManifestSkill(skill); err == nil {
				t.Fatal("invalid provenance must be rejected")
			}
		})
	}
}

func TestValidateManifestSkillRejectsMissingLicenseAndUndeclaredTool(t *testing.T) {
	base := manifestSkill{
		Dir: "governed-skill", Version: 1, Name: "受治理技能", Description: "用于验证来源许可和工具边界。",
		Categories: []string{"治理"}, Visibility: "public", SourceKind: "adapted",
		SourceURL: "https://github.com/example/upstream/tree/revision", SourceRevision: "revision", SourceLicense: "MIT",
		Changelog: "发布受治理技能", CapabilityManifest: agentruntime.SkillCapabilityManifest{
			Specialists: []agentruntime.SpecialistKey{agentruntime.SpecialistVisual}, ArtifactSchemas: []string{"visual_evidence.v1"},
		},
	}
	tests := []struct {
		name   string
		mutate func(*manifestSkill)
	}{
		{name: "missing license", mutate: func(skill *manifestSkill) { skill.SourceLicense = "" }},
		{name: "undeclared tool", mutate: func(skill *manifestSkill) {
			skill.CapabilityManifest.Tools = []agentruntime.AgentToolName{"undeclared.tool"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			skill := base
			test.mutate(&skill)
			if err := validateManifestSkill(skill); err == nil {
				t.Fatal("invalid governed skill must be rejected")
			}
		})
	}
}

func TestBuiltinsPublishNarrativePipelineSkills(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	byDir := make(map[string]BuiltinSkill, len(skills))
	for _, skill := range skills {
		byDir[skill.Dir] = skill
	}

	story, exists := byDir["story-development"]
	if !exists {
		t.Fatal("missing story-development skill")
	}
	if story.Name != "故事开发" || story.Version != 2 || story.SourceKind != "adapted" || story.SourceLicense != "MIT" {
		t.Fatalf("unexpected story-development facts: %#v", story)
	}

	screenplay, exists := byDir["screenplay-writer"]
	if !exists {
		t.Fatal("missing screenplay-writer skill")
	}
	if screenplay.Name != "剧本撰写" || screenplay.Version != 2 || screenplay.SourceKind != "adapted" || screenplay.SourceLicense != "MIT" {
		t.Fatalf("unexpected screenplay-writer facts: %#v", screenplay)
	}

	storyboard, exists := byDir["storyboard-continuity-director"]
	if !exists {
		t.Fatal("missing storyboard-continuity-director skill")
	}
	if storyboard.Version != 7 || storyboard.SourceRevision != "hmaigc-v7" {
		t.Fatalf("unexpected storyboard skill facts: %#v", storyboard)
	}
	requiredPlanSchemas := []string{"assembly_plan.v2", "audio_plan.v1", "video_plan.v1"}
	for _, requiredSchema := range requiredPlanSchemas {
		if !containsString(storyboard.CapabilityManifest.ArtifactSchemas, requiredSchema) {
			t.Errorf("storyboard skill missing governed plan schema %q: %#v", requiredSchema, storyboard.CapabilityManifest.ArtifactSchemas)
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestBuiltinsPublishDirectorOperatingContracts(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	byDir := make(map[string]BuiltinSkill, len(skills))
	for _, skill := range skills {
		byDir[skill.Dir] = skill
	}
	tests := []struct {
		dir      string
		sections []string
	}{
		{
			dir: "short-drama-director",
			sections: []string{
				"## 使命", "## 成功标准", "## 工作流程", "## 决策规则", "## 边界", "## 常见失败", "## 最终自检",
			},
		},
		{
			dir: "storyboard-continuity-director",
			sections: []string{
				"## 使命", "## 成功标准", "## 工作流程", "## 决策规则", "## 边界", "## 常见失败", "## 最终自检",
			},
		},
	}
	for _, test := range tests {
		skill, ok := byDir[test.dir]
		if !ok {
			t.Fatalf("missing director skill %s", test.dir)
		}
		expectedVersion := 3
		expectedRevision := "hmaigc-v3"
		if test.dir == "short-drama-director" {
			expectedVersion = 6
			expectedRevision = "hmaigc-v6"
		}
		if test.dir == "storyboard-continuity-director" {
			expectedVersion = 7
			expectedRevision = "hmaigc-v7"
		}
		if skill.Version != expectedVersion || skill.SourceRevision != expectedRevision {
			t.Fatalf("director skill %s version facts = v%d/%s, want v%d/%s", test.dir, skill.Version, skill.SourceRevision, expectedVersion, expectedRevision)
		}
		for _, section := range test.sections {
			if !strings.Contains(skill.Instructions, section) {
				t.Fatalf("director skill %s missing operating contract section %s", test.dir, section)
			}
		}
	}
}
