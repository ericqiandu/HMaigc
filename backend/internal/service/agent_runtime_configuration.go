package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type AgentRuntimeConfigurationInput struct {
	GenerationModels agentruntime.GenerationModelSelections
	SkillDirs        []string
	Attachments      []AgentRuntimeResourceInput
	ExecutionMode    agentruntime.ExecutionMode
}

type AgentRuntimeResourceInput struct {
	ResourceID string
	Name       string
}

func (s *Service) resolveAgentRuntimeConfiguration(ctx context.Context, actorUserID string, input AgentRuntimeConfigurationInput) (agentruntime.RunConfiguration, error) {
	normalized, err := normalizeAgentRuntimeConfigurationInput(input)
	if err != nil {
		return agentruntime.RunConfiguration{}, err
	}
	callableModels, err := s.agentRuntimeCallableModels(actorUserID)
	if err != nil {
		return agentruntime.RunConfiguration{}, err
	}
	if _, err := filterAgentRuntimeCallableModels(callableModels, normalized.GenerationModels); err != nil {
		return agentruntime.RunConfiguration{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	resolver := s.agentRuntimeSkillResolver
	if resolver == nil {
		resolver = func(resolveContext context.Context, userID string, dir string) (*UpdreamSkill, error) {
			return s.CommunitySkillDetail(resolveContext, userID, dir)
		}
	}
	skills := make([]agentruntime.SkillSelection, 0, len(normalized.SkillDirs))
	for _, dir := range normalized.SkillDirs {
		skill, resolveErr := resolver(ctx, actorUserID, dir)
		if resolveErr != nil {
			return agentruntime.RunConfiguration{}, resolveErr
		}
		if skill == nil || strings.TrimSpace(skill.Dir) != dir {
			return agentruntime.RunConfiguration{}, errors.New("Agent Skill 事实无效")
		}
		skills = append(skills, agentruntime.SkillSelection{
			Dir: dir, Name: strings.TrimSpace(skill.Name), Description: strings.TrimSpace(skill.Description),
			Instructions: strings.TrimSpace(skill.DetailText), Version: skill.Version,
		})
	}
	attachments := make([]agentruntime.ResourceAttachment, 0, len(normalized.Attachments))
	for _, inputAttachment := range normalized.Attachments {
		resource, resourceErr := s.repo.ResourceForUser(actorUserID, inputAttachment.ResourceID)
		if errors.Is(resourceErr, gorm.ErrRecordNotFound) {
			return agentruntime.RunConfiguration{}, Forbidden("Agent 参考图片不存在或不属于当前用户")
		}
		if resourceErr != nil {
			return agentruntime.RunConfiguration{}, resourceErr
		}
		if resource.Status != model.ResourceStatusReady || resource.Kind != "image" || !strings.HasPrefix(strings.TrimSpace(resource.MimeType), "image/") {
			return agentruntime.RunConfiguration{}, BadAuthRequest("Agent 参考图片尚不可用")
		}
		attachments = append(attachments, agentruntime.ResourceAttachment{
			ResourceID: resource.ID, Name: inputAttachment.Name, MIMEType: strings.TrimSpace(resource.MimeType), Width: resource.Width, Height: resource.Height,
		})
	}
	configuration := agentruntime.RunConfiguration{GenerationModels: normalized.GenerationModels, Skills: skills, Attachments: attachments, ExecutionMode: normalized.ExecutionMode}
	if err := agentruntime.ValidateRunConfiguration(configuration); err != nil {
		return agentruntime.RunConfiguration{}, BadAuthRequest("Agent 模型或 Skill 选择无效")
	}
	return configuration, nil
}

func normalizeAgentRuntimeConfigurationInput(input AgentRuntimeConfigurationInput) (AgentRuntimeConfigurationInput, error) {
	result := AgentRuntimeConfigurationInput{GenerationModels: cloneGenerationModelSelections(input.GenerationModels), ExecutionMode: input.ExecutionMode}
	if result.ExecutionMode != agentruntime.ExecutionGuided && result.ExecutionMode != agentruntime.ExecutionAutomatic {
		return AgentRuntimeConfigurationInput{}, BadAuthRequest("Agent 执行模式无效")
	}
	if err := validateGenerationModelSelections(result.GenerationModels); err != nil {
		return AgentRuntimeConfigurationInput{}, BadAuthRequest("Agent 模型选择无效")
	}
	seen := make(map[string]struct{}, len(input.SkillDirs))
	for _, raw := range input.SkillDirs {
		dir := strings.TrimSpace(raw)
		if dir == "" || len(dir) > 120 {
			return AgentRuntimeConfigurationInput{}, BadAuthRequest("Agent Skill 选择无效")
		}
		if _, duplicate := seen[dir]; duplicate {
			continue
		}
		seen[dir] = struct{}{}
		result.SkillDirs = append(result.SkillDirs, dir)
	}
	if len(result.SkillDirs) > 8 {
		return AgentRuntimeConfigurationInput{}, BadAuthRequest("一次最多选择 8 个 Skills")
	}
	sort.Strings(result.SkillDirs)
	if len(input.Attachments) > 4 {
		return AgentRuntimeConfigurationInput{}, BadAuthRequest("一次最多添加 4 张 Agent 参考图片")
	}
	seenResources := make(map[string]struct{}, len(input.Attachments))
	for _, attachment := range input.Attachments {
		resourceID := strings.TrimSpace(attachment.ResourceID)
		name := strings.TrimSpace(attachment.Name)
		if resourceID == "" || len(resourceID) > 80 || name == "" || len(name) > 240 {
			return AgentRuntimeConfigurationInput{}, BadAuthRequest("Agent 参考图片选择无效")
		}
		if _, duplicate := seenResources[resourceID]; duplicate {
			return AgentRuntimeConfigurationInput{}, BadAuthRequest("Agent 参考图片不能重复")
		}
		seenResources[resourceID] = struct{}{}
		result.Attachments = append(result.Attachments, AgentRuntimeResourceInput{ResourceID: resourceID, Name: name})
	}
	sort.Slice(result.Attachments, func(left, right int) bool {
		return result.Attachments[left].ResourceID < result.Attachments[right].ResourceID
	})
	return result, nil
}

func agentRuntimeConfigurationMatchesInput(configuration agentruntime.RunConfiguration, input AgentRuntimeConfigurationInput) bool {
	normalized, err := normalizeAgentRuntimeConfigurationInput(input)
	if err != nil || configuration.ExecutionMode != normalized.ExecutionMode || !sameGenerationModelSelections(configuration.GenerationModels, normalized.GenerationModels) || len(configuration.Skills) != len(normalized.SkillDirs) || len(configuration.Attachments) != len(normalized.Attachments) {
		return false
	}
	for index, dir := range normalized.SkillDirs {
		if configuration.Skills[index].Dir != dir {
			return false
		}
	}
	for index, attachment := range normalized.Attachments {
		if configuration.Attachments[index].ResourceID != attachment.ResourceID || configuration.Attachments[index].Name != attachment.Name {
			return false
		}
	}
	return true
}

func validateGenerationModelSelections(selections agentruntime.GenerationModelSelections) error {
	for _, selection := range []*agentruntime.GenerationModelSelection{selections.Image, selections.Video} {
		if selection == nil {
			continue
		}
		if strings.TrimSpace(selection.ChannelID) == "" || len(selection.ChannelID) > 80 || strings.TrimSpace(selection.Model) == "" || len(selection.Model) > 120 {
			return errors.New("invalid generation model selection")
		}
	}
	return nil
}

func filterAgentRuntimeCallableModels(models []agentRuntimeCallableModelFact, selections agentruntime.GenerationModelSelections) ([]agentRuntimeCallableModelFact, error) {
	selected := map[string]*agentruntime.GenerationModelSelection{"image": selections.Image, "video": selections.Video}
	found := map[string]bool{}
	result := make([]agentRuntimeCallableModelFact, 0, len(models))
	for _, item := range models {
		selection := selected[item.Capability]
		if selection != nil {
			if item.ChannelID != selection.ChannelID || item.Model != selection.Model {
				continue
			}
			found[item.Capability] = true
		}
		result = append(result, item)
	}
	for capability, selection := range selected {
		if selection != nil && !found[capability] {
			return nil, BadAuthRequest("所选" + map[string]string{"image": "图片", "video": "视频"}[capability] + "模型当前不可用于 Agent")
		}
	}
	return result, nil
}

func cloneGenerationModelSelections(value agentruntime.GenerationModelSelections) agentruntime.GenerationModelSelections {
	result := agentruntime.GenerationModelSelections{}
	if value.Image != nil {
		result.Image = &agentruntime.GenerationModelSelection{ChannelID: strings.TrimSpace(value.Image.ChannelID), Model: strings.TrimSpace(value.Image.Model)}
	}
	if value.Video != nil {
		result.Video = &agentruntime.GenerationModelSelection{ChannelID: strings.TrimSpace(value.Video.ChannelID), Model: strings.TrimSpace(value.Video.Model)}
	}
	return result
}

func sameGenerationModelSelections(left agentruntime.GenerationModelSelections, right agentruntime.GenerationModelSelections) bool {
	return sameGenerationModelSelection(left.Image, right.Image) && sameGenerationModelSelection(left.Video, right.Video)
}

func sameGenerationModelSelection(left *agentruntime.GenerationModelSelection, right *agentruntime.GenerationModelSelection) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.ChannelID == right.ChannelID && left.Model == right.Model
}
