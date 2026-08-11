package model

import "time"

type ProviderAccount struct {
	ID           string    `json:"id" gorm:"primaryKey;size:36"`
	ProviderKind string    `json:"providerKind" gorm:"not null;default:'';size:40;index"`
	Name         string    `json:"name" gorm:"not null;default:'';size:120"`
	Enabled      bool      `json:"enabled" gorm:"index"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ProviderEndpointVersion struct {
	ID                string     `json:"id" gorm:"primaryKey;size:36"`
	ProviderAccountID string     `json:"providerAccountId" gorm:"not null;default:'';size:36;index;uniqueIndex:idx_provider_endpoint_version,priority:1"`
	BaseURL           string     `json:"baseUrl" gorm:"not null;default:'';type:text"`
	Status            string     `json:"status" gorm:"not null;default:'';size:24;index"`
	CreatedBy         string     `json:"createdBy" gorm:"not null;default:'';size:36;index"`
	Version           int64      `json:"version" gorm:"uniqueIndex:idx_provider_endpoint_version,priority:2"`
	ActivatedAt       *time.Time `json:"activatedAt,omitempty"`
	RetiredAt         *time.Time `json:"retiredAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
}

type ProviderCredential struct {
	ID                string     `json:"id" gorm:"primaryKey;size:36"`
	ProviderAccountID string     `json:"providerAccountId" gorm:"not null;default:'';size:36;index"`
	Family            string     `json:"family" gorm:"not null;default:'';size:80;index"`
	HealthStatus      string     `json:"healthStatus" gorm:"not null;default:'';size:24;index"`
	HealthCode        string     `json:"healthCode" gorm:"not null;default:'';size:80"`
	HealthMessage     string     `json:"healthMessage" gorm:"not null;default:'';size:500"`
	Enabled           bool       `json:"enabled" gorm:"index"`
	ConcurrencyLimit  int        `json:"concurrencyLimit"`
	HealthCheckedAt   *time.Time `json:"healthCheckedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type ProviderCredentialVersion struct {
	ID                      string     `json:"id" gorm:"primaryKey;size:36"`
	ProviderCredentialID    string     `json:"providerCredentialId" gorm:"not null;default:'';size:36;index;uniqueIndex:idx_provider_credential_version,priority:1"`
	KeyCipher               string     `json:"-" gorm:"not null;default:'';type:text"`
	KeyFingerprint          string     `json:"-" gorm:"not null;default:'';size:128"`
	Status                  string     `json:"status" gorm:"not null;default:'';size:24;index"`
	Version                 int64      `json:"version" gorm:"uniqueIndex:idx_provider_credential_version,priority:2"`
	VerifiedAt              *time.Time `json:"verifiedAt,omitempty"`
	LastBalanceCheckedAt    *time.Time `json:"lastBalanceCheckedAt,omitempty"`
	ActivatedAt             *time.Time `json:"activatedAt,omitempty"`
	RetiredAt               *time.Time `json:"retiredAt,omitempty"`
	LastVerificationCode    string     `json:"lastVerificationCode" gorm:"not null;default:'';size:80"`
	LastVerificationTraceID string     `json:"lastVerificationTraceId" gorm:"not null;default:'';size:160"`
	LastBalanceSubunits     string     `json:"lastBalanceSubunits" gorm:"not null;default:'';size:80"`
	CreatedBy               string     `json:"createdBy" gorm:"not null;default:'';size:36;index"`
	CreatedAt               time.Time  `json:"createdAt"`
}

type ProviderTaskFact struct {
	TaskID                      string    `json:"taskId" gorm:"primaryKey;size:36"`
	BillingOrderID              string    `json:"billingOrderId" gorm:"not null;default:'';size:36;index"`
	ProviderAccountID           string    `json:"providerAccountId" gorm:"not null;default:'';size:36;index"`
	ProviderEndpointVersionID   string    `json:"providerEndpointVersionId" gorm:"not null;default:'';size:36;index"`
	ProviderCredentialID        string    `json:"providerCredentialId" gorm:"not null;default:'';size:36;index"`
	ProviderCredentialVersionID string    `json:"providerCredentialVersionId" gorm:"not null;default:'';size:36;index"`
	ChannelModelID              string    `json:"channelModelId" gorm:"not null;default:'';size:36;index"`
	ProviderTaskID              string    `json:"providerTaskId" gorm:"not null;default:'';size:160;index"`
	CreateTraceID               string    `json:"createTraceId" gorm:"not null;default:'';size:160"`
	LastPollTraceID             string    `json:"lastPollTraceId" gorm:"not null;default:'';size:160"`
	RequestedDurationSeconds    int       `json:"requestedDurationSeconds"`
	ActualDurationSeconds       int       `json:"actualDurationSeconds"`
	Resolution                  string    `json:"resolution" gorm:"not null;default:'';size:24"`
	InputVariant                string    `json:"inputVariant" gorm:"not null;default:'';size:40"`
	ProviderStatus              string    `json:"providerStatus" gorm:"not null;default:'';size:40;index"`
	AssetSourceURL              string    `json:"assetSourceUrl" gorm:"not null;default:'';type:text"`
	LastFrameURL                string    `json:"lastFrameUrl" gorm:"not null;default:'';type:text"`
	InputImageCount             int       `json:"inputImageCount"`
	InputVideoCount             int       `json:"inputVideoCount"`
	InputAudioCount             int       `json:"inputAudioCount"`
	TotalTokens                 string    `json:"totalTokens" gorm:"not null;default:'';size:80"`
	ReconciliationStatus        string    `json:"reconciliationStatus" gorm:"not null;default:'';size:40;index"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
}

type ProviderBillingFact struct {
	ID                          string    `json:"id" gorm:"primaryKey;size:36"`
	ProviderTaskFactID          string    `json:"providerTaskFactId" gorm:"not null;default:'';size:36;index"`
	ProviderCredentialVersionID string    `json:"providerCredentialVersionId" gorm:"not null;default:'';size:36;index"`
	UpstreamOrderID             string    `json:"upstreamOrderId" gorm:"not null;default:'';size:160;index"`
	ProviderTaskID              string    `json:"providerTaskId" gorm:"not null;default:'';size:160;index"`
	AmountSubunits              string    `json:"amountSubunits" gorm:"not null;default:'';size:80"`
	BillingStatus               string    `json:"billingStatus" gorm:"not null;default:'';size:40;index"`
	ProviderTaskStatus          string    `json:"providerTaskStatus" gorm:"not null;default:'';size:40;index"`
	TaskDurationSeconds         int       `json:"taskDurationSeconds"`
	TotalTokens                 string    `json:"totalTokens" gorm:"not null;default:'';size:80"`
	Description                 string    `json:"description" gorm:"not null;default:'';size:500"`
	QueryTraceID                string    `json:"queryTraceId" gorm:"not null;default:'';size:160"`
	PayloadDigest               string    `json:"payloadDigest" gorm:"not null;default:'';size:64"`
	BilledAt                    time.Time `json:"billedAt" gorm:"index"`
	ObservedAt                  time.Time `json:"observedAt" gorm:"index"`
}
