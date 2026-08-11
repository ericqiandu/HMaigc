package service

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type AdminProviderEndpointView struct {
	BaseURL string `json:"baseUrl"`
	Version int64  `json:"version"`
	Active  bool   `json:"active"`
}

type AdminProviderCredentialVersionView struct {
	HasKey                bool       `json:"hasKey"`
	KeyFingerprint        string     `json:"keyFingerprint"`
	Version               int64      `json:"version"`
	HealthStatus          string     `json:"healthStatus"`
	WalletBalanceSubunits string     `json:"walletBalanceSubunits"`
	VerifiedAt            *time.Time `json:"verifiedAt,omitempty"`
}

type AdminProviderCredentialView struct {
	Family    string                              `json:"family"`
	Active    *AdminProviderCredentialVersionView `json:"active"`
	Candidate *AdminProviderCredentialVersionView `json:"candidate"`
}

type AdminProviderAccountView struct {
	ProviderKind      string                        `json:"providerKind"`
	Name              string                        `json:"name"`
	Enabled           bool                          `json:"enabled"`
	Endpoint          *AdminProviderEndpointView    `json:"endpoint,omitempty"`
	EndpointCandidate *AdminProviderEndpointView    `json:"endpointCandidate,omitempty"`
	Credentials       []AdminProviderCredentialView `json:"credentials"`
	Adapters          []ProviderAdapterDescriptor   `json:"adapters"`
}

func (s *Service) adminKuaiziProviderView() (*AdminProviderAccountView, error) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	if err != nil {
		return nil, err
	}
	view := &AdminProviderAccountView{ProviderKind: kuaiziProviderKind, Name: "筷子科技", Credentials: []AdminProviderCredentialView{}, Adapters: registry.Descriptors()}
	account, err := s.repo.ProviderAccountByKind(kuaiziProviderKind)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	view.Name = account.Name
	view.Enabled = account.Enabled
	endpointVersions, err := s.repo.ProviderEndpointVersions(account.ID)
	if err != nil {
		return nil, err
	}
	if active := activeEndpointVersion(endpointVersions); active != nil {
		view.Endpoint = providerEndpointView(active)
	}
	if pending := pendingEndpointVersion(endpointVersions); pending != nil {
		if view.Endpoint == nil {
			view.Endpoint = providerEndpointView(pending)
		} else {
			view.EndpointCandidate = providerEndpointView(pending)
		}
	}
	credentials, err := s.repo.ProviderCredentials(account.ID)
	if err != nil {
		return nil, err
	}
	for _, credential := range credentials {
		versions, versionsErr := s.repo.ProviderCredentialVersions(credential.ID)
		if versionsErr != nil {
			return nil, versionsErr
		}
		credentialView := AdminProviderCredentialView{Family: credential.Family}
		active := activeCredentialVersion(versions)
		if active != nil {
			activeView := providerCredentialVersionView(active, credential.HealthStatus)
			credentialView.Active = &activeView
		}
		if pending := pendingCredentialVersion(versions); pending != nil {
			candidate := providerCredentialCandidateView(pending)
			credentialView.Candidate = &candidate
		}
		view.Credentials = append(view.Credentials, credentialView)
	}
	return view, nil
}

func activeEndpointVersion(versions []model.ProviderEndpointVersion) *model.ProviderEndpointVersion {
	for index := range versions {
		if versions[index].Status == "active" {
			return &versions[index]
		}
	}
	return nil
}

func pendingEndpointVersion(versions []model.ProviderEndpointVersion) *model.ProviderEndpointVersion {
	for index := range versions {
		if versions[index].Status == "pending" {
			return &versions[index]
		}
	}
	return nil
}

func activeEndpointID(versions []model.ProviderEndpointVersion) string {
	if active := activeEndpointVersion(versions); active != nil {
		return active.ID
	}
	return ""
}

func preferredVerificationEndpoint(versions []model.ProviderEndpointVersion) *model.ProviderEndpointVersion {
	if active := activeEndpointVersion(versions); active != nil {
		return active
	}
	return pendingEndpointVersion(versions)
}

func activeCredentialVersion(versions []model.ProviderCredentialVersion) *model.ProviderCredentialVersion {
	for index := range versions {
		if versions[index].Status == "active" {
			return &versions[index]
		}
	}
	return nil
}

func pendingCredentialVersion(versions []model.ProviderCredentialVersion) *model.ProviderCredentialVersion {
	for index := range versions {
		if versions[index].Status == "pending" {
			return &versions[index]
		}
	}
	return nil
}

func preferredVerificationCredential(versions []model.ProviderCredentialVersion) *model.ProviderCredentialVersion {
	if pending := pendingCredentialVersion(versions); pending != nil {
		return pending
	}
	return activeCredentialVersion(versions)
}

func providerEndpointView(version *model.ProviderEndpointVersion) *AdminProviderEndpointView {
	return &AdminProviderEndpointView{BaseURL: version.BaseURL, Version: version.Version, Active: version.Status == "active"}
}

func providerCredentialVersionView(version *model.ProviderCredentialVersion, healthStatus string) AdminProviderCredentialVersionView {
	return AdminProviderCredentialVersionView{
		HasKey: version.KeyCipher != "", KeyFingerprint: version.KeyFingerprint, Version: version.Version,
		HealthStatus: healthStatus, WalletBalanceSubunits: version.LastBalanceSubunits, VerifiedAt: version.VerifiedAt,
	}
}

func providerCredentialCandidateView(version *model.ProviderCredentialVersion) AdminProviderCredentialVersionView {
	status := "unverified"
	if version.LastVerificationCode != "" {
		switch version.LastVerificationCode {
		case "verified":
			status = "healthy"
			if version.LastBalanceSubunits == "0" {
				status = "insufficient_balance"
			}
		default:
			status = kuaiziHealthStatusForCode(version.LastVerificationCode)
		}
	}
	return providerCredentialVersionView(version, status)
}
