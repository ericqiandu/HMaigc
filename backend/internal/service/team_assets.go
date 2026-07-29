package service

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

func (s *Service) TeamResources(userID string, teamID string, limit int) ([]model.Resource, error) {
	team, _, err := s.teamAccess(userID, teamID)
	if err != nil {
		return nil, err
	}
	resources, err := s.repo.TeamResources(team.ID, limit)
	for index := range resources {
		resources[index].PublicURL = ""
	}
	return resources, err
}

func (s *Service) UploadTeamResource(userID string, teamID string, header *multipart.FileHeader, kind string, width int, height int, durationMs int64) (*model.Resource, error) {
	if header == nil {
		return nil, BadAuthRequest("请选择要上传的文件")
	}
	entitlement, err := s.teamEntitlement(userID, teamID)
	if err != nil {
		return nil, err
	}
	if !entitlement.SharedAssetsEnabled {
		return nil, Forbidden("当前团队套餐未开通共享资产库")
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	if header.Size <= 0 || header.Size >= megabytes(policy.Resource.ResourceUploadMB) {
		return nil, BadAuthRequest(fmt.Sprintf("单个上传文件必须小于 %dMB", policy.Resource.ResourceUploadMB))
	}
	day, err := s.reserveTeamResourceQuota(userID, teamID, header.Size, entitlement.TeamStorageBytes, megabytes(policy.Resource.DailyUploadMB))
	if err != nil {
		return nil, err
	}
	file, err := header.Open()
	if err != nil {
		s.releaseTeamResourceQuota(userID, teamID, day, header.Size)
		return nil, err
	}
	defer file.Close()
	mimeType := detectUploadedMimeType(file, header.Filename, header.Header.Get("Content-Type"))
	resource, err := s.storeScopedResource(userID, teamID, kind, header.Filename, mimeType, header.Size, width, height, durationMs, file)
	if err != nil {
		s.releaseTeamResourceQuota(userID, teamID, day, header.Size)
		return nil, err
	}
	s.commitTeamResourceQuota(teamID, header.Size)
	return resource, nil
}

func (s *Service) TeamResource(userID string, teamID string, resourceID string) (*model.Resource, error) {
	team, _, err := s.teamAccess(userID, teamID)
	if err != nil {
		return nil, err
	}
	resource, err := s.repo.ResourceForTeam(team.ID, strings.TrimSpace(resourceID))
	if resource != nil {
		resource.PublicURL = ""
	}
	return resource, err
}

func (s *Service) OpenTeamResourceRange(userID string, teamID string, resourceID string, rangeHeader string) (*ResourceStream, error) {
	resource, err := s.TeamResource(userID, teamID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource.Status != model.ResourceStatusReady {
		return nil, BadAuthRequest("团队资源尚未上传完成")
	}
	if resource.Provider == "local" {
		body, err := os.Open(filepath.Join(s.dataDir, "resources", filepath.FromSlash(resource.ObjectKey)))
		if err != nil {
			return nil, err
		}
		return &ResourceStream{Resource: resource, Body: body, StatusCode: http.StatusOK, ContentLength: resource.Size, AcceptRanges: "bytes"}, nil
	}
	setting, err := s.ossSettingForResource(resource.UserID, resource)
	if err != nil {
		return nil, err
	}
	if setting.AccessKeyID == "" || setting.AccessKeySecret == "" {
		return nil, fmt.Errorf("团队资源 OSS 访问密钥不可用")
	}
	setting.Provider = firstNonEmpty(resource.Provider, setting.Provider)
	setting.Endpoint = firstNonEmpty(resource.Endpoint, setting.Endpoint)
	setting.Bucket = firstNonEmpty(resource.Bucket, setting.Bucket)
	stream, err := getOSSObjectRange(setting, resource.ObjectKey, normalizeSingleByteRange(rangeHeader))
	if err != nil {
		return nil, err
	}
	return &ResourceStream{Resource: resource, Body: stream.body, StatusCode: stream.statusCode, ContentLength: stream.contentLength, ContentRange: stream.contentRange, AcceptRanges: stream.acceptRanges}, nil
}

func (s *Service) teamEntitlement(userID string, teamID string) (*MembershipEntitlement, error) {
	team, _, err := s.teamAccess(userID, teamID)
	if err != nil {
		return nil, err
	}
	subscription, err := s.repo.ActiveTeamSubscription(team.ID, time.Now())
	if err != nil {
		return nil, &AuthError{Status: http.StatusPaymentRequired, Message: "团队会员未生效，不能使用团队商业能力"}
	}
	return membershipEntitlementFromSubscription(*subscription)
}

func (s *Service) reserveTeamResourceQuota(userID string, teamID string, size int64, teamLimit int64, dailyLimit int64) (string, error) {
	if teamLimit <= 0 {
		return "", BadAuthRequest("当前团队套餐未配置有效存储额度")
	}
	day := time.Now().UTC().Format("2006-01-02")
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	storedBytes, err := s.repo.TeamStoredResourceBytes(teamID)
	if err != nil {
		return "", err
	}
	if s.pendingTeamStorage == nil {
		s.pendingTeamStorage = map[string]int64{}
	}
	if storedBytes+s.pendingTeamStorage[teamID]+size > teamLimit {
		return "", BadAuthRequest(fmt.Sprintf("团队共享资产已达到 %s 存储额度", formatStorageLimit(teamLimit)))
	}
	s.pendingTeamStorage[teamID] += size
	if err := s.repo.ReserveDailyUpload(userID, day, size, dailyLimit); err != nil {
		s.decreasePendingTeamStorage(teamID, size)
		return "", err
	}
	return day, nil
}

func (s *Service) releaseTeamResourceQuota(userID string, teamID string, day string, size int64) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	s.decreasePendingTeamStorage(teamID, size)
	_ = s.repo.ReleaseDailyUpload(userID, day, size)
}

func (s *Service) commitTeamResourceQuota(teamID string, size int64) {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	s.decreasePendingTeamStorage(teamID, size)
}

func (s *Service) decreasePendingTeamStorage(teamID string, size int64) {
	remaining := s.pendingTeamStorage[teamID] - size
	if remaining > 0 {
		s.pendingTeamStorage[teamID] = remaining
		return
	}
	delete(s.pendingTeamStorage, teamID)
}
