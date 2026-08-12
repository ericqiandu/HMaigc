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
