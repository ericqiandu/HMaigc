package skillcatalog

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.json catalog/*/SKILL.md
var builtinFiles embed.FS

type catalogManifest struct {
	Skills []manifestSkill `json:"skills"`
}

type manifestSkill struct {
	Dir                string                               `json:"dir"`
	Version            int                                  `json:"version"`
	Name               string                               `json:"name"`
	Description        string                               `json:"description"`
	Icon               string                               `json:"icon"`
	CoverURL           string                               `json:"coverUrl"`
	Categories         []string                             `json:"categories"`
	Visibility         string                               `json:"visibility"`
	SourceKind         string                               `json:"sourceKind"`
	SourceURL          string                               `json:"sourceUrl"`
	SourceRevision     string                               `json:"sourceRevision"`
	SourceLicense      string                               `json:"sourceLicense"`
	Changelog          string                               `json:"changelog"`
	CapabilityManifest agentruntime.SkillCapabilityManifest `json:"capabilityManifest"`
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type BuiltinSkill struct {
	Dir                    string
	Version                int
	Name                   string
	Description            string
	Icon                   string
	CoverURL               string
	Categories             []string
	Visibility             string
	SourceKind             string
	SourceURL              string
	SourceRevision         string
	SourceLicense          string
	Changelog              string
	Instructions           string
	Checksum               string
	CapabilityManifest     agentruntime.SkillCapabilityManifest
	CapabilityManifestJSON string
}

func Builtins() ([]BuiltinSkill, error) {
	rawManifest, err := fs.ReadFile(builtinFiles, "catalog.json")
	if err != nil {
		return nil, fmt.Errorf("读取第一方技能目录失败: %w", err)
	}
	var manifest catalogManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return nil, fmt.Errorf("解析第一方技能目录失败: %w", err)
	}
	result := make([]BuiltinSkill, 0, len(manifest.Skills))
	seen := make(map[string]struct{}, len(manifest.Skills))
	for _, item := range manifest.Skills {
		item.Dir = strings.TrimSpace(item.Dir)
		if err := validateManifestSkill(item); err != nil {
			return nil, fmt.Errorf("第一方技能目录事实无效: dir=%q version=%d: %w", item.Dir, item.Version, err)
		}
		if _, duplicate := seen[item.Dir]; duplicate {
			return nil, fmt.Errorf("第一方技能标识重复: %s", item.Dir)
		}
		seen[item.Dir] = struct{}{}
		document, err := fs.ReadFile(builtinFiles, "catalog/"+item.Dir+"/SKILL.md")
		if err != nil {
			return nil, fmt.Errorf("读取第一方技能 %s 失败: %w", item.Dir, err)
		}
		frontmatter, instructions, err := parseSkillDocument(document)
		if err != nil {
			return nil, fmt.Errorf("解析第一方技能 %s 失败: %w", item.Dir, err)
		}
		if frontmatter.Name != item.Name || frontmatter.Description != item.Description {
			return nil, fmt.Errorf("第一方技能 %s 的目录与 SKILL.md 元数据不一致", item.Dir)
		}
		sum := sha256.Sum256([]byte(instructions))
		capabilityManifestJSON, err := json.Marshal(item.CapabilityManifest)
		if err != nil {
			return nil, fmt.Errorf("序列化第一方技能 %s Capability Manifest 失败: %w", item.Dir, err)
		}
		result = append(result, BuiltinSkill{
			Dir: item.Dir, Version: item.Version, Name: item.Name, Description: item.Description,
			Icon: strings.TrimSpace(item.Icon), CoverURL: strings.TrimSpace(item.CoverURL), Categories: cleanStrings(item.Categories),
			Visibility: item.Visibility, SourceKind: strings.TrimSpace(item.SourceKind), SourceURL: strings.TrimSpace(item.SourceURL),
			SourceRevision: strings.TrimSpace(item.SourceRevision), SourceLicense: strings.TrimSpace(item.SourceLicense),
			Changelog: strings.TrimSpace(item.Changelog), Instructions: instructions, Checksum: hex.EncodeToString(sum[:]),
			CapabilityManifest: item.CapabilityManifest, CapabilityManifestJSON: string(capabilityManifestJSON),
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Dir < result[right].Dir })
	return result, nil
}

func validateManifestSkill(item manifestSkill) error {
	if !isSkillDir(item.Dir) || item.Version <= 0 || item.Visibility != "public" {
		return fmt.Errorf("技能标识、版本或可见性无效")
	}
	if strings.TrimSpace(item.Name) == "" || len(item.Name) > 160 || strings.TrimSpace(item.Description) == "" || len(item.Description) > 500 {
		return fmt.Errorf("技能名称或描述无效")
	}
	if len(item.Icon) > 80 || len(item.CoverURL) > 1000 || len(item.SourceURL) > 1000 || strings.TrimSpace(item.SourceRevision) == "" || len(item.SourceRevision) > 160 ||
		strings.TrimSpace(item.SourceLicense) == "" || len(item.SourceLicense) > 80 || strings.TrimSpace(item.Changelog) == "" || len(item.Changelog) > 500 {
		return fmt.Errorf("技能来源或展示元数据无效")
	}
	if item.SourceKind != "original" && item.SourceKind != "adapted" {
		return fmt.Errorf("技能来源类型无效")
	}
	if item.SourceKind == "adapted" && strings.TrimSpace(item.SourceURL) == "" {
		return fmt.Errorf("改编技能缺少来源地址")
	}
	if item.SourceKind == "original" && strings.TrimSpace(item.SourceURL) != "" {
		return fmt.Errorf("原创技能不得声明上游来源地址")
	}
	if err := agentruntime.ValidateSkillCapabilityManifest(item.CapabilityManifest); err != nil {
		return fmt.Errorf("技能 Capability Manifest 无效: %w", err)
	}
	categories := cleanStrings(item.Categories)
	if len(categories) == 0 || len(categories) > 12 {
		return fmt.Errorf("技能分类无效")
	}
	for _, category := range categories {
		if len(category) > 40 {
			return fmt.Errorf("技能分类过长")
		}
	}
	return nil
}

func isSkillDir(value string) bool {
	if value == "" || len(value) > 120 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if character == '-' || (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') {
			continue
		}
		return false
	}
	return true
}

func parseSkillDocument(document []byte) (skillFrontmatter, string, error) {
	raw := strings.ReplaceAll(string(document), "\r\n", "\n")
	parts := strings.SplitN(raw, "---\n", 3)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		return skillFrontmatter{}, "", fmt.Errorf("缺少标准 YAML frontmatter")
	}
	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(parts[1]), &frontmatter); err != nil {
		return skillFrontmatter{}, "", err
	}
	frontmatter.Name = strings.TrimSpace(frontmatter.Name)
	frontmatter.Description = strings.TrimSpace(frontmatter.Description)
	instructions := strings.TrimSpace(parts[2])
	if frontmatter.Name == "" || frontmatter.Description == "" || instructions == "" {
		return skillFrontmatter{}, "", fmt.Errorf("name、description 与技能正文均不能为空")
	}
	return frontmatter, instructions, nil
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
