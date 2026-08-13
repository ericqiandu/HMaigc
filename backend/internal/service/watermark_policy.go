package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"golang.org/x/net/html"
)

type WatermarkPreferenceStatus string

const (
	WatermarkPreferenceDisabled          WatermarkPreferenceStatus = "disabled"
	WatermarkPreferenceActive            WatermarkPreferenceStatus = "active"
	WatermarkPreferencePolicyUpdated     WatermarkPreferenceStatus = "policy_updated"
	WatermarkPreferencePolicyUnavailable WatermarkPreferenceStatus = "policy_unavailable"
)

type WatermarkPolicySummary struct {
	ID                     string    `json:"id"`
	Version                int64     `json:"version"`
	ManagementRuleRichText string    `json:"managementRuleRichText"`
	WatermarkPolicyURL     string    `json:"watermarkPolicyUrl"`
	ContentHash            string    `json:"contentHash"`
	PublishedBy            string    `json:"publishedBy"`
	PublishedAt            time.Time `json:"publishedAt"`
}

type WatermarkPreferenceView struct {
	RemoveWatermark bool                      `json:"removeWatermark"`
	Status          WatermarkPreferenceStatus `json:"status"`
	CanEnable       bool                      `json:"canEnable"`
	AcceptedAt      *time.Time                `json:"acceptedAt"`
	CurrentPolicy   *WatermarkPolicySummary   `json:"currentPolicy"`
}

type PublishWatermarkPolicyRequest struct {
	ManagementRuleRichText string `json:"managementRuleRichText"`
	WatermarkPolicyURL     string `json:"watermarkPolicyUrl"`
}

type UpdateWatermarkPreferenceRequest struct {
	RemoveWatermark bool   `json:"removeWatermark"`
	PublicationID   string `json:"publicationId"`
}

func (s *Service) PublishWatermarkPolicy(actor *model.User, request PublishWatermarkPolicyRequest) (*WatermarkPolicySummary, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	richText := strings.TrimSpace(request.ManagementRuleRichText)
	if err := validateLegalRichText("AI 生成内容水印管理规则", richText); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	if !legalRichTextHasVisibleText(richText) {
		return nil, BadAuthRequest("AI 生成内容水印管理规则不能为空")
	}
	policyURL, err := validateWatermarkPolicyURL(request.WatermarkPolicyURL)
	if err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	digest := sha256.Sum256([]byte(richText + "\n" + policyURL))
	now := time.Now().UTC()
	publication := &model.PolicyPublication{
		ID: newID(), Kind: model.PolicyKindAIWatermark, ManagementRuleRichText: richText,
		WatermarkPolicyURL: policyURL, ContentHash: hex.EncodeToString(digest[:]), PublishedBy: actor.ID, PublishedAt: now,
	}
	audit, err := newAdminAuditEvent(actor, "watermark_policy.publish", "policy_publication", publication.ID, "发布 AI 生成内容水印管理规则", nil)
	if err != nil {
		return nil, err
	}
	if err := s.repo.PublishWatermarkPolicy(publication, audit); err != nil {
		return nil, err
	}
	return watermarkPolicySummary(publication), nil
}

func (s *Service) AdminWatermarkPolicy(actor *model.User) (*WatermarkPolicySummary, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	publication, err := s.repo.CurrentWatermarkPolicy()
	if err != nil || publication == nil {
		return nil, err
	}
	return watermarkPolicySummary(publication), nil
}

func (s *Service) WatermarkPreference(user *model.User) (*WatermarkPreferenceView, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	preference, publication, err := s.repo.WatermarkPreference(user.ID)
	if err != nil {
		return nil, err
	}
	return watermarkPreferenceView(preference, publication), nil
}

func (s *Service) UpdateWatermarkPreference(user *model.User, request UpdateWatermarkPreferenceRequest) (*WatermarkPreferenceView, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	publicationID := strings.TrimSpace(request.PublicationID)
	if request.RemoveWatermark && publicationID == "" {
		return nil, BadAuthRequest("开启去 AI 水印前必须确认当前水印规范")
	}
	now := time.Now().UTC()
	event := &model.UserWatermarkPreferenceEvent{ID: newID(), UserID: user.ID, RemoveWatermark: request.RemoveWatermark, PolicyPublicationID: publicationID, ResultStatus: "succeeded", CreatedAt: now}
	preference, publication, err := s.repo.SaveWatermarkPreference(user.ID, request.RemoveWatermark, publicationID, event, now)
	if errors.Is(err, repository.ErrWatermarkPolicyVersionConflict) {
		if auditErr := s.recordWatermarkPreferenceFailure(user.ID, request.RemoveWatermark, publicationID, "version_conflict", now); auditErr != nil {
			return nil, auditErr
		}
		log.Printf("event=watermark_preference_update_failed user_id=%s reason=version_conflict", user.ID)
		return nil, Conflict("水印规范已更新，请重新阅读后确认")
	}
	if errors.Is(err, repository.ErrWatermarkPolicyUnavailable) {
		if auditErr := s.recordWatermarkPreferenceFailure(user.ID, request.RemoveWatermark, publicationID, "policy_unavailable", now); auditErr != nil {
			return nil, auditErr
		}
		log.Printf("event=watermark_preference_update_failed user_id=%s reason=policy_unavailable", user.ID)
		return nil, BadAuthRequest("当前尚未发布可用的水印规范")
	}
	if err != nil {
		return nil, err
	}
	return watermarkPreferenceView(preference, publication), nil
}

func (s *Service) recordWatermarkPreferenceFailure(userID string, remove bool, publicationID string, resultStatus string, now time.Time) error {
	return s.repo.RecordWatermarkPreferenceFailure(&model.UserWatermarkPreferenceEvent{
		ID: newID(), UserID: userID, RemoveWatermark: remove,
		PolicyPublicationID: publicationID, ResultStatus: resultStatus, CreatedAt: now,
	})
}

func watermarkPreferenceView(preference *model.UserWatermarkPreference, publication *model.PolicyPublication) *WatermarkPreferenceView {
	view := &WatermarkPreferenceView{Status: WatermarkPreferenceDisabled, CanEnable: publication != nil, CurrentPolicy: watermarkPolicySummary(publication)}
	if publication == nil {
		view.Status = WatermarkPreferencePolicyUnavailable
		return view
	}
	if preference == nil {
		return view
	}
	view.AcceptedAt = preference.AcceptedAt
	if !preference.RemoveWatermark {
		return view
	}
	if preference.AcceptedPublicationID != publication.ID {
		view.Status = WatermarkPreferencePolicyUpdated
		return view
	}
	view.Status = WatermarkPreferenceActive
	view.RemoveWatermark = true
	return view
}

func watermarkPolicySummary(publication *model.PolicyPublication) *WatermarkPolicySummary {
	if publication == nil {
		return nil
	}
	return &WatermarkPolicySummary{
		ID: publication.ID, Version: publication.Version, ManagementRuleRichText: publication.ManagementRuleRichText,
		WatermarkPolicyURL: publication.WatermarkPolicyURL, ContentHash: publication.ContentHash,
		PublishedBy: publication.PublishedBy, PublishedAt: publication.PublishedAt,
	}
}

func validateWatermarkPolicyURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("水印规范外链不能为空")
	}
	if len(raw) > 2048 {
		return "", errors.New("水印规范外链不能超过 2048 个字符")
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f {
			return "", errors.New("水印规范外链包含控制字符")
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("水印规范外链必须是无账号信息和片段的 HTTPS 地址")
	}
	return parsed.String(), nil
}

func legalRichTextHasVisibleText(value string) bool {
	tokenizer := html.NewTokenizer(strings.NewReader(value))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.TextToken:
			if strings.TrimSpace(string(tokenizer.Text())) != "" {
				return true
			}
		}
	}
}
