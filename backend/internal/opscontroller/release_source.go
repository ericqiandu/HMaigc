package opscontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"
)

const maxReleaseResponseBytes = 1 << 20

type ReleaseSource interface {
	Check(context.Context, string) opsprotocol.ReleaseCheck
}

type GitHubReleaseSource struct {
	URL        string
	Token      string
	HTTPClient *http.Client
}

type githubReleaseResponse struct {
	TagName string `json:"tag_name"`
}

func (s GitHubReleaseSource) Check(ctx context.Context, currentVersion string) opsprotocol.ReleaseCheck {
	result := opsprotocol.ReleaseCheck{
		Status: "unconfigured", CurrentVersion: currentVersion, CheckedAt: time.Now(),
		Message: "未配置 HMAIGC_RELEASES_API_URL，无法检查远程版本",
	}
	if strings.TrimSpace(s.URL) == "" {
		return result
	}
	result.Status = "failed"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(s.Token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		result.Message = "版本源请求失败: " + err.Error()
		return result
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseResponseBytes+1))
	if err != nil {
		result.Message = "版本源响应读取失败: " + err.Error()
		return result
	}
	if len(body) > maxReleaseResponseBytes {
		result.Message = "版本源响应超过大小限制"
		return result
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Message = fmt.Sprintf("版本源返回 HTTP %d", response.StatusCode)
		return result
	}
	var payload githubReleaseResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		result.Message = "版本源响应格式无效: " + err.Error()
		return result
	}
	latest := strings.TrimSpace(payload.TagName)
	if err := opsprotocol.ValidateReleaseVersion(latest); err != nil {
		result.Message = "版本源返回了无效标签: " + err.Error()
		return result
	}
	result.Status = "ok"
	result.LatestVersion = latest
	result.Message = ""
	if currentVersion == "" {
		return result
	}
	comparison, err := opsprotocol.CompareReleaseVersions(currentVersion, latest)
	if err != nil {
		result.Status = "failed"
		result.Message = "当前版本状态无效: " + err.Error()
		return result
	}
	result.UpdateAvailable = comparison < 0
	return result
}

type StaticReleaseSource struct {
	Result opsprotocol.ReleaseCheck
	Err    error
}

func (s StaticReleaseSource) Check(_ context.Context, currentVersion string) opsprotocol.ReleaseCheck {
	result := s.Result
	result.CurrentVersion = currentVersion
	result.CheckedAt = time.Now()
	if s.Err != nil {
		result.Status = "failed"
		result.Message = s.Err.Error()
	}
	if result.Status == "" {
		result.Status = "ok"
	}
	return result
}
