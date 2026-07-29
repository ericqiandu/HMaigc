package service

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const (
	referralProgramSettingKey = "referral_program"
	referralCodeLength        = 10
	maxReferralRewardCredits  = int64(10_000_000)
)

const referralCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type ReferralProgramSetting struct {
	Enabled bool `json:"enabled"`
}

type ReferralRuleView struct {
	model.ReferralRewardRule
	PlanCode     string                       `json:"planCode"`
	PlanName     string                       `json:"planName"`
	PlanTier     string                       `json:"planTier"`
	BillingCycle model.MembershipBillingCycle `json:"billingCycle"`
	PriceCents   int64                        `json:"priceCents"`
	Currency     string                       `json:"currency"`
}

type ReferralCenter struct {
	Program     ReferralProgramSetting               `json:"program"`
	InviteCode  string                               `json:"inviteCode"`
	Summary     repository.ReferralSummary           `json:"summary"`
	Rules       []ReferralRuleView                   `json:"rules"`
	Invitations []repository.ReferralRelationshipRow `json:"invitations"`
	Total       int64                                `json:"total"`
}

type AdminReferralProgram struct {
	Program ReferralProgramSetting               `json:"program"`
	Summary repository.AdminReferralSummary      `json:"summary"`
	Rules   []ReferralRuleView                   `json:"rules"`
	Invites []repository.ReferralRelationshipRow `json:"invites"`
	Total   int64                                `json:"total"`
}

type UpdateReferralRewardRuleRequest struct {
	InviterRewardMicrocredits int64 `json:"inviterRewardMicrocredits"`
	InviteeRewardMicrocredits int64 `json:"inviteeRewardMicrocredits"`
	Enabled                   bool  `json:"enabled"`
}

type DisqualifyReferralRequest struct {
	Reason string `json:"reason"`
}

func (s *Service) ReferralCenter(user *model.User, page int, limit int) (*ReferralCenter, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	profile, err := s.ensureReferralProfile(user.ID)
	if err != nil {
		return nil, err
	}
	program, err := s.referralProgramSetting()
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.ReferralSummaryForInviter(user.ID)
	if err != nil {
		return nil, err
	}
	rules, err := s.referralRuleViews(true)
	if err != nil {
		return nil, err
	}
	page, limit = normalizeReferralPage(page, limit)
	invitations, total, err := s.repo.ReferralRelationshipsForInviter(user.ID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &ReferralCenter{
		Program: program, InviteCode: profile.Code, Summary: summary,
		Rules: rules, Invitations: invitations, Total: total,
	}, nil
}

func (s *Service) AdminReferralProgram(actor *model.User, page int, limit int) (*AdminReferralProgram, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	program, err := s.referralProgramSetting()
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.AdminReferralSummary()
	if err != nil {
		return nil, err
	}
	rules, err := s.referralRuleViews(false)
	if err != nil {
		return nil, err
	}
	page, limit = normalizeReferralPage(page, limit)
	invites, total, err := s.repo.AdminReferralRelationships(limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &AdminReferralProgram{Program: program, Summary: summary, Rules: rules, Invites: invites, Total: total}, nil
}

func (s *Service) UpdateReferralProgram(actor *model.User, request ReferralProgramSetting) (*ReferralProgramSetting, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if request.Enabled {
		if err := s.validateReferralRuleCoverage(); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{Key: referralProgramSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(referralProgramSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "referral_program.update", "system_setting", referralProgramSettingKey, "更新邀请奖励活动状态", request)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveReferralProgramSetting(&setting, audit); err != nil {
		return nil, err
	}
	return &request, nil
}

func (s *Service) UpdateReferralRewardRule(actor *model.User, planID string, request UpdateReferralRewardRuleRequest) (*ReferralRuleView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	plan, err := s.repo.MembershipPlan(strings.TrimSpace(planID))
	if err != nil {
		return nil, err
	}
	if plan.Audience != model.MembershipAudiencePersonal || plan.BillingCycle == model.MembershipBillingCycleFree || plan.PriceCents <= 0 {
		return nil, BadAuthRequest("邀请奖励只能配置在付费个人会员套餐上")
	}
	if err := validateReferralRewardAmount(request.InviterRewardMicrocredits); err != nil {
		return nil, fmt.Errorf("邀请人奖励：%w", err)
	}
	if err := validateReferralRewardAmount(request.InviteeRewardMicrocredits); err != nil {
		return nil, fmt.Errorf("受邀人奖励：%w", err)
	}
	if request.Enabled && (request.InviterRewardMicrocredits == 0 || request.InviteeRewardMicrocredits == 0) {
		return nil, BadAuthRequest("启用规则时，邀请人与受邀人奖励都必须大于 0")
	}
	now := time.Now()
	rule, err := s.repo.ReferralRewardRuleForPlan(plan.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		rule = &model.ReferralRewardRule{ID: newID(), MembershipPlanID: plan.ID, CreatedBy: actor.ID, CreatedAt: now}
	} else if err != nil {
		return nil, err
	}
	rule.InviterRewardMicrocredits = request.InviterRewardMicrocredits
	rule.InviteeRewardMicrocredits = request.InviteeRewardMicrocredits
	rule.Enabled = request.Enabled
	rule.UpdatedBy = actor.ID
	rule.UpdatedAt = now
	audit, err := newAdminAuditEvent(actor, "referral_reward_rule.update", "membership_plan", plan.ID, "更新套餐邀请奖励规则", request)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveReferralRewardRule(rule, audit); err != nil {
		return nil, err
	}
	view := referralRuleView(*plan, rule)
	return &view, nil
}

func (s *Service) DisqualifyReferral(actor *model.User, relationshipID string, request DisqualifyReferralRequest) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return BadAuthRequest("取消资格原因必须填写且不能超过 500 字")
	}
	now := time.Now()
	audit, err := newAdminAuditEvent(actor, "referral_relationship.disqualify", "referral_relationship", relationshipID, "取消邀请奖励资格", request)
	if err != nil {
		return err
	}
	if err := s.repo.DisqualifyReferralRelationship(strings.TrimSpace(relationshipID), actor.ID, reason, now, audit); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("邀请关系不存在、已发奖或已取消资格")
		}
		return err
	}
	return nil
}

func (s *Service) referralProgramSetting() (ReferralProgramSetting, error) {
	setting, err := s.repo.SystemSetting(referralProgramSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ReferralProgramSetting{Enabled: false}, nil
	}
	if err != nil {
		return ReferralProgramSetting{}, err
	}
	var value ReferralProgramSetting
	if strings.TrimSpace(setting.ValueJSON) == "" || json.Unmarshal([]byte(setting.ValueJSON), &value) != nil {
		return ReferralProgramSetting{}, errors.New("邀请奖励配置格式无效")
	}
	return value, nil
}

func (s *Service) referralRuleViews(enabledOnly bool) ([]ReferralRuleView, error) {
	plans, err := s.repo.MembershipPlans(false)
	if err != nil {
		return nil, err
	}
	rules, err := s.repo.ReferralRewardRules()
	if err != nil {
		return nil, err
	}
	ruleByPlan := make(map[string]*model.ReferralRewardRule, len(rules))
	for index := range rules {
		ruleByPlan[rules[index].MembershipPlanID] = &rules[index]
	}
	views := make([]ReferralRuleView, 0, len(plans))
	for index := range plans {
		plan := plans[index]
		if plan.Audience != model.MembershipAudiencePersonal || plan.BillingCycle == model.MembershipBillingCycleFree || plan.PriceCents <= 0 {
			continue
		}
		if !plan.Enabled {
			continue
		}
		rule := ruleByPlan[plan.ID]
		if enabledOnly && (rule == nil || !rule.Enabled || !plan.Enabled) {
			continue
		}
		views = append(views, referralRuleView(plan, rule))
	}
	return views, nil
}

func referralRuleView(plan model.MembershipPlan, rule *model.ReferralRewardRule) ReferralRuleView {
	view := ReferralRuleView{
		PlanCode: plan.Code, PlanName: plan.Name, PlanTier: plan.Tier,
		BillingCycle: plan.BillingCycle, PriceCents: plan.PriceCents, Currency: plan.Currency,
	}
	if rule != nil {
		view.ReferralRewardRule = *rule
	} else {
		view.MembershipPlanID = plan.ID
	}
	return view
}

func (s *Service) validateReferralRuleCoverage() error {
	views, err := s.referralRuleViews(false)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	for _, view := range views {
		if view.ReferralRewardRule.ID == "" || !view.Enabled {
			missing = append(missing, view.PlanName)
		}
	}
	if len(missing) > 0 {
		return BadAuthRequest("启用邀请活动前，请先配置并启用全部个人付费套餐规则：" + strings.Join(missing, "、"))
	}
	if len(views) == 0 {
		return BadAuthRequest("当前没有可配置的个人付费会员套餐")
	}
	return nil
}

func validateReferralRewardAmount(amount int64) error {
	if amount < 0 {
		return BadAuthRequest("积分不能小于 0")
	}
	if amount > maxReferralRewardCredits*CreditScale {
		return BadAuthRequest("积分超过单次奖励上限")
	}
	return nil
}

func (s *Service) ensureReferralProfile(userID string) (*model.ReferralProfile, error) {
	profile, err := s.repo.ReferralProfileForUser(userID)
	if err == nil {
		return profile, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		code, codeErr := newReferralCode()
		if codeErr != nil {
			return nil, codeErr
		}
		now := time.Now()
		candidate := &model.ReferralProfile{UserID: userID, Code: code, CreatedAt: now, UpdatedAt: now}
		created, createErr := s.repo.CreateReferralProfile(candidate)
		if createErr == nil && created {
			return candidate, nil
		}
		if createErr == nil {
			return s.repo.ReferralProfileForUser(userID)
		}
	}
	return nil, errors.New("生成唯一邀请码失败，请重试")
}

func (s *Service) referralRegistration(userID string, inviteCode string, registrationIP string, now time.Time) (*model.ReferralProfile, *model.ReferralRelationship, error) {
	code, err := newReferralCode()
	if err != nil {
		return nil, nil, err
	}
	profile := &model.ReferralProfile{UserID: userID, Code: code, CreatedAt: now, UpdatedAt: now}
	normalizedInviteCode := normalizeReferralCode(inviteCode)
	if normalizedInviteCode == "" {
		return profile, nil, nil
	}
	program, err := s.referralProgramSetting()
	if err != nil {
		return nil, nil, err
	}
	if !program.Enabled {
		return nil, nil, BadAuthRequest("当前邀请活动未开放")
	}
	inviterProfile, err := s.repo.ReferralProfileByCode(normalizedInviteCode)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, BadAuthRequest("邀请码无效")
	}
	if err != nil {
		return nil, nil, err
	}
	if inviterProfile.UserID == userID {
		return nil, nil, BadAuthRequest("不能使用自己的邀请码")
	}
	inviter, err := s.repo.User(inviterProfile.UserID)
	if err != nil {
		return nil, nil, err
	}
	if inviter.Status != model.UserStatusActive {
		return nil, nil, BadAuthRequest("邀请码当前不可用")
	}
	relationship := &model.ReferralRelationship{
		ID: newID(), InviterUserID: inviterProfile.UserID, InviteeUserID: userID,
		ReferralCode: normalizedInviteCode, BindingIP: strings.TrimSpace(registrationIP),
		Status: model.ReferralRelationshipEligible, BoundAt: now, CreatedAt: now, UpdatedAt: now,
	}
	return profile, relationship, nil
}

func normalizeReferralCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func newReferralCode() (string, error) {
	buffer := make([]byte, referralCodeLength)
	random := make([]byte, referralCodeLength)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for index, value := range random {
		buffer[index] = referralCodeAlphabet[int(value)%len(referralCodeAlphabet)]
	}
	return string(buffer), nil
}

func normalizeReferralPage(page int, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}
