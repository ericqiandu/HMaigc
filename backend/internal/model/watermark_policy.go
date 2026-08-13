package model

import "time"

type WatermarkCapability string
type WatermarkDirective string

const (
	PolicyKindAIWatermark = "ai_watermark"

	WatermarkCapabilityControlled    WatermarkCapability = "controlled"
	WatermarkCapabilityUnsupported   WatermarkCapability = "unsupported"
	WatermarkCapabilityNotApplicable WatermarkCapability = "not_applicable"

	WatermarkDirectiveWithWatermark    WatermarkDirective = "with_watermark"
	WatermarkDirectiveWithoutWatermark WatermarkDirective = "without_watermark"
	WatermarkDirectiveProviderDefault  WatermarkDirective = "provider_default"
)

// PolicyPublicationHead serializes publication versions; one row exists per immutable policy kind.
type PolicyPublicationHead struct {
	Kind                 string    `json:"kind" gorm:"primaryKey;size:32"`
	CurrentPublicationID string    `json:"currentPublicationId" gorm:"size:36;uniqueIndex"`
	CurrentVersion       int64     `json:"currentVersion"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type PolicyPublication struct {
	ID                     string    `json:"id" gorm:"primaryKey;size:36"`
	Kind                   string    `json:"kind" gorm:"size:32;uniqueIndex:idx_policy_publication_version,priority:1"`
	Version                int64     `json:"version" gorm:"uniqueIndex:idx_policy_publication_version,priority:2"`
	ManagementRuleRichText string    `json:"managementRuleRichText" gorm:"type:text"`
	WatermarkPolicyURL     string    `json:"watermarkPolicyUrl" gorm:"size:2048"`
	ContentHash            string    `json:"contentHash" gorm:"size:64;index"`
	PublishedBy            string    `json:"publishedBy" gorm:"size:36;index"`
	PublishedAt            time.Time `json:"publishedAt" gorm:"index"`
}

type UserWatermarkPreference struct {
	UserID                string     `json:"userId" gorm:"primaryKey;size:36"`
	RemoveWatermark       bool       `json:"removeWatermark"`
	AcceptedPublicationID string     `json:"acceptedPublicationId" gorm:"size:36;index"`
	AcceptedAt            *time.Time `json:"acceptedAt"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type UserPolicyConsent struct {
	ID                  string    `json:"id" gorm:"primaryKey;size:36"`
	UserID              string    `json:"userId" gorm:"size:36;uniqueIndex:idx_user_policy_consent,priority:1"`
	PolicyPublicationID string    `json:"policyPublicationId" gorm:"size:36;uniqueIndex:idx_user_policy_consent,priority:2"`
	AcceptedAt          time.Time `json:"acceptedAt"`
}

// UserWatermarkPreferenceEvent is append-only and records each accepted account-side write.
type UserWatermarkPreferenceEvent struct {
	ID                  string    `json:"id" gorm:"primaryKey;size:36"`
	UserID              string    `json:"userId" gorm:"size:36;index:idx_user_watermark_preference_events_user_created,priority:1"`
	RemoveWatermark     bool      `json:"removeWatermark"`
	PolicyPublicationID string    `json:"policyPublicationId" gorm:"size:36;index"`
	ResultStatus        string    `json:"resultStatus" gorm:"size:32"`
	CreatedAt           time.Time `json:"createdAt" gorm:"index:idx_user_watermark_preference_events_user_created,priority:2"`
}
