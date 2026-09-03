package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AssetsSyncRequest struct {
	Assets []json.RawMessage `json:"assets"`
}

type CanvasProjectsSyncRequest struct {
	Projects []json.RawMessage `json:"projects"`
}

type UserDataSummary struct {
	ID                     string                  `json:"id"`
	Kind                   string                  `json:"kind,omitempty"`
	Category               string                  `json:"category,omitempty"`
	Status                 string                  `json:"status,omitempty"`
	Title                  string                  `json:"title"`
	PreviewURL             string                  `json:"previewUrl,omitempty"`
	TeamID                 string                  `json:"teamId,omitempty"`
	Revision               int64                   `json:"revision,omitempty"`
	DefaultTeamAccess      model.CanvasAccessLevel `json:"defaultTeamAccess,omitempty"`
	AccessLevel            model.CanvasAccessLevel `json:"accessLevel,omitempty"`
	CanEdit                bool                    `json:"canEdit,omitempty"`
	CanManage              bool                    `json:"canManage,omitempty"`
	TeamSubscriptionActive bool                    `json:"teamSubscriptionActive,omitempty"`
	CreatedAt              time.Time               `json:"createdAt"`
	UpdatedAt              time.Time               `json:"updatedAt"`
}

type CanvasProjectDeletionSummary struct {
	ID        string    `json:"id"`
	DeletedAt time.Time `json:"deletedAt"`
}

func (s *Service) UserAssetSummaries(userID string) ([]UserDataSummary, error) {
	assets, err := s.repo.AssetSummaries(userID)
	if err != nil {
		return nil, err
	}
	result := make([]UserDataSummary, 0, len(assets))
	for _, asset := range assets {
		previewURL, err := assetSummaryPreview(asset.Kind, asset.PayloadJSON)
		if err != nil {
			return nil, fmt.Errorf("读取素材 %s 的预览信息失败：%w", asset.ID, err)
		}
		result = append(result, UserDataSummary{ID: asset.ID, Kind: asset.Kind, Category: string(asset.Category), Status: string(asset.Status), Title: asset.Title, PreviewURL: previewURL, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt})
	}
	return result, nil
}

func assetSummaryPreview(kind string, payloadJSON string) (string, error) {
	var payload struct {
		CoverURL string `json:"coverUrl"`
		Data     struct {
			DataURL string `json:"dataUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", err
	}
	if coverURL := strings.TrimSpace(payload.CoverURL); coverURL != "" {
		return coverURL, nil
	}
	if strings.TrimSpace(kind) == "image" {
		return strings.TrimSpace(payload.Data.DataURL), nil
	}
	return "", nil
}

func (s *Service) UserAsset(userID string, id string) (json.RawMessage, error) {
	asset, err := s.repo.AssetForUser(userID, id)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(asset.PayloadJSON), nil
}

func (s *Service) UpsertUserAsset(userID string, raw json.RawMessage) (UserDataSummary, error) {
	asset, err := assetFromJSON(userID, raw)
	if err != nil {
		return UserDataSummary{}, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return UserDataSummary{}, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	existing, existingErr := s.repo.AssetForUser(userID, asset.ID)
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return UserDataSummary{}, existingErr
	}
	existingBytes := int64(0)
	if existing != nil {
		existingBytes = int64(len([]byte(existing.PayloadJSON)))
	}
	usage, err := s.repo.UserStorageUsage(userID)
	if err != nil {
		return UserDataSummary{}, err
	}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", errors.Is(existingErr, gorm.ErrRecordNotFound), int64(len(raw))-existingBytes, policy.Resource); err != nil {
		return UserDataSummary{}, err
	}
	if err := s.repo.UpsertAsset(&asset); err != nil {
		return UserDataSummary{}, err
	}
	if existingErr != nil {
		s.recordActivity(userID, "asset", 1)
	}
	return UserDataSummary{ID: asset.ID, Kind: asset.Kind, Category: string(asset.Category), Status: string(asset.Status), Title: asset.Title, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}, nil
}

func (s *Service) DeleteUserAsset(userID string, id string) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()

	asset, err := s.repo.AssetForUser(userID, id)
	if err != nil {
		return err
	}
	references, err := s.repo.AssetReferenceCount(id)
	if err != nil {
		return err
	}
	if references > 0 {
		return BadAuthRequest("素材仍被项目或镜头引用，请先解除引用")
	}
	resources, err := s.assetExclusiveResources(asset)
	if err != nil {
		return err
	}
	for index := range resources {
		if err := s.deleteStoredResourceObject(userID, &resources[index]); err != nil {
			return fmt.Errorf("删除素材资源 %s 失败：%w", resources[index].ID, err)
		}
	}
	resourceIDs := make([]string, 0, len(resources))
	for index := range resources {
		resourceIDs = append(resourceIDs, resources[index].ID)
	}
	return s.repo.DeleteAssetAndMarkResourcesDeleted(userID, id, resourceIDs, time.Now())
}

func (s *Service) UserAssets(userID string) ([]json.RawMessage, error) {
	assets, err := s.repo.Assets(userID)
	if err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(assets))
	for _, asset := range assets {
		if strings.TrimSpace(asset.PayloadJSON) != "" {
			result = append(result, json.RawMessage(asset.PayloadJSON))
		}
	}
	return result, nil
}

func (s *Service) ReplaceUserAssets(userID string, req AssetsSyncRequest) ([]json.RawMessage, error) {
	assets := make([]model.Asset, 0, len(req.Assets))
	var totalBytes int64
	for _, raw := range req.Assets {
		item, err := assetFromJSON(userID, raw)
		if err != nil {
			return nil, err
		}
		assets = append(assets, item)
		totalBytes += int64(len(raw))
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	usage, err := s.repo.UserStorageUsage(userID)
	if err != nil {
		return nil, err
	}
	if err := validateStructuredReplacementQuotaWithPolicy(usage, "asset", len(assets), totalBytes, policy.Resource); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceAssets(userID, assets); err != nil {
		return nil, err
	}
	if len(assets) > 0 {
		s.recordActivity(userID, "asset", len(assets))
	}
	return s.UserAssets(userID)
}

func (s *Service) UserCanvasProjects(userID string) ([]json.RawMessage, error) {
	projects, err := s.repo.CanvasProjects(userID)
	if err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, 0, len(projects))
	for _, project := range projects {
		if strings.TrimSpace(project.PayloadJSON) != "" {
			result = append(result, json.RawMessage(project.PayloadJSON))
		}
	}
	return result, nil
}

func (s *Service) UserCanvasProjectSummaries(userID string) ([]UserDataSummary, error) {
	projects, err := s.repo.CanvasProjectSummariesForActor(userID)
	if err != nil {
		return nil, err
	}
	result := make([]UserDataSummary, 0, len(projects))
	for _, project := range projects {
		access, err := canvasSummaryAccess(project)
		if err != nil {
			return nil, err
		}
		result = append(result, UserDataSummary{
			ID: project.ID, Title: project.Title, TeamID: project.TeamID, Revision: project.Revision,
			DefaultTeamAccess: project.DefaultTeamAccess, AccessLevel: access.Level,
			CanEdit: access.CanEdit, CanManage: access.CanManage,
			TeamSubscriptionActive: access.TeamSubscriptionActive,
			CreatedAt:              project.CreatedAt, UpdatedAt: project.UpdatedAt,
		})
	}
	return result, nil
}

func (s *Service) UserCanvasProjectDeletions(userID string) ([]CanvasProjectDeletionSummary, error) {
	deletions, err := s.repo.CanvasProjectDeletionsForActor(userID)
	if err != nil {
		return nil, err
	}
	result := make([]CanvasProjectDeletionSummary, 0, len(deletions))
	for _, deletion := range deletions {
		result = append(result, CanvasProjectDeletionSummary{ID: deletion.CanvasID, DeletedAt: deletion.DeletedAt})
	}
	return result, nil
}

func (s *Service) UserCanvasProject(userID string, id string) (json.RawMessage, error) {
	project, access, err := s.canvasAccess(userID, id)
	if err != nil {
		return nil, err
	}
	return canvasProjectResponse(project, access)
}

func (s *Service) CreateUserCanvasProject(userID string, raw json.RawMessage) (UserDataSummary, error) {
	project, err := canvasProjectFromJSON(userID, raw)
	if err != nil {
		return UserDataSummary{}, err
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return UserDataSummary{}, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	existing, existingErr := s.repo.CanvasProject(project.ID)
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return UserDataSummary{}, existingErr
	}
	if existing != nil {
		if existing.UserID != userID {
			return UserDataSummary{}, &AuthError{Status: 404, Message: "画布不存在"}
		}
		return UserDataSummary{}, &AuthError{Status: 409, Message: "画布已存在，内容必须通过版本化变更通道保存"}
	}
	if _, deletionErr := s.repo.CanvasProjectDeletion(project.ID); deletionErr == nil {
		return UserDataSummary{}, &AuthError{Status: http.StatusGone, Message: "画布已删除，不能使用原 ID 重新创建"}
	} else if !errors.Is(deletionErr, gorm.ErrRecordNotFound) {
		return UserDataSummary{}, deletionErr
	}
	usage, err := s.repo.UserStorageUsage(userID)
	if err != nil {
		return UserDataSummary{}, err
	}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "canvas", true, int64(len(raw)), policy.Resource); err != nil {
		return UserDataSummary{}, err
	}
	if err := s.repo.CreateCanvasProject(&project); err != nil {
		if errors.Is(err, repository.ErrCanvasProjectConflict) {
			return UserDataSummary{}, &AuthError{Status: 409, Message: "画布已存在，内容必须通过版本化变更通道保存"}
		}
		return UserDataSummary{}, err
	}
	s.recordActivity(userID, "canvas", 1)
	return UserDataSummary{
		ID: project.ID, Title: project.Title, Revision: project.Revision,
		AccessLevel: model.CanvasAccessManager, CanEdit: true, CanManage: true,
		TeamSubscriptionActive: true, CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}, nil
}

func (s *Service) DeleteUserCanvasProject(userID string, id string) error {
	project, access, err := s.canvasAccess(userID, id)
	if err != nil {
		var authErr *AuthError
		if errors.As(err, &authErr) && authErr.Status == http.StatusNotFound {
			if _, deletionErr := s.repo.CanvasProjectDeletionForActor(userID, strings.TrimSpace(id)); deletionErr == nil {
				return nil
			} else if !errors.Is(deletionErr, gorm.ErrRecordNotFound) {
				return deletionErr
			}
		}
		return err
	}
	if !access.CanManage {
		return Forbidden("当前用户不能删除该画布")
	}
	return s.repo.DeleteCanvasProjectWithCollaboration(project, userID, time.Now().UTC())
}

func (s *Service) ReplaceUserCanvasProjects(userID string, req CanvasProjectsSyncRequest) ([]json.RawMessage, error) {
	projects := make([]model.CanvasProject, 0, len(req.Projects))
	var totalBytes int64
	for _, raw := range req.Projects {
		item, err := canvasProjectFromJSON(userID, raw)
		if err != nil {
			return nil, err
		}
		projects = append(projects, item)
		totalBytes += int64(len(raw))
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	usage, err := s.repo.UserStorageUsage(userID)
	if err != nil {
		return nil, err
	}
	if err := validateStructuredReplacementQuotaWithPolicy(usage, "canvas", len(projects), totalBytes, policy.Resource); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceCanvasProjects(userID, projects); err != nil {
		return nil, err
	}
	if len(projects) > 0 {
		s.recordActivity(userID, "canvas", 1)
	}
	return s.UserCanvasProjects(userID)
}

func assetFromJSON(userID string, raw json.RawMessage) (model.Asset, error) {
	if err := validateSyncedPayload(raw, "素材"); err != nil {
		return model.Asset{}, err
	}
	var payload struct {
		ID               string `json:"id"`
		Kind             string `json:"kind"`
		Category         string `json:"category"`
		Status           string `json:"status"`
		PrimaryVersionID string `json:"primaryVersionId"`
		Title            string `json:"title"`
		CreatedAt        string `json:"createdAt"`
		UpdatedAt        string `json:"updatedAt"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return model.Asset{}, BadAuthRequest("素材数据格式错误")
	}
	now := time.Now()
	createdAt := parseClientTime(payload.CreatedAt, now)
	updatedAt := parseClientTime(payload.UpdatedAt, createdAt)
	id := strings.TrimSpace(payload.ID)
	if id == "" {
		id = newID()
	}
	category := model.AssetCategory(strings.TrimSpace(payload.Category))
	if category == "" {
		category = model.AssetCategoryOther
	}
	status := model.AssetVersionStatus(strings.TrimSpace(payload.Status))
	if status == "" {
		status = model.AssetVersionStatusConfirmed
	}
	return model.Asset{
		ID:               id,
		UserID:           userID,
		Kind:             strings.TrimSpace(payload.Kind),
		Category:         category,
		Status:           status,
		PrimaryVersionID: strings.TrimSpace(payload.PrimaryVersionID),
		Title:            strings.TrimSpace(payload.Title),
		PayloadJSON:      string(raw),
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

func canvasProjectFromJSON(userID string, raw json.RawMessage) (model.CanvasProject, error) {
	if err := validateSyncedPayload(raw, "画布"); err != nil {
		return model.CanvasProject{}, err
	}
	var payload struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		ProjectID string `json:"projectId"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return model.CanvasProject{}, BadAuthRequest("画布数据格式错误")
	}
	now := time.Now()
	createdAt := parseClientTime(payload.CreatedAt, now)
	updatedAt := parseClientTime(payload.UpdatedAt, createdAt)
	id := strings.TrimSpace(payload.ID)
	if id == "" {
		id = newID()
	}
	return model.CanvasProject{
		ID:          id,
		UserID:      userID,
		ProjectID:   strings.TrimSpace(payload.ProjectID),
		Title:       strings.TrimSpace(payload.Title),
		PayloadJSON: string(raw),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func canvasSummaryAccess(project repository.CanvasProjectSummaryRecord) (CanvasAccessView, error) {
	if project.TeamID == "" {
		return CanvasAccessView{
			Level: model.CanvasAccessManager, CanEdit: true, CanManage: true,
			TeamSubscriptionActive: true,
		}, nil
	}
	level := project.DefaultTeamAccess
	if canManageTeam(project.CurrentTeamRole) {
		level = model.CanvasAccessManager
	} else if project.OverrideAccess != "" {
		level = project.OverrideAccess
	}
	if level != model.CanvasAccessViewer && level != model.CanvasAccessEditor && level != model.CanvasAccessManager {
		return CanvasAccessView{}, errors.New("团队画布权限配置无效")
	}
	canManage := level == model.CanvasAccessManager
	return CanvasAccessView{
		Level: level, TeamID: project.TeamID, TeamRole: project.CurrentTeamRole,
		CanEdit:   project.SubscriptionActive && (level == model.CanvasAccessEditor || canManage),
		CanManage: canManage, TeamSubscriptionActive: project.SubscriptionActive,
	}, nil
}

func validateSyncedPayload(raw json.RawMessage, label string) error {
	if len(raw) > 4<<20 {
		return BadAuthRequest(label + "数据超过 4MB，请先把媒体文件保存到资源存储")
	}
	var payload interface{}
	if err := json.Unmarshal(raw, &payload); err == nil && containsInlineMediaDataURL(payload) {
		return BadAuthRequest(label + "数据包含内嵌媒体，请先上传到资源存储")
	}
	return nil
}

// 同步数据只禁止作为字段值存在的媒体 Data URL；提示词和上游错误文案可能合法提到相同字符串。
func containsInlineMediaDataURL(value interface{}) bool {
	switch item := value.(type) {
	case string:
		text := strings.ToLower(strings.TrimSpace(item))
		return strings.HasPrefix(text, "data:image/") || strings.HasPrefix(text, "data:video/") || strings.HasPrefix(text, "data:audio/")
	case []interface{}:
		for _, child := range item {
			if containsInlineMediaDataURL(child) {
				return true
			}
		}
	case map[string]interface{}:
		for _, child := range item {
			if containsInlineMediaDataURL(child) {
				return true
			}
		}
	}
	return false
}

func parseClientTime(value string, fallback time.Time) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	return fallback
}
