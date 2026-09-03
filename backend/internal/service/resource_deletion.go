package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (s *Service) assetExclusiveResources(asset *model.Asset) ([]model.Resource, error) {
	resourceIDs, err := structuredPayloadResourceIDs(asset.PayloadJSON)
	if err != nil {
		return nil, fmt.Errorf("素材 %s 数据损坏，无法安全删除关联资源：%w", asset.ID, err)
	}
	representationResourceIDs, err := s.repo.AssetRepresentationResourceIDs(asset.ID)
	if err != nil {
		return nil, err
	}
	for _, resourceID := range representationResourceIDs {
		resourceIDs[resourceID] = struct{}{}
	}

	orderedIDs := make([]string, 0, len(resourceIDs))
	for resourceID := range resourceIDs {
		orderedIDs = append(orderedIDs, resourceID)
	}
	sort.Strings(orderedIDs)

	resources := make([]model.Resource, 0, len(orderedIDs))
	for _, resourceID := range orderedIDs {
		resource, err := s.repo.ResourceForUser(asset.UserID, resourceID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existingResource, lookupErr := s.repo.Resource(resourceID)
			switch {
			case errors.Is(lookupErr, gorm.ErrRecordNotFound):
				log.Printf("asset deletion detected missing resource reference asset=%s resource=%s", asset.ID, resourceID)
				continue
			case lookupErr != nil:
				return nil, lookupErr
			case existingResource.UserID != asset.UserID:
				return nil, BadAuthRequest("素材包含不属于当前账号的资源引用，无法删除")
			default:
				return nil, fmt.Errorf("素材 %s 的资源归属校验失败：%s", asset.ID, resourceID)
			}
		}
		if err != nil {
			return nil, err
		}
		if resource.TeamID != "" || resource.Status == model.ResourceStatusDeleted {
			continue
		}
		activeMigration, err := s.repo.ResourceHasActiveStorageMigration(resource.ID)
		if err != nil {
			return nil, err
		}
		if activeMigration {
			return nil, BadAuthRequest("素材资源正在迁移到 OSS，请等待迁移完成后再删除")
		}
		referenced, err := s.resourceReferencedOutsideAsset(resource.ID, asset.ID)
		if err != nil {
			return nil, err
		}
		if referenced {
			log.Printf("asset deletion retained referenced resource asset=%s resource=%s", asset.ID, resource.ID)
			continue
		}
		resources = append(resources, *resource)
	}
	return resources, nil
}

func (s *Service) resourceReferencedOutsideAsset(resourceID string, assetID string) (bool, error) {
	referenced, err := s.repo.ResourceHasExplicitReferenceOutsideAsset(resourceID, assetID)
	if err != nil || referenced {
		return referenced, err
	}
	documents, err := s.repo.PotentialResourcePayloadDocuments(resourceID, assetID)
	if err != nil {
		return false, err
	}
	for _, document := range documents {
		resourceIDs, parseErr := structuredPayloadResourceIDs(document.PayloadJSON)
		if parseErr != nil {
			return false, fmt.Errorf(
				"%s %s 数据损坏，无法确认资源 %s 是否仍被引用：%w",
				document.Kind,
				document.ID,
				resourceID,
				parseErr,
			)
		}
		if _, exists := resourceIDs[resourceID]; exists {
			return true, nil
		}
	}
	return false, nil
}

func structuredPayloadResourceIDs(payloadJSON string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	payloadJSON = strings.TrimSpace(payloadJSON)
	if payloadJSON == "" {
		return result, nil
	}
	if err := collectStructuredResourceIDs(json.RawMessage(payloadJSON), result); err != nil {
		return nil, err
	}
	return result, nil
}

func collectStructuredResourceIDs(raw json.RawMessage, result map[string]struct{}) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	switch raw[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		for key, child := range object {
			if isStructuredResourceKey(key) {
				var value string
				if err := json.Unmarshal(child, &value); err == nil {
					if resourceID := canvasResourceID(value); resourceID != "" {
						result[resourceID] = struct{}{}
					}
				}
			}
			if err := collectStructuredResourceIDs(child, result); err != nil {
				return err
			}
		}
	case '[':
		var array []json.RawMessage
		if err := json.Unmarshal(raw, &array); err != nil {
			return err
		}
		for _, child := range array {
			if err := collectStructuredResourceIDs(child, result); err != nil {
				return err
			}
		}
	default:
		var scalar json.RawMessage
		if err := json.Unmarshal(raw, &scalar); err != nil {
			return err
		}
	}
	return nil
}

func isStructuredResourceKey(key string) bool {
	return key == "storageKey" || key == "resourceKey" || strings.HasSuffix(key, "StorageKey")
}

func (s *Service) deleteStoredResourceObject(userID string, resource *model.Resource) error {
	switch resource.Provider {
	case "local":
		filePath, err := s.localResourcePath(resource.ObjectKey)
		if err != nil {
			return err
		}
		if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("删除本地资源文件失败：%w", err)
		}
		return nil
	case "aliyun":
		setting, err := s.ossSettingForResource(userID, resource)
		if err != nil {
			return err
		}
		setting.Provider = firstNonEmpty(resource.Provider, setting.Provider)
		setting.Endpoint = firstNonEmpty(resource.Endpoint, setting.Endpoint)
		setting.Bucket = firstNonEmpty(resource.Bucket, setting.Bucket)
		return deleteOSSObject(setting, resource.ObjectKey)
	default:
		return fmt.Errorf("不支持删除资源存储渠道 %q", resource.Provider)
	}
}

func deleteOSSObject(setting ossSettingValue, objectKey string) error {
	req, err := newOSSRequest(http.MethodDelete, setting, objectKey, "", nil)
	if err != nil {
		return err
	}
	resp, err := OutboundHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("OSS 对象删除失败：%s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}
