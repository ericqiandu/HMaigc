package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const membershipStorefrontSettingKey = "membership_storefront"

type MembershipStorefrontPromotion struct {
	Enabled           bool      `json:"enabled"`
	Title             string    `json:"title"`
	Subtitle          string    `json:"subtitle"`
	SubtitleHighlight string    `json:"subtitleHighlight"`
	EndsAt            time.Time `json:"endsAt"`
}

type MembershipStorefrontActivity struct {
	Icon string `json:"icon"`
	Text string `json:"text"`
}

type MembershipStorefrontCopy struct {
	CreatorTab        string `json:"creatorTab"`
	TeamTab           string `json:"teamTab"`
	YearCycle         string `json:"yearCycle"`
	MonthCycle        string `json:"monthCycle"`
	CreditStore       string `json:"creditStore"`
	ActivityHeading   string `json:"activityHeading"`
	ExclusiveHeading  string `json:"exclusiveHeading"`
	GenerationHeading string `json:"generationHeading"`
	FAQHeading        string `json:"faqHeading"`
}

type MembershipStorefrontGenerationColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type MembershipStorefrontPlanHighlight struct {
	Tier   string `json:"tier"`
	Images string `json:"images"`
	Videos string `json:"videos"`
}

type MembershipStorefrontGenerationRow struct {
	Model  string   `json:"model"`
	Icon   string   `json:"icon"`
	Unit   string   `json:"unit"`
	Values []string `json:"values"`
}

type MembershipStorefrontGenerationSection struct {
	Title string                              `json:"title"`
	Rows  []MembershipStorefrontGenerationRow `json:"rows"`
}

type MembershipStorefrontFAQ struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type MembershipStorefrontSetting struct {
	Promotion          MembershipStorefrontPromotion           `json:"promotion"`
	Copy               MembershipStorefrontCopy                `json:"copy"`
	Activities         []MembershipStorefrontActivity          `json:"activities"`
	CommonFeatures     []string                                `json:"commonFeatures"`
	ExclusiveFeatures  []string                                `json:"exclusiveFeatures"`
	PlanHighlights     []MembershipStorefrontPlanHighlight     `json:"planHighlights"`
	GenerationColumns  []MembershipStorefrontGenerationColumn  `json:"generationColumns"`
	GenerationSections []MembershipStorefrontGenerationSection `json:"generationSections"`
	GenerationFootnote string                                  `json:"generationFootnote"`
	MembershipNotes    []string                                `json:"membershipNotes"`
	FAQs               []MembershipStorefrontFAQ               `json:"faqs"`
}

type MembershipStorefrontView struct {
	Presentation MembershipStorefrontSetting `json:"presentation"`
	Plans        []MembershipPlanView        `json:"plans"`
	ServerNow    time.Time                   `json:"serverNow"`
	UpdatedAt    *time.Time                  `json:"updatedAt,omitempty"`
}

func (s *Service) MembershipStorefront() (*MembershipStorefrontView, error) {
	presentation, setting, err := s.readMembershipStorefrontSetting()
	if err != nil {
		return nil, err
	}
	plans, err := s.MembershipPlans(nil)
	if err != nil {
		return nil, err
	}
	if err := validateMembershipStorefrontPlanCoverage(presentation, plans); err != nil {
		return nil, err
	}
	result := &MembershipStorefrontView{
		Presentation: presentation,
		Plans:        plans,
		ServerNow:    time.Now().UTC(),
	}
	if setting != nil {
		updatedAt := setting.UpdatedAt.UTC()
		result.UpdatedAt = &updatedAt
	}
	return result, nil
}

func (s *Service) AdminMembershipStorefront(actor *model.User) (*MembershipStorefrontView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.MembershipStorefront()
}

func (s *Service) UpdateMembershipStorefront(actor *model.User, value MembershipStorefrontSetting) (*MembershipStorefrontView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	normalizeMembershipStorefrontSetting(&value)
	if err := validateMembershipStorefrontSetting(value); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	setting := &model.SystemSetting{Key: membershipStorefrontSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	current, err := s.repo.SystemSetting(membershipStorefrontSettingKey)
	if err == nil {
		setting.CreatedAt = current.CreatedAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	audit, err := newAdminAuditEvent(actor, "membership_storefront.update", "system_setting", membershipStorefrontSettingKey, "更新会员商城运营内容", value)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveMembershipStorefrontSetting(setting, audit); err != nil {
		return nil, err
	}
	return s.AdminMembershipStorefront(actor)
}

func (s *Service) readMembershipStorefrontSetting() (MembershipStorefrontSetting, *model.SystemSetting, error) {
	setting, err := s.repo.SystemSetting(membershipStorefrontSettingKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		value := defaultMembershipStorefrontSetting()
		return value, nil, validateMembershipStorefrontSetting(value)
	}
	if err != nil {
		return MembershipStorefrontSetting{}, nil, err
	}
	var value MembershipStorefrontSetting
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return MembershipStorefrontSetting{}, nil, errors.New("会员商城运营配置格式无效")
	}
	normalizeMembershipStorefrontSetting(&value)
	if err := validateMembershipStorefrontSetting(value); err != nil {
		return MembershipStorefrontSetting{}, nil, fmt.Errorf("会员商城运营配置无效: %w", err)
	}
	return value, setting, nil
}

func defaultMembershipStorefrontSetting() MembershipStorefrontSetting {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	return MembershipStorefrontSetting{
		Promotion: MembershipStorefrontPromotion{
			Enabled:           true,
			Title:             "Seedance 2.5 预售重磅上线！购买会员最高赠送 60 条/席位",
			Subtitle:          "MiniMax H3 会员 6 折，",
			SubtitleHighlight: "低至 0.1 元/秒！",
			EndsAt:            time.Date(2026, time.December, 31, 23, 59, 59, 0, location),
		},
		Copy: MembershipStorefrontCopy{
			CreatorTab: "创作会员", TeamTab: "团队版会员", YearCycle: "连续包年", MonthCycle: "连续包月",
			CreditStore: "积分超市", ActivityHeading: "限时活动", ExclusiveHeading: "独家功能", GenerationHeading: "每月生成数量", FAQHeading: "常见问题",
		},
		Activities: []MembershipStorefrontActivity{
			{Icon: "✦", Text: "MiniMax H3 限时 6 折"},
			{Icon: "✦", Text: "Happy Horse 1.1 限时 4 折"},
		},
		CommonFeatures:    []string{"去除品牌水印 商用无忧", "会员专享无限次加速", "登录每日赠送 20 积分", "训练专属权益"},
		ExclusiveFeatures: []string{"全能视频 Agent", "3D 导演台", "脚本策划", "智能分镜（Kling 3.0/O3）", "9/4/25 宫格生成", "宫格切分", "镜头聚焦", "多模态主体库", "视频剪辑", "720 度全景"},
		PlanHighlights: []MembershipStorefrontPlanHighlight{
			{Tier: "origin", Images: "6,000 张图片", Videos: "187 个视频"},
			{Tier: "pro", Images: "18,400 张图片", Videos: "575 个视频"},
			{Tier: "max", Images: "65,200 张图片", Videos: "2,037 个视频"},
			{Tier: "ultra", Images: "264,000 张图片", Videos: "8,250 个视频"},
		},
		GenerationColumns: []MembershipStorefrontGenerationColumn{
			{Key: "standard-1500", Label: "标准版 1.5k"},
			{Key: "advanced-4600", Label: "进阶版 4.6k"},
			{Key: "pro-11700", Label: "高级版 11.7k"},
			{Key: "pro-16300", Label: "高级版 16.3k"},
			{Key: "deluxe-32800", Label: "豪华版 32.8k"},
			{Key: "supreme-50500", Label: "至尊版 50.5k"},
			{Key: "supreme-66000", Label: "至尊版 66k"},
		},
		GenerationSections: []MembershipStorefrontGenerationSection{
			{Title: "视频模型（含参数）", Rows: []MembershipStorefrontGenerationRow{
				{Model: "Seedance 2.0（720P）", Icon: "♪", Unit: "秒", Values: []string{"56", "170", "433", "604", "1,215", "1,870", "2,444"}},
				{Model: "Happy Horse 1.0（720P）", Icon: "♞", Unit: "秒", Values: []string{"63", "192", "488", "679", "1,367", "2,104", "2,750"}},
				{Model: "Kling 3.0（720P）", Icon: "◉", Unit: "秒", Values: []string{"188", "575", "1,463", "2,038", "4,100", "6,313", "8,250"}},
			}},
			{Title: "图片模型（含参数）", Rows: []MembershipStorefrontGenerationRow{
				{Model: "General image Pro", Icon: "✂", Unit: "张", Values: []string{"107", "329", "836", "1,164", "2,343", "3,607", "4,714"}},
			}},
		},
		GenerationFootnote: "生成次数为单一模型的估算值，实际结果会因模型参数与计费策略变化。",
		MembershipNotes:    []string{"免费用户权益与签到奖励以后台当前积分规则为准。", "订阅积分在支付成功后按所购套餐的订阅周期一次性发放，未使用积分保留在账户中。"},
		FAQs: []MembershipStorefrontFAQ{
			{Question: "积分有效期规则", Answer: "订阅积分在支付成功后按所购套餐的订阅周期一次性发放，未使用积分保留在账户中。通过活动、签到等方式获得的积分以对应规则为准，全部变动均可在积分明细中追溯。"},
			{Question: "会员与积分退款规则", Answer: "会员与积分退款资格以订单状态、权益使用情况和平台最新退款政策为准，请通过订单记录提交处理。"},
			{Question: "积分返还规则", Answer: "生成任务失败时，系统会按实际失败部分返还已扣积分。返还记录可在积分明细中查询。"},
			{Question: "积分消耗顺序", Answer: "积分按系统账本记录的有效期与可用状态扣减，具体变动可在积分明细中追溯。"},
			{Question: "如何获取更多积分", Answer: "可以升级会员套餐、前往积分超市兑换积分或参加后台已启用的运营活动。"},
			{Question: "发票申请", Answer: "包含开票权益的已支付会员订单，可以在本页发票中心提交电子发票申请并查询处理结果。"},
		},
	}
}

func normalizeMembershipStorefrontSetting(value *MembershipStorefrontSetting) {
	value.Promotion.Title = strings.TrimSpace(value.Promotion.Title)
	value.Promotion.Subtitle = strings.TrimSpace(value.Promotion.Subtitle)
	value.Promotion.SubtitleHighlight = strings.TrimSpace(value.Promotion.SubtitleHighlight)
	copyValues := []*string{
		&value.Copy.CreatorTab, &value.Copy.TeamTab, &value.Copy.YearCycle, &value.Copy.MonthCycle, &value.Copy.CreditStore,
		&value.Copy.ActivityHeading, &value.Copy.ExclusiveHeading, &value.Copy.GenerationHeading, &value.Copy.FAQHeading,
	}
	for _, item := range copyValues {
		*item = strings.TrimSpace(*item)
	}
	for index := range value.Activities {
		value.Activities[index].Icon = strings.TrimSpace(value.Activities[index].Icon)
		value.Activities[index].Text = strings.TrimSpace(value.Activities[index].Text)
	}
	trimStrings(value.CommonFeatures)
	trimStrings(value.ExclusiveFeatures)
	for index := range value.PlanHighlights {
		value.PlanHighlights[index].Tier = strings.TrimSpace(value.PlanHighlights[index].Tier)
		value.PlanHighlights[index].Images = strings.TrimSpace(value.PlanHighlights[index].Images)
		value.PlanHighlights[index].Videos = strings.TrimSpace(value.PlanHighlights[index].Videos)
	}
	trimStrings(value.MembershipNotes)
	for index := range value.GenerationColumns {
		value.GenerationColumns[index].Key = strings.TrimSpace(value.GenerationColumns[index].Key)
		value.GenerationColumns[index].Label = strings.TrimSpace(value.GenerationColumns[index].Label)
	}
	for sectionIndex := range value.GenerationSections {
		section := &value.GenerationSections[sectionIndex]
		section.Title = strings.TrimSpace(section.Title)
		for rowIndex := range section.Rows {
			row := &section.Rows[rowIndex]
			row.Model = strings.TrimSpace(row.Model)
			row.Icon = strings.TrimSpace(row.Icon)
			row.Unit = strings.TrimSpace(row.Unit)
			trimStrings(row.Values)
		}
	}
	value.GenerationFootnote = strings.TrimSpace(value.GenerationFootnote)
	for index := range value.FAQs {
		value.FAQs[index].Question = strings.TrimSpace(value.FAQs[index].Question)
		value.FAQs[index].Answer = strings.TrimSpace(value.FAQs[index].Answer)
	}
}

func trimStrings(values []string) {
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}
}

func validateMembershipStorefrontSetting(value MembershipStorefrontSetting) error {
	if value.Promotion.Enabled {
		if invalidStorefrontText(value.Promotion.Title, 1, 160) || invalidStorefrontText(value.Promotion.Subtitle, 1, 120) || invalidStorefrontText(value.Promotion.SubtitleHighlight, 1, 80) || value.Promotion.EndsAt.IsZero() {
			return BadAuthRequest("会员活动标题、副标题或截止时间无效")
		}
	}
	copyValues := []string{
		value.Copy.CreatorTab, value.Copy.TeamTab, value.Copy.YearCycle, value.Copy.MonthCycle, value.Copy.CreditStore,
		value.Copy.ActivityHeading, value.Copy.ExclusiveHeading, value.Copy.GenerationHeading, value.Copy.FAQHeading,
	}
	for _, text := range copyValues {
		if invalidStorefrontText(text, 1, 40) {
			return BadAuthRequest("会员商城界面文案无效")
		}
	}
	if len(value.Activities) > 12 || len(value.CommonFeatures) == 0 || len(value.CommonFeatures) > 24 || len(value.ExclusiveFeatures) == 0 || len(value.ExclusiveFeatures) > 40 {
		return BadAuthRequest("会员活动或权益数量无效")
	}
	for _, activity := range value.Activities {
		if invalidStorefrontText(activity.Icon, 1, 8) || invalidStorefrontText(activity.Text, 1, 100) {
			return BadAuthRequest("会员活动内容无效")
		}
	}
	for _, feature := range append(append([]string{}, value.CommonFeatures...), value.ExclusiveFeatures...) {
		if invalidStorefrontText(feature, 1, 120) {
			return BadAuthRequest("会员权益内容无效")
		}
	}
	if len(value.PlanHighlights) == 0 || len(value.PlanHighlights) > 20 {
		return BadAuthRequest("会员套餐生成能力摘要无效")
	}
	highlightTiers := make(map[string]struct{}, len(value.PlanHighlights))
	for _, highlight := range value.PlanHighlights {
		if invalidStorefrontKey(highlight.Tier) || invalidStorefrontText(highlight.Images, 1, 80) || invalidStorefrontText(highlight.Videos, 1, 80) {
			return BadAuthRequest("会员套餐生成能力摘要无效")
		}
		if _, exists := highlightTiers[highlight.Tier]; exists {
			return BadAuthRequest("会员套餐生成能力层级重复")
		}
		highlightTiers[highlight.Tier] = struct{}{}
	}
	if len(value.GenerationColumns) == 0 || len(value.GenerationColumns) > 12 || len(value.GenerationSections) == 0 || len(value.GenerationSections) > 8 {
		return BadAuthRequest("生成数量表结构无效")
	}
	columnKeys := make(map[string]struct{}, len(value.GenerationColumns))
	for _, column := range value.GenerationColumns {
		if invalidStorefrontKey(column.Key) || invalidStorefrontText(column.Label, 1, 60) {
			return BadAuthRequest("生成数量表列无效")
		}
		if _, exists := columnKeys[column.Key]; exists {
			return BadAuthRequest("生成数量表列标识重复")
		}
		columnKeys[column.Key] = struct{}{}
	}
	for _, section := range value.GenerationSections {
		if invalidStorefrontText(section.Title, 1, 80) || len(section.Rows) == 0 || len(section.Rows) > 30 {
			return BadAuthRequest("生成数量表分组无效")
		}
		for _, row := range section.Rows {
			if invalidStorefrontText(row.Model, 1, 100) || invalidStorefrontText(row.Icon, 1, 8) || invalidStorefrontText(row.Unit, 1, 12) || len(row.Values) != len(value.GenerationColumns) {
				return BadAuthRequest("生成数量表行无效")
			}
			for _, cell := range row.Values {
				if invalidStorefrontText(cell, 1, 20) {
					return BadAuthRequest("生成数量表数值无效")
				}
			}
		}
	}
	if invalidStorefrontText(value.GenerationFootnote, 1, 300) || len(value.MembershipNotes) > 8 || len(value.FAQs) == 0 || len(value.FAQs) > 30 {
		return BadAuthRequest("会员说明或常见问题配置无效")
	}
	for _, note := range value.MembershipNotes {
		if invalidStorefrontText(note, 1, 300) {
			return BadAuthRequest("会员说明内容无效")
		}
	}
	for _, item := range value.FAQs {
		if invalidStorefrontText(item.Question, 1, 120) || invalidStorefrontText(item.Answer, 1, 1000) {
			return BadAuthRequest("会员常见问题内容无效")
		}
	}
	return nil
}

func invalidStorefrontText(value string, min int, max int) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(value))
	return length < min || length > max
}

func invalidStorefrontKey(value string) bool {
	if len(value) < 1 || len(value) > 80 {
		return true
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return true
	}
	return false
}

func validateMembershipStorefrontPlanCoverage(value MembershipStorefrontSetting, plans []MembershipPlanView) error {
	tiers := make(map[string]struct{}, len(value.PlanHighlights))
	for _, highlight := range value.PlanHighlights {
		tiers[highlight.Tier] = struct{}{}
	}
	for _, plan := range plans {
		if _, exists := tiers[plan.Tier]; !exists {
			return fmt.Errorf("会员商城缺少套餐层级 %s 的生成能力摘要", plan.Tier)
		}
	}
	return nil
}
