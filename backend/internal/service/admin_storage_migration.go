package service

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

const storageMigrationSnapshotBatchSize = 500

type AdminStorageMigrationOverview struct {
	Eligible repository.LocalResourceMigrationStats `json:"eligible"`
	Active   *model.StorageMigrationJob             `json:"active,omitempty"`
	Latest   *model.StorageMigrationJob             `json:"latest,omitempty"`
	Items    []model.StorageMigrationItem           `json:"items"`
}

type StartStorageMigrationRequest struct {
	Confirmation string `json:"confirmation"`
}

type storageMigrationStartAudit struct {
	Items  int64  `json:"items"`
	Bytes  int64  `json:"bytes"`
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
}

func (s *Service) AdminStorageMigrationOverview(actor *model.User) (*AdminStorageMigrationOverview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	eligible, err := s.repo.LocalResourceMigrationStats()
	if err != nil {
		return nil, err
	}
	active, err := s.repo.ActiveStorageMigrationJob()
	if err != nil {
		return nil, err
	}
	latest, err := s.repo.LatestStorageMigrationJob()
	if err != nil {
		return nil, err
	}
	items := make([]model.StorageMigrationItem, 0)
	if latest != nil {
		items, err = s.repo.StorageMigrationItems(latest.ID, 100)
		if err != nil {
			return nil, err
		}
	}
	return &AdminStorageMigrationOverview{Eligible: eligible, Active: active, Latest: latest, Items: items}, nil
}

// StartStorageMigration 创建截至当前时间的本地资源快照。后续新资源会直接使用已启用的 OSS，不属于本次迁移。
func (s *Service) StartStorageMigration(actor *model.User, request StartStorageMigrationRequest) (*model.StorageMigrationJob, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if strings.TrimSpace(request.Confirmation) != "MIGRATE LOCAL TO OSS" {
		return nil, BadAuthRequest("确认短语不正确")
	}

	s.storageMigrationMu.Lock()
	defer s.storageMigrationMu.Unlock()

	setting, err := s.activeOSSSetting()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	job := &model.StorageMigrationJob{
		ID:             newID(),
		Status:         model.StorageMigrationPreparing,
		RequestedBy:    actor.ID,
		TargetProvider: setting.Provider,
		TargetEndpoint: setting.Endpoint,
		TargetBucket:   setting.Bucket,
		TargetPrefix:   setting.PathPrefix,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateStorageMigrationJobIfIdle(job); err != nil {
		if errors.Is(err, repository.ErrStorageMigrationAlreadyActive) {
			return nil, BadAuthRequest("已有存储迁移任务正在执行")
		}
		return nil, err
	}

	totalItems, totalBytes, snapshotErr := s.snapshotLocalResourcesForMigration(job, setting)
	if snapshotErr != nil {
		_ = s.repo.FailPreparingStorageMigration(job.ID, snapshotErr.Error(), time.Now())
		return nil, snapshotErr
	}
	if totalItems == 0 {
		message := "没有可迁移的本地资源"
		_ = s.repo.FailPreparingStorageMigration(job.ID, message, time.Now())
		return nil, BadAuthRequest(message)
	}
	if err := s.appendAdminAudit(actor, "storage.migration.start", "storage_migration", job.ID, "启动本地资源迁移到平台 OSS", storageMigrationStartAudit{
		Items: totalItems, Bytes: totalBytes, Bucket: setting.Bucket, Prefix: setting.PathPrefix,
	}); err != nil {
		_ = s.repo.FailPreparingStorageMigration(job.ID, "写入迁移审计失败："+err.Error(), time.Now())
		return nil, err
	}
	if err := s.repo.QueuePreparedStorageMigration(job.ID, totalItems, totalBytes, time.Now()); err != nil {
		_ = s.repo.FailPreparingStorageMigration(job.ID, "迁移任务进入队列失败："+err.Error(), time.Now())
		return nil, err
	}
	return s.repo.StorageMigrationJob(job.ID)
}

func (s *Service) snapshotLocalResourcesForMigration(job *model.StorageMigrationJob, setting ossSettingValue) (int64, int64, error) {
	var totalItems int64
	var totalBytes int64
	afterID := ""
	for {
		resources, err := s.repo.LocalResourcesForMigration(job.CreatedAt, afterID, storageMigrationSnapshotBatchSize)
		if err != nil {
			return totalItems, totalBytes, err
		}
		if len(resources) == 0 {
			return totalItems, totalBytes, nil
		}
		items := make([]model.StorageMigrationItem, 0, len(resources))
		for index := range resources {
			resource := resources[index]
			targetKey := strings.Trim(path.Join(setting.PathPrefix, resource.ObjectKey), "/")
			if targetKey == "" {
				return totalItems, totalBytes, fmt.Errorf("资源 %s 的目标对象路径为空", resource.ID)
			}
			items = append(items, model.StorageMigrationItem{
				ID:              newID(),
				JobID:           job.ID,
				ResourceID:      resource.ID,
				Status:          model.StorageMigrationItemPending,
				SourceObjectKey: resource.ObjectKey,
				TargetObjectKey: targetKey,
				Size:            resource.Size,
				CreatedAt:       job.CreatedAt,
				UpdatedAt:       job.CreatedAt,
			})
			totalItems++
			totalBytes += resource.Size
		}
		if err := s.repo.AppendStorageMigrationItems(items); err != nil {
			return totalItems, totalBytes, err
		}
		afterID = resources[len(resources)-1].ID
		if len(resources) < storageMigrationSnapshotBatchSize {
			return totalItems, totalBytes, nil
		}
	}
}

func (s *Service) RetryStorageMigration(actor *model.User, jobID string, confirmation string) (*model.StorageMigrationJob, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if strings.TrimSpace(confirmation) != "RETRY "+jobID {
		return nil, BadAuthRequest("确认短语不正确")
	}
	s.storageMigrationMu.Lock()
	defer s.storageMigrationMu.Unlock()
	if active, err := s.repo.ActiveStorageMigrationJob(); err != nil {
		return nil, err
	} else if active != nil {
		return nil, BadAuthRequest("已有存储迁移任务正在执行")
	}
	job, err := s.repo.StorageMigrationJob(jobID)
	if err != nil {
		return nil, err
	}
	if job.Status != model.StorageMigrationPartialFailed && job.Status != model.StorageMigrationFailed {
		return nil, BadAuthRequest("当前迁移任务不可重试")
	}
	if job.TotalItems <= 0 {
		return nil, BadAuthRequest("迁移快照未完成，不能重试")
	}
	if err := s.appendAdminAudit(actor, "storage.migration.retry", "storage_migration", jobID, "重试失败的 OSS 迁移资源", nil); err != nil {
		return nil, err
	}
	if err := s.repo.RetryFailedStorageMigration(jobID, time.Now()); err != nil {
		return nil, err
	}
	return s.repo.StorageMigrationJob(jobID)
}

func (s *Service) StartStorageMigrationWorker() {
	s.storageMigrationOnce.Do(func() {
		if err := s.repo.ResumeStorageMigrations(time.Now()); err != nil {
			log.Printf("storage migration resume failed: %v", err)
		}
		go func() {
			s.processNextStorageMigration()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				s.processNextStorageMigration()
			}
		}()
	})
}

func (s *Service) processNextStorageMigration() {
	job, err := s.repo.ClaimNextStorageMigrationJob(time.Now())
	if err != nil {
		log.Printf("claim storage migration failed: %v", err)
		return
	}
	if job == nil {
		return
	}
	if err := s.runStorageMigration(job); err != nil {
		log.Printf("storage migration job=%s failed: %v", job.ID, err)
		_ = s.repo.FailRunningStorageMigration(job.ID, err.Error(), time.Now())
	}
}

func (s *Service) runStorageMigration(job *model.StorageMigrationJob) error {
	setting, err := s.activeOSSSetting()
	if err != nil {
		return fmt.Errorf("读取平台 OSS 配置失败：%w", err)
	}
	if setting.Provider != job.TargetProvider || setting.Endpoint != job.TargetEndpoint || setting.Bucket != job.TargetBucket || setting.PathPrefix != job.TargetPrefix {
		return errors.New("平台 OSS 目标配置在迁移任务创建后发生变化，拒绝继续写入")
	}
	for {
		item, err := s.repo.NextStorageMigrationItem(job.ID)
		if err != nil {
			return err
		}
		if item == nil {
			break
		}
		if err := s.repo.MarkStorageMigrationItemRunning(item.ID, time.Now()); err != nil {
			return err
		}
		if err := s.migrateLocalResourceItem(job, item, setting); err != nil {
			if saveErr := s.repo.FailStorageMigrationItem(item.ID, err.Error(), time.Now()); saveErr != nil {
				return fmt.Errorf("迁移资源失败且状态写入失败：%v；原错误：%w", saveErr, err)
			}
		}
	}
	completed, err := s.repo.FinishStorageMigrationJob(job.ID, time.Now())
	if err != nil {
		return err
	}
	log.Printf(
		"storage migration completed job=%s status=%s committed_items=%d failed_items=%d committed_bytes=%d",
		completed.ID,
		completed.Status,
		completed.CommittedItems,
		completed.FailedItems,
		completed.CommittedBytes,
	)
	return nil
}

func (s *Service) migrateLocalResourceItem(job *model.StorageMigrationJob, item *model.StorageMigrationItem, setting ossSettingValue) error {
	sourcePath, err := s.localResourcePath(item.SourceObjectKey)
	if err != nil {
		return err
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开本地资源失败：%w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取本地资源状态失败：%w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("本地资源不是普通文件")
	}
	if info.Size() != item.Size {
		return fmt.Errorf("本地资源大小不一致：数据库=%d 文件=%d", item.Size, info.Size())
	}
	sha256Digest := sha256.New()
	md5Digest := md5.New()
	if _, err := io.Copy(io.MultiWriter(sha256Digest, md5Digest), file); err != nil {
		return fmt.Errorf("计算本地资源校验和失败：%w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("重置本地资源读取位置失败：%w", err)
	}
	resource, err := s.repo.Resource(item.ResourceID)
	if err != nil {
		return err
	}
	etag, err := putOSSObject(setting, item.TargetObjectKey, resource.MimeType, item.Size, file)
	if err != nil {
		return err
	}
	metadata, err := headOSSObject(setting, item.TargetObjectKey)
	if err != nil {
		return err
	}
	if metadata.contentLength != item.Size {
		return fmt.Errorf("OSS 对象大小校验失败：期望=%d 实际=%d", item.Size, metadata.contentLength)
	}
	if etag == "" || metadata.etag == "" || !strings.EqualFold(etag, metadata.etag) {
		return fmt.Errorf("OSS ETag 校验失败：上传=%q 读取=%q", etag, metadata.etag)
	}
	sourceETag := hex.EncodeToString(md5Digest.Sum(nil))
	if !strings.EqualFold(sourceETag, metadata.etag) {
		return fmt.Errorf("OSS 对象内容校验失败：源文件 MD5=%q OSS ETag=%q", sourceETag, metadata.etag)
	}
	return s.repo.CommitStorageMigrationItem(repository.CommitStorageMigrationInput{
		JobID: job.ID, ItemID: item.ID, ResourceID: item.ResourceID, SourceObjectKey: item.SourceObjectKey,
		Provider: setting.Provider, Endpoint: setting.Endpoint, Bucket: setting.Bucket, TargetObjectKey: item.TargetObjectKey,
		SourceSHA256: hex.EncodeToString(sha256Digest.Sum(nil)), TargetETag: metadata.etag, Size: item.Size, Now: time.Now(),
	})
}

func (s *Service) localResourcePath(objectKey string) (string, error) {
	root := filepath.Join(s.dataDir, "resources")
	candidate := filepath.Join(root, filepath.FromSlash(strings.TrimSpace(objectKey)))
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("本地资源路径越界")
	}
	return candidate, nil
}
