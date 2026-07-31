package service

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const staticAssetCacheControl = "public,max-age=31536000,immutable"

var immutableReleasePattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[.-][0-9A-Za-z.-]+)?$`)
var sourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type StaticAssetPublishConfig struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	PathPrefix      string
	Release         string
	SourceCommit    string
}

type StaticAssetPublishSummary struct {
	Release      string `json:"release"`
	ObjectPrefix string `json:"objectPrefix"`
	Files        int    `json:"files"`
	Bytes        int64  `json:"bytes"`
}

type staticAssetManifest struct {
	Release      string                    `json:"release"`
	SourceCommit string                    `json:"sourceCommit"`
	PublishedAt  string                    `json:"publishedAt"`
	Files        []staticAssetManifestFile `json:"files"`
}

type staticAssetManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	ETag   string `json:"etag"`
}

// PublishStaticAssets 上传完整构建产物并逐个 HEAD 校验，最后才写入 manifest。
// 每个发布版本使用独立前缀，因此发布失败不会覆盖线上版本，也无需删除对象回滚。
func PublishStaticAssets(directory string, config StaticAssetPublishConfig) (StaticAssetPublishSummary, error) {
	config.Release = strings.TrimSpace(config.Release)
	if !immutableReleasePattern.MatchString(config.Release) {
		return StaticAssetPublishSummary{}, errors.New("静态资源发布版本必须是不可变 vX.Y.Z 标签")
	}
	config.SourceCommit = strings.ToLower(strings.TrimSpace(config.SourceCommit))
	if !sourceCommitPattern.MatchString(config.SourceCommit) {
		return StaticAssetPublishSummary{}, errors.New("静态资源发布必须绑定完整 Git commit SHA")
	}
	directory, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil {
		return StaticAssetPublishSummary{}, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return StaticAssetPublishSummary{}, err
	}
	if !info.IsDir() {
		return StaticAssetPublishSummary{}, errors.New("静态资源目录无效")
	}
	setting, err := validateActiveOSSSetting(normalizeOSSSetting(ossSettingValue{
		Enabled: true, Provider: "aliyun", Endpoint: config.Endpoint, Bucket: config.Bucket,
		AccessKeyID: config.AccessKeyID, AccessKeySecret: config.AccessKeySecret, PathPrefix: config.PathPrefix,
	}), "", "静态资源 OSS 配置不完整")
	if err != nil {
		return StaticAssetPublishSummary{}, err
	}
	objectPrefix := strings.Trim(path.Join(setting.PathPrefix, "releases", config.Release), "/")
	manifestKey := path.Join(objectPrefix, "manifest.json")
	existingManifest, err := readStaticAssetManifest(setting, manifestKey)
	if err != nil {
		return StaticAssetPublishSummary{}, err
	}
	if existingManifest != nil {
		if existingManifest.Release != config.Release || existingManifest.SourceCommit != config.SourceCommit {
			return StaticAssetPublishSummary{}, errors.New("目标版本已由其他 Git commit 发布，拒绝覆盖不可变静态资源")
		}
		summary := StaticAssetPublishSummary{Release: config.Release, ObjectPrefix: objectPrefix}
		for _, file := range existingManifest.Files {
			metadata, exists, err := headStaticAssetIfExists(setting, path.Join(objectPrefix, file.Path))
			if err != nil {
				return StaticAssetPublishSummary{}, err
			}
			if !exists || metadata.contentLength != file.Size || !strings.EqualFold(metadata.etag, file.ETag) {
				return StaticAssetPublishSummary{}, fmt.Errorf("已发布静态资源清单与对象不一致：%s", file.Path)
			}
			summary.Files++
			summary.Bytes += file.Size
		}
		return summary, nil
	}
	files, err := collectStaticAssetFiles(directory)
	if err != nil {
		return StaticAssetPublishSummary{}, err
	}
	if len(files) == 0 {
		return StaticAssetPublishSummary{}, errors.New("静态资源构建目录为空")
	}

	manifest := staticAssetManifest{
		Release:      config.Release,
		SourceCommit: config.SourceCommit,
		PublishedAt:  time.Now().UTC().Format(time.RFC3339),
		Files:        make([]staticAssetManifestFile, 0, len(files)),
	}
	summary := StaticAssetPublishSummary{Release: config.Release, ObjectPrefix: objectPrefix}
	for _, file := range files {
		entry, err := publishStaticAssetFile(directory, file, objectPrefix, setting)
		if err != nil {
			return summary, fmt.Errorf("发布静态资源 %s 失败：%w", file, err)
		}
		manifest.Files = append(manifest.Files, entry)
		summary.Files++
		summary.Bytes += entry.Size
	}
	encodedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return summary, err
	}
	etag, err := putImmutableStaticObject(
		setting,
		manifestKey,
		"application/json; charset=utf-8",
		int64(len(encodedManifest)),
		strings.NewReader(string(encodedManifest)),
		staticAssetCacheControl,
	)
	if err != nil {
		return summary, fmt.Errorf("写入静态资源清单失败：%w", err)
	}
	metadata, err := headOSSObject(setting, manifestKey)
	if err != nil {
		return summary, err
	}
	if metadata.contentLength != int64(len(encodedManifest)) || etag == "" || !strings.EqualFold(etag, metadata.etag) {
		return summary, errors.New("静态资源清单校验失败")
	}
	return summary, nil
}

func collectStaticAssetFiles(directory string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(directory, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("静态资源目录不允许符号链接：%s", filePath)
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(directory, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "index.html" {
			return nil
		}
		files = append(files, relative)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func publishStaticAssetFile(directory string, relativePath string, objectPrefix string, setting ossSettingValue) (staticAssetManifestFile, error) {
	filePath := filepath.Join(directory, filepath.FromSlash(relativePath))
	file, err := os.Open(filePath)
	if err != nil {
		return staticAssetManifestFile{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return staticAssetManifestFile{}, err
	}
	if !info.Mode().IsRegular() {
		return staticAssetManifestFile{}, errors.New("静态资源不是普通文件")
	}
	sha256Digest := sha256.New()
	md5Digest := md5.New()
	if _, err := io.Copy(io.MultiWriter(sha256Digest, md5Digest), file); err != nil {
		return staticAssetManifestFile{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return staticAssetManifestFile{}, err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(relativePath)))
	objectKey := path.Join(objectPrefix, relativePath)
	sourceETag := hex.EncodeToString(md5Digest.Sum(nil))
	existingMetadata, exists, err := headStaticAssetIfExists(setting, objectKey)
	if err != nil {
		return staticAssetManifestFile{}, err
	}
	if exists {
		if existingMetadata.contentLength != info.Size() || !strings.EqualFold(existingMetadata.etag, sourceETag) {
			return staticAssetManifestFile{}, errors.New("不可变静态资源对象已存在且内容不一致")
		}
		return staticAssetManifestFile{
			Path: relativePath, Size: info.Size(), SHA256: hex.EncodeToString(sha256Digest.Sum(nil)), ETag: existingMetadata.etag,
		}, nil
	}
	etag, err := putImmutableStaticObject(setting, objectKey, contentType, info.Size(), file, staticAssetCacheControl)
	if err != nil {
		return staticAssetManifestFile{}, err
	}
	metadata, err := headOSSObject(setting, objectKey)
	if err != nil {
		return staticAssetManifestFile{}, err
	}
	if metadata.contentLength != info.Size() {
		return staticAssetManifestFile{}, fmt.Errorf("对象大小不一致：期望=%d 实际=%d", info.Size(), metadata.contentLength)
	}
	if etag == "" || metadata.etag == "" || !strings.EqualFold(etag, metadata.etag) {
		return staticAssetManifestFile{}, fmt.Errorf("对象 ETag 不一致：上传=%q 读取=%q", etag, metadata.etag)
	}
	return staticAssetManifestFile{
		Path: relativePath, Size: info.Size(), SHA256: hex.EncodeToString(sha256Digest.Sum(nil)), ETag: metadata.etag,
	}, nil
}

func putImmutableStaticObject(setting ossSettingValue, objectKey string, contentType string, size int64, body io.Reader, cacheControl string) (string, error) {
	req, err := newOSSRequestWithHeaders(http.MethodPut, setting, objectKey, contentType, body, map[string]string{
		"x-oss-forbid-overwrite": "true",
	})
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	req.Header.Set("Cache-Control", cacheControl)
	resp, err := OutboundHTTPClient(2 * time.Minute).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("不可变静态资源上传失败：%s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

func headStaticAssetIfExists(setting ossSettingValue, objectKey string) (ossObjectMetadata, bool, error) {
	req, err := newOSSRequest(http.MethodHead, setting, objectKey, "", nil)
	if err != nil {
		return ossObjectMetadata{}, false, err
	}
	resp, err := OutboundHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return ossObjectMetadata{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ossObjectMetadata{}, false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return ossObjectMetadata{}, false, fmt.Errorf("静态资源对象检查失败：%s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return ossObjectMetadata{
		contentLength: resp.ContentLength,
		etag:          strings.Trim(resp.Header.Get("ETag"), `"`),
	}, true, nil
}

func readStaticAssetManifest(setting ossSettingValue, objectKey string) (*staticAssetManifest, error) {
	req, err := newOSSRequest(http.MethodGet, setting, objectKey, "", nil)
	if err != nil {
		return nil, err
	}
	resp, err := OutboundHTTPClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("读取静态资源发布清单失败：%s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	var manifest staticAssetManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("解析静态资源发布清单失败：%w", err)
	}
	return &manifest, nil
}
