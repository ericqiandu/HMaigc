package skillcatalog

import (
	"strings"
	"testing"
)

func TestBuiltinsExposeValidatedFirstPartySkills(t *testing.T) {
	skills, err := Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 6 {
		t.Fatalf("builtin skills = %d, want 6", len(skills))
	}
	previousDir := ""
	for _, skill := range skills {
		if skill.Dir <= previousDir || len(skill.Checksum) != 64 {
			t.Fatalf("invalid first-party skill: %#v", skill)
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
	if story.Name != "故事开发" || story.Version != 1 || story.SourceKind != "adapted" || story.SourceLicense != "MIT" {
		t.Fatalf("unexpected story-development facts: %#v", story)
	}

	screenplay, exists := byDir["screenplay-writer"]
	if !exists {
		t.Fatal("missing screenplay-writer skill")
	}
	if screenplay.Name != "剧本撰写" || screenplay.Version != 1 || screenplay.SourceKind != "adapted" || screenplay.SourceLicense != "MIT" {
		t.Fatalf("unexpected screenplay-writer facts: %#v", screenplay)
	}

	storyboard, exists := byDir["storyboard-continuity-director"]
	if !exists {
		t.Fatal("missing storyboard-continuity-director skill")
	}
	if storyboard.Version != 3 || storyboard.SourceRevision != "hmaigc-v3" {
		t.Fatalf("unexpected storyboard skill facts: %#v", storyboard)
	}
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
		expectedVersion := 2
		expectedRevision := "hmaigc-v2"
		if test.dir == "short-drama-director" || test.dir == "storyboard-continuity-director" {
			expectedVersion = 3
			expectedRevision = "hmaigc-v3"
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
