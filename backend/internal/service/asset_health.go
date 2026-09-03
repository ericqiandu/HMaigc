package service

import (
	"context"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/model"
)

type AssetHealthStatus string

const (
	AssetHealthAvailable     AssetHealthStatus = "available"
	AssetHealthMissing       AssetHealthStatus = "missing"
	AssetHealthUnverified    AssetHealthStatus = "unverified"
	AssetHealthNotApplicable AssetHealthStatus = "not_applicable"
)

type UserAssetHealth struct {
	ID        string            `json:"id"`
	Status    AssetHealthStatus `json:"status"`
	Reason    string            `json:"reason,omitempty"`
	CheckedAt time.Time         `json:"checkedAt"`
}

type resourceHealthFact struct {
	status AssetHealthStatus
	reason string
}

type resourceHealthCheck struct {
	resource   model.Resource
	ossSetting *ossSettingValue
}

type assetHealthInput struct {
	asset       model.Asset
	resourceIDs []string
	parseErr    error
}

func (s *Service) UserAssetHealth(ctx context.Context, userID string) ([]UserAssetHealth, error) {
	assets, err := s.repo.AssetSummaries(userID)
	if err != nil {
		return nil, err
	}
	inputs := make([]assetHealthInput, 0, len(assets))
	allResourceIDs := make(map[string]struct{})
	for _, asset := range assets {
		resourceIDs, parseErr := structuredPayloadResourceIDs(asset.PayloadJSON)
		orderedIDs := make([]string, 0, len(resourceIDs))
		for resourceID := range resourceIDs {
			orderedIDs = append(orderedIDs, resourceID)
			allResourceIDs[resourceID] = struct{}{}
		}
		sort.Strings(orderedIDs)
		inputs = append(inputs, assetHealthInput{asset: asset, resourceIDs: orderedIDs, parseErr: parseErr})
	}

	resourceIDs := make([]string, 0, len(allResourceIDs))
	for resourceID := range allResourceIDs {
		resourceIDs = append(resourceIDs, resourceID)
	}
	resources, err := s.repo.ResourcesForUserByIDs(userID, resourceIDs)
	if err != nil {
		return nil, err
	}
	resourceByID := make(map[string]model.Resource, len(resources))
	for _, resource := range resources {
		resourceByID[resource.ID] = resource
	}

	checkedAt := time.Now().UTC()
	facts, err := s.userResourceHealthFacts(ctx, userID, resourceIDs, resourceByID)
	if err != nil {
		return nil, err
	}

	result := make([]UserAssetHealth, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, userAssetHealthFromFacts(input, facts, checkedAt))
	}
	return result, nil
}

func (s *Service) userResourceHealthFacts(ctx context.Context, userID string, resourceIDs []string, resourceByID map[string]model.Resource) (map[string]resourceHealthFact, error) {
	facts := make(map[string]resourceHealthFact, len(resourceIDs))
	checks := make([]resourceHealthCheck, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resource, exists := resourceByID[resourceID]
		if !exists {
			facts[resourceID] = resourceHealthFact{status: AssetHealthMissing, reason: "素材引用的资源记录不存在"}
			continue
		}
		check, immediateFact := s.prepareUserResourceHealthCheck(userID, resource)
		if immediateFact != nil {
			facts[resourceID] = *immediateFact
			continue
		}
		checks = append(checks, *check)
	}
	if len(checks) == 0 {
		return facts, nil
	}
	workerCount := min(len(checks), 8)
	jobs := make(chan resourceHealthCheck)
	var workers sync.WaitGroup
	var factsLock sync.Mutex
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for check := range jobs {
				fact := s.runResourceHealthCheck(ctx, check)
				factsLock.Lock()
				facts[check.resource.ID] = fact
				factsLock.Unlock()
			}
		}()
	}
	for _, check := range checks {
		select {
		case jobs <- check:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func (s *Service) prepareUserResourceHealthCheck(userID string, resource model.Resource) (*resourceHealthCheck, *resourceHealthFact) {
	if resource.Status == model.ResourceStatusDeleted {
		fact := resourceHealthFact{status: AssetHealthMissing, reason: "素材资源已删除"}
		return nil, &fact
	}
	if resource.Status != model.ResourceStatusReady {
		fact := resourceHealthFact{status: AssetHealthUnverified, reason: "素材资源尚未处于可用状态"}
		return nil, &fact
	}
	switch resource.Provider {
	case "local":
		return &resourceHealthCheck{resource: resource}, nil
	case "aliyun":
		setting, err := s.ossSettingForResource(userID, &resource)
		if err != nil {
			log.Printf("event=asset_health_oss_setting_unavailable user_id=%s resource_id=%s error=%q", userID, resource.ID, err.Error())
			fact := resourceHealthFact{status: AssetHealthUnverified, reason: "OSS 配置不可用，无法核验"}
			return nil, &fact
		}
		setting.Provider = firstNonEmpty(resource.Provider, setting.Provider)
		setting.Endpoint = firstNonEmpty(resource.Endpoint, setting.Endpoint)
		setting.Bucket = firstNonEmpty(resource.Bucket, setting.Bucket)
		return &resourceHealthCheck{resource: resource, ossSetting: &setting}, nil
	default:
		fact := resourceHealthFact{status: AssetHealthUnverified, reason: "素材存储渠道不受支持"}
		return nil, &fact
	}
}

func (s *Service) runResourceHealthCheck(ctx context.Context, check resourceHealthCheck) resourceHealthFact {
	switch check.resource.Provider {
	case "local":
		filePath, err := s.localResourcePath(check.resource.ObjectKey)
		if err != nil {
			log.Printf("event=asset_health_local_path_invalid resource_id=%s error=%q", check.resource.ID, err.Error())
			return resourceHealthFact{status: AssetHealthUnverified, reason: "本地资源路径无法核验"}
		}
		if _, err := os.Stat(filePath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return resourceHealthFact{status: AssetHealthMissing, reason: "素材文件不存在"}
			}
			log.Printf("event=asset_health_local_check_failed resource_id=%s error=%q", check.resource.ID, err.Error())
			return resourceHealthFact{status: AssetHealthUnverified, reason: "本地资源检查失败"}
		}
		return resourceHealthFact{status: AssetHealthAvailable}
	case "aliyun":
		if check.ossSetting == nil {
			return resourceHealthFact{status: AssetHealthUnverified, reason: "OSS 配置核验结果缺失"}
		}
		exists, err := ossObjectExists(ctx, *check.ossSetting, check.resource.ObjectKey)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("event=asset_health_oss_check_failed resource_id=%s error=%q", check.resource.ID, err.Error())
			}
			return resourceHealthFact{status: AssetHealthUnverified, reason: "OSS 资源检查失败，请稍后重试"}
		}
		if !exists {
			return resourceHealthFact{status: AssetHealthMissing, reason: "OSS 素材对象不存在"}
		}
		return resourceHealthFact{status: AssetHealthAvailable}
	default:
		return resourceHealthFact{status: AssetHealthUnverified, reason: "素材存储渠道不受支持"}
	}
}

func userAssetHealthFromFacts(input assetHealthInput, facts map[string]resourceHealthFact, checkedAt time.Time) UserAssetHealth {
	result := UserAssetHealth{ID: input.asset.ID, CheckedAt: checkedAt}
	if input.parseErr != nil {
		result.Status = AssetHealthUnverified
		result.Reason = "素材数据损坏，无法核验资源"
		return result
	}
	if len(input.resourceIDs) == 0 {
		if kind := strings.TrimSpace(input.asset.Kind); kind == "text" || kind == "entity" {
			result.Status = AssetHealthNotApplicable
			return result
		}
		result.Status = AssetHealthUnverified
		result.Reason = "素材未引用平台托管资源"
		return result
	}

	result.Status = AssetHealthAvailable
	for _, resourceID := range input.resourceIDs {
		fact, exists := facts[resourceID]
		if !exists {
			result.Status = AssetHealthUnverified
			result.Reason = "素材资源核验结果缺失"
			return result
		}
		if fact.status == AssetHealthMissing {
			result.Status = fact.status
			result.Reason = fact.reason
			return result
		}
		if fact.status == AssetHealthUnverified && result.Status == AssetHealthAvailable {
			result.Status = fact.status
			result.Reason = fact.reason
		}
	}
	return result
}
