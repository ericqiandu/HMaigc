package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const (
	SkillCatalogMaximumPage     = 1_000_000
	skillCatalogMaximumPageSize = 60
	skillCatalogMaximumSearch   = 160
	skillCatalogMaximumFilters  = 12
)

type SkillListRequest struct {
	Page       int
	PageSize   int
	Search     string
	Categories []string
}

type SkillList struct {
	Skills     []Skill  `json:"skills"`
	Total      int64    `json:"total"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	Categories []string `json:"categories"`
}

type SkillIntegrationCapabilities struct {
	Provider        string `json:"provider"`
	PublicCatalog   bool   `json:"publicCatalog"`
	CategoryFilter  bool   `json:"categoryFilter"`
	Versioned       bool   `json:"versioned"`
	AdminPublishing bool   `json:"adminPublishing"`
	Upload          bool   `json:"upload"`
}

func (s *Service) SkillIntegrationCapabilities() SkillIntegrationCapabilities {
	return SkillIntegrationCapabilities{Provider: "first_party", PublicCatalog: true, CategoryFilter: true, Versioned: true, AdminPublishing: false, Upload: false}
}

type Skill struct {
	Dir                string                               `json:"dir"`
	Name               string                               `json:"name"`
	Description        string                               `json:"description"`
	Icon               string                               `json:"icon"`
	CoverURL           string                               `json:"cover_url"`
	DetailText         string                               `json:"detail_text"`
	Categories         []string                             `json:"categories"`
	Version            int                                  `json:"version"`
	Checksum           string                               `json:"checksum"`
	Status             string                               `json:"status"`
	SourceKind         string                               `json:"source_kind"`
	SourceURL          string                               `json:"source_url"`
	SourceRevision     string                               `json:"source_revision"`
	SourceLicense      string                               `json:"source_license"`
	PublishedAt        string                               `json:"published_at"`
	UploaderName       string                               `json:"uploader_name"`
	Liked              bool                                 `json:"liked"`
	Activated          bool                                 `json:"activated"`
	CapabilityManifest agentruntime.SkillCapabilityManifest `json:"capability_manifest"`
}

func (s *Service) SkillsCatalog(_ context.Context, userID string, req SkillListRequest) (*SkillList, error) {
	var err error
	req, err = validateSkillListRequest(req)
	if err != nil {
		return nil, err
	}
	categoryValues, err := s.repo.PublishedSkillCategoryValues()
	if err != nil {
		return nil, err
	}
	categories, err := decodeSkillCategories(categoryValues)
	if err != nil {
		return nil, err
	}
	if err := validateSkillCategoryFilters(req.Categories, categories); err != nil {
		return nil, err
	}
	records, total, err := s.repo.PublishedSkills(repository.PublishedSkillQuery{
		Page: req.Page, PageSize: req.PageSize, Search: req.Search, Categories: req.Categories,
	})
	if err != nil {
		return nil, err
	}
	states := []model.UserSkillState(nil)
	if strings.TrimSpace(userID) != "" {
		states, err = s.repo.UserSkillStatesByDirs(userID, publishedSkillDirs(records))
		if err != nil {
			return nil, err
		}
	}
	skills, err := skillsFromPublishedRecords(records, states)
	if err != nil {
		return nil, err
	}
	return &SkillList{Skills: skills, Total: total, Page: req.Page, PageSize: req.PageSize, Categories: categories}, nil
}

func (s *Service) SkillDetail(_ context.Context, userID string, dir string) (*Skill, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, BadAuthRequest("技能标识不能为空")
	}
	record, err := s.repo.PublishedSkillByDir(dir)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("技能不存在或尚未发布")
	}
	if err != nil {
		return nil, err
	}
	states := []model.UserSkillState(nil)
	if strings.TrimSpace(userID) != "" {
		states, err = s.repo.UserSkillStatesByDirs(userID, []string{dir})
		if err != nil {
			return nil, err
		}
	}
	skills, err := skillsFromPublishedRecords([]repository.PublishedSkillRecord{*record}, states)
	if err != nil {
		return nil, err
	}
	return &skills[0], nil
}

func (s *Service) ActivatedSkills(_ context.Context, userID string) ([]Skill, error) {
	return s.userStateSkills(userID, func(state model.UserSkillState) bool { return state.Activated })
}

func (s *Service) FavoriteSkills(_ context.Context, userID string) ([]Skill, error) {
	return s.userStateSkills(userID, func(state model.UserSkillState) bool { return state.Liked })
}

func (s *Service) userStateSkills(userID string, pick func(model.UserSkillState) bool) ([]Skill, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, Unauthorized("请先登录")
	}
	states, err := s.repo.UserSkillStates(userID)
	if err != nil {
		return nil, err
	}
	selectedStates := make([]model.UserSkillState, 0, len(states))
	dirs := make([]string, 0, len(states))
	for _, state := range states {
		if pick(state) {
			selectedStates = append(selectedStates, state)
			dirs = append(dirs, state.SkillDir)
		}
	}
	records, err := s.repo.PublishedSkillsByDirs(dirs)
	if err != nil {
		return nil, err
	}
	recordByDir := make(map[string]repository.PublishedSkillRecord, len(records))
	for _, record := range records {
		recordByDir[record.Dir] = record
	}
	orderedRecords := make([]repository.PublishedSkillRecord, 0, len(selectedStates))
	for _, state := range selectedStates {
		record, exists := recordByDir[state.SkillDir]
		if !exists {
			return nil, NotFound("已保存的技能 " + state.SkillDir + " 不存在或尚未发布，请联系管理员处理")
		}
		orderedRecords = append(orderedRecords, record)
	}
	return skillsFromPublishedRecords(orderedRecords, selectedStates)
}

func (s *Service) SetSkillActivated(ctx context.Context, userID string, dir string, activated bool) (*Skill, error) {
	skill, err := s.SkillDetail(ctx, userID, dir)
	if err != nil {
		return nil, err
	}
	state, err := s.repo.SetUserSkillActivated(model.UserSkillState{ID: newID(), UserID: userID, SkillDir: skill.Dir, Activated: activated})
	if err != nil {
		return nil, err
	}
	skill.Activated = state.Activated
	skill.Liked = state.Liked
	return skill, nil
}

func (s *Service) SetSkillLiked(ctx context.Context, userID string, dir string, liked bool) (*Skill, error) {
	skill, err := s.SkillDetail(ctx, userID, dir)
	if err != nil {
		return nil, err
	}
	state, err := s.repo.SetUserSkillLiked(model.UserSkillState{ID: newID(), UserID: userID, SkillDir: skill.Dir, Liked: liked})
	if err != nil {
		return nil, err
	}
	skill.Activated = state.Activated
	skill.Liked = state.Liked
	return skill, nil
}

func validateSkillListRequest(req SkillListRequest) (SkillListRequest, error) {
	if req.Page <= 0 || req.Page > SkillCatalogMaximumPage {
		return SkillListRequest{}, BadAuthRequest("页码无效")
	}
	if req.PageSize <= 0 || req.PageSize > skillCatalogMaximumPageSize {
		return SkillListRequest{}, BadAuthRequest("每页数量无效")
	}
	req.Search = strings.TrimSpace(req.Search)
	if len(req.Search) > skillCatalogMaximumSearch {
		return SkillListRequest{}, BadAuthRequest("技能搜索内容过长")
	}
	req.Categories = cleanStringList(req.Categories)
	if len(req.Categories) > skillCatalogMaximumFilters {
		return SkillListRequest{}, BadAuthRequest("技能分类筛选过多")
	}
	for _, category := range req.Categories {
		if len(category) > 40 {
			return SkillListRequest{}, BadAuthRequest("技能分类无效")
		}
	}
	return req, nil
}

func skillsFromPublishedRecords(records []repository.PublishedSkillRecord, states []model.UserSkillState) ([]Skill, error) {
	stateByDir := make(map[string]model.UserSkillState, len(states))
	for _, state := range states {
		stateByDir[state.SkillDir] = state
	}
	result := make([]Skill, 0, len(records))
	for _, record := range records {
		categories, err := decodeSkillCategories([]string{record.CategoriesJSON})
		if err != nil {
			return nil, err
		}
		publishedAt := ""
		if record.PublishedAt != nil {
			publishedAt = record.PublishedAt.UTC().Format(time.RFC3339)
		}
		state := stateByDir[record.Dir]
		var capabilityManifest agentruntime.SkillCapabilityManifest
		if err := json.Unmarshal([]byte(record.CapabilityManifestJSON), &capabilityManifest); err != nil || agentruntime.ValidateSkillCapabilityManifest(capabilityManifest) != nil {
			return nil, errors.New("已发布 Skill Capability Manifest 无效")
		}
		result = append(result, Skill{
			Dir: record.Dir, Name: record.Name, Description: record.Description, Icon: record.Icon, CoverURL: record.CoverURL,
			DetailText: record.Instructions, Categories: categories, Version: record.Version, Checksum: record.Checksum,
			Status: string(record.Status), SourceKind: record.SourceKind, SourceURL: record.SourceURL, SourceRevision: record.SourceRevision,
			SourceLicense: record.SourceLicense, PublishedAt: publishedAt,
			UploaderName: "HMaigc", Liked: state.Liked, Activated: state.Activated,
			CapabilityManifest: capabilityManifest,
		})
	}
	return result, nil
}

func decodeSkillCategories(values []string) ([]string, error) {
	result := make([]string, 0)
	for _, value := range values {
		var categories []string
		if err := json.Unmarshal([]byte(value), &categories); err != nil {
			return nil, err
		}
		result = append(result, categories...)
	}
	result = cleanStringList(result)
	sort.Strings(result)
	return result, nil
}

func cleanStringList(values []string) []string {
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

func validateSkillCategoryFilters(requested []string, available []string) error {
	availableSet := make(map[string]struct{}, len(available))
	for _, category := range available {
		availableSet[category] = struct{}{}
	}
	for _, category := range requested {
		if _, exists := availableSet[category]; !exists {
			return BadAuthRequest("技能分类不存在: " + category)
		}
	}
	return nil
}

func publishedSkillDirs(records []repository.PublishedSkillRecord) []string {
	dirs := make([]string, 0, len(records))
	for _, record := range records {
		dirs = append(dirs, record.Dir)
	}
	return dirs
}
