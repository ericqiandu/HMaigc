package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type UpdateUserRequest struct {
	DisplayName string           `json:"displayName"`
	Email       string           `json:"email"`
	Password    string           `json:"password"`
	Role        model.UserRole   `json:"role"`
	Status      model.UserStatus `json:"status"`
}

type BulkDisableUsersRequest struct {
	UserIDs []string `json:"userIds"`
}

type BulkDisableUsersResult struct {
	Users         []model.User `json:"users"`
	DisabledCount int          `json:"disabledCount"`
}

type AdminListQuery struct {
	Keyword string
	Status  string
	Type    string
	Page    int
	Limit   int
}

type AdminUserPage struct {
	Users []AdminUser `json:"users"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

type AdminUser struct {
	model.User
	AvailableMicrocredits int64 `json:"availableMicrocredits"`
	ReservedMicrocredits  int64 `json:"reservedMicrocredits"`
}

type AdminChannelPage struct {
	Channels []PublicModelChannel `json:"channels"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	Limit    int                  `json:"limit"`
}

type AdminUserReference struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type AdminChannelReference struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

type AdminReferenceData struct {
	Users    []AdminUserReference    `json:"users"`
	Channels []AdminChannelReference `json:"channels"`
}

type ChannelRequest struct {
	Name                 string   `json:"name"`
	BaseURL              string   `json:"baseUrl"`
	APIKey               string   `json:"apiKey"`
	InterfaceType        string   `json:"interfaceType"`
	ConcurrencyLimit     *int     `json:"concurrencyLimit"`
	UseGlobalConcurrency *bool    `json:"useGlobalConcurrency"`
	Models               []string `json:"models"`
	Enabled              *bool    `json:"enabled"`
}

type PublicModelChannel struct {
	ID               string                     `json:"id"`
	UserID           string                     `json:"userId"`
	Scope            model.ChannelScope         `json:"scope"`
	Enabled          bool                       `json:"enabled"`
	Name             string                     `json:"name"`
	BaseURL          string                     `json:"baseUrl"`
	APIKey           string                     `json:"apiKey"`
	APIFormat        string                     `json:"apiFormat"`
	InterfaceType    model.ChannelInterfaceType `json:"interfaceType"`
	ConcurrencyLimit int                        `json:"concurrencyLimit"`
	Models           []string                   `json:"models"`
	ModelCosts       []PublicChannelModelPrice  `json:"modelCosts"`
	Voices           []PublicChannelVoice       `json:"voices"`
	HasAPIKey        bool                       `json:"hasApiKey"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
}

type PublicChannelVoice struct {
	ID                 string                  `json:"id"`
	VoiceKey           string                  `json:"voiceKey"`
	DisplayName        string                  `json:"displayName"`
	Description        string                  `json:"description"`
	Language           string                  `json:"language"`
	Kind               string                  `json:"kind"`
	AccessPolicy       model.ModelAccessPolicy `json:"accessPolicy"`
	Accessible         bool                    `json:"accessible"`
	CompatibleModels   []string                `json:"compatibleModels"`
	ProviderStatus     string                  `json:"providerStatus"`
	Enabled            bool                    `json:"enabled"`
	OwnedByCurrentUser bool                    `json:"ownedByCurrentUser"`
	Favorited          bool                    `json:"favorited"`
	OwnerUserID        string                  `json:"ownerUserId,omitempty"`
	LastError          string                  `json:"lastError,omitempty"`
}

type PublicChannelModelPrice struct {
	Model                    string                        `json:"model"`
	DisplayName              string                        `json:"displayName"`
	MarketingCopy            string                        `json:"marketingCopy"`
	PromotionBadge           string                        `json:"promotionBadge"`
	EstimatedDurationSeconds int                           `json:"estimatedDurationSeconds"`
	BrandKey                 string                        `json:"brandKey"`
	AccessPolicy             model.ModelAccessPolicy       `json:"accessPolicy"`
	Accessible               bool                          `json:"accessible"`
	Capability               string                        `json:"capability"`
	WatermarkCapability      model.WatermarkCapability     `json:"watermarkCapability"`
	BillingMode              string                        `json:"billingMode"`
	PriceStrategy            string                        `json:"priceStrategy"`
	UnitPriceMicrocredits    int64                         `json:"unitPriceMicrocredits"`
	PriceTiers               []PublicChannelModelPriceTier `json:"priceTiers"`
	ProviderCapabilities     *PublicProviderCapabilities   `json:"providerCapabilities,omitempty"`
}

type PublicProviderCapabilities struct {
	ModelKey                  string                    `json:"modelKey"`
	DisplayName               string                    `json:"displayName"`
	UpstreamMode              string                    `json:"upstreamMode"`
	Capability                string                    `json:"capability"`
	Resolutions               []string                  `json:"resolutions"`
	InputVariants             []string                  `json:"inputVariants"`
	Ratios                    []string                  `json:"ratios"`
	Qualities                 []string                  `json:"qualities"`
	OutputCounts              []int                     `json:"outputCounts"`
	DurationMin               int                       `json:"durationMin"`
	DurationMax               int                       `json:"durationMax"`
	SupportsSmartDuration     bool                      `json:"supportsSmartDuration"`
	SupportsGeneratedAudio    bool                      `json:"supportsGeneratedAudio"`
	WatermarkCapability       model.WatermarkCapability `json:"watermarkCapability"`
	SupportsAudioOnly         bool                      `json:"supportsAudioOnly"`
	RequiresAdaptiveFrames    bool                      `json:"requiresAdaptiveFrames"`
	MaxImages                 int                       `json:"maxImages"`
	MaxVideos                 int                       `json:"maxVideos"`
	MaxAudios                 int                       `json:"maxAudios"`
	MaxVideoDurationSeconds   int                       `json:"maxVideoDurationSeconds"`
	MaxAudioDurationSeconds   int                       `json:"maxAudioDurationSeconds"`
	Tools                     []string                  `json:"tools"`
	SupportsTokenUsageBilling bool                      `json:"supportsTokenUsageBilling"`
}

type PublicChannelModelPriceTier struct {
	Resolution            string `json:"resolution"`
	InputVariant          string `json:"inputVariant"`
	UnitPriceMicrocredits int64  `json:"unitPriceMicrocredits"`
}

func (s *Service) RequireAdmin(user *model.User) error {
	if user == nil {
		return Unauthorized("请先登录")
	}
	if user.Role != model.UserRoleAdmin {
		return Forbidden("需要管理员权限")
	}
	return nil
}

func (s *Service) AdminUsers(actor *model.User, query AdminListQuery) (*AdminUserPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	users, total, err := s.repo.AdminUsers(query.Keyword, model.UserRole(query.Type), model.UserStatus(query.Status), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	userIDs := make([]string, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}
	accounts, err := s.repo.CreditAccounts(userIDs)
	if err != nil {
		return nil, err
	}
	accountByUserID := make(map[string]model.CreditAccount, len(accounts))
	for _, account := range accounts {
		accountByUserID[account.UserID] = account
	}
	result := make([]AdminUser, 0, len(users))
	for _, user := range users {
		account := accountByUserID[user.ID]
		result = append(result, AdminUser{User: user, AvailableMicrocredits: account.AvailableMicrocredits, ReservedMicrocredits: account.ReservedMicrocredits})
	}
	return &AdminUserPage{Users: result, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminReferences(actor *model.User) (*AdminReferenceData, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	users, err := s.repo.AdminUserReferences()
	if err != nil {
		return nil, err
	}
	channels, err := s.repo.AdminSystemChannelReferences()
	if err != nil {
		return nil, err
	}
	result := &AdminReferenceData{
		Users:    make([]AdminUserReference, 0, len(users)),
		Channels: make([]AdminChannelReference, 0, len(channels)),
	}
	for _, user := range users {
		result.Users = append(result.Users, AdminUserReference{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName})
	}
	for _, channel := range channels {
		items, itemErr := s.repo.ChannelModels(channel.ID, true)
		if itemErr != nil {
			return nil, itemErr
		}
		models := make([]string, 0, len(items))
		for _, item := range items {
			models = append(models, item.ModelKey)
		}
		result.Channels = append(result.Channels, AdminChannelReference{ID: channel.ID, Name: channel.Name, Models: uniqueNonEmpty(models)})
	}
	return result, nil
}

func (s *Service) UpdateUser(actor *model.User, userID string, req UpdateUserRequest) (*model.User, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	user, err := s.repo.User(userID)
	if err != nil {
		return nil, err
	}
	if actor.ID == user.ID && req.Status == model.UserStatusDisabled {
		return nil, BadAuthRequest("不能禁用当前管理员账号")
	}
	nextRole := user.Role
	if req.Role == model.UserRoleAdmin || req.Role == model.UserRoleUser {
		nextRole = req.Role
	}
	nextStatus := user.Status
	if req.Status == model.UserStatusActive || req.Status == model.UserStatusDisabled {
		nextStatus = req.Status
	}
	if user.Role == model.UserRoleAdmin && nextRole != model.UserRoleAdmin {
		count, err := s.repo.ActiveAdminCountExcluding(user.ID)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, BadAuthRequest("至少需要保留一个管理员")
		}
	}
	if user.Role == model.UserRoleAdmin && nextStatus != model.UserStatusActive {
		count, err := s.repo.ActiveAdminCountExcluding(user.ID)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, BadAuthRequest("至少需要保留一个可用管理员")
		}
	}
	if strings.TrimSpace(req.DisplayName) != "" {
		user.DisplayName = normalizeDisplayName(req.DisplayName, user.Username)
	}
	if req.Email != "" {
		email := normalizeEmail(req.Email)
		if err := validateEmail(email); err != nil {
			return nil, err
		}
		existing, err := s.repo.UserByEmail(email)
		if err == nil && existing.ID != user.ID {
			return nil, BadAuthRequest("邮箱已被注册")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		user.Email = email
	}
	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			return nil, err
		}
		hash, err := hashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		user.PasswordHash = hash
		_ = s.repo.DeleteUserAuthSessions(user.ID)
	}
	user.Role = nextRole
	user.Status = nextStatus
	user.UpdatedAt = time.Now()
	if err := s.repo.Save(user); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "user.update", "user", user.ID, "更新用户账号状态或资料", map[string]any{"role": user.Role, "status": user.Status}); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) DeleteUser(actor *model.User, userID string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	if actor.ID == userID {
		return BadAuthRequest("不能删除当前登录的管理员账号")
	}
	user, err := s.repo.User(userID)
	if err != nil {
		return err
	}
	if user.Role == model.UserRoleAdmin {
		count, err := s.repo.ActiveAdminCountExcluding(user.ID)
		if err != nil {
			return err
		}
		if count == 0 {
			return BadAuthRequest("至少需要保留一个管理员")
		}
	}
	if err := s.repo.DeleteUserAuthSessions(user.ID); err != nil {
		return err
	}
	// 有资金流水后必须保留用户主体，删除入口改为停用并清除全部登录态。
	user.Status = model.UserStatusDisabled
	user.UpdatedAt = time.Now()
	if err := s.repo.Save(user); err != nil {
		return err
	}
	return s.appendAdminAudit(actor, "user.disable", "user", user.ID, "停用用户并清除登录态", nil)
}

func (s *Service) BulkDisableUsers(actor *model.User, req BulkDisableUsersRequest) (*BulkDisableUsersResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(req.UserIDs))
	userIDs := make([]string, 0, len(req.UserIDs))
	for _, rawID := range req.UserIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, BadAuthRequest("用户 ID 无效")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		userIDs = append(userIDs, id)
	}
	if len(userIDs) == 0 {
		return nil, BadAuthRequest("请选择要停用的用户")
	}
	if len(userIDs) > 100 {
		return nil, BadAuthRequest("单次最多停用 100 个用户")
	}
	metadata, err := json.Marshal(map[string]any{"userIds": userIDs, "count": len(userIDs)})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	events := make([]model.AdminAuditEvent, 0, len(userIDs))
	for _, userID := range userIDs {
		events = append(events, model.AdminAuditEvent{ID: newID(), ActorUserID: actor.ID, Action: "user.bulk_disable", TargetType: "user", TargetID: userID, Summary: "批量停用用户并清除登录态", MetadataJSON: string(metadata), CreatedAt: now})
	}
	users, err := s.repo.BulkDisableUsers(actor.ID, userIDs, events, now)
	if errors.Is(err, repository.ErrBulkUserNotFound) {
		return nil, BadAuthRequest("部分用户不存在，请刷新列表后重试")
	}
	if errors.Is(err, repository.ErrBulkCurrentAdmin) {
		return nil, BadAuthRequest("不能停用当前登录的管理员账号")
	}
	if errors.Is(err, repository.ErrBulkLastActiveAdmin) {
		return nil, BadAuthRequest("批量操作后至少需要保留一个可用管理员")
	}
	if err != nil {
		return nil, err
	}
	return &BulkDisableUsersResult{Users: users, DisabledCount: len(users)}, nil
}

func (s *Service) PublicSystemChannels(user *model.User) ([]PublicModelChannel, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	hasMembership, err := s.HasActiveMembership(user.ID)
	if err != nil {
		return nil, err
	}
	channels, err := s.repo.SystemChannels(false)
	if err != nil {
		return nil, err
	}
	result := make([]PublicModelChannel, 0, len(channels))
	for _, channel := range channels {
		items, itemErr := s.repo.ChannelModels(channel.ID, false)
		if itemErr != nil {
			return nil, itemErr
		}
		items, itemErr = s.publiclyCallableChannelModels(items)
		if itemErr != nil {
			return nil, itemErr
		}
		voices, voiceErr := s.repo.ChannelVoicesForUser(channel.ID, user.ID, false)
		if voiceErr != nil {
			return nil, voiceErr
		}
		voiceIDs := make([]string, 0, len(voices))
		for _, voice := range voices {
			voiceIDs = append(voiceIDs, voice.ID)
		}
		favorites, favoriteErr := s.repo.UserVoiceFavoriteIDs(user.ID, voiceIDs)
		if favoriteErr != nil {
			return nil, favoriteErr
		}
		public := publicChannel(channel, false, items, hasMembership)
		public.Voices, err = publicChannelVoicesForUser(voices, hasMembership, false, user.ID, favorites)
		if err != nil {
			return nil, err
		}
		result = append(result, public)
	}
	return result, nil
}

func (s *Service) publiclyCallableChannelModels(items []model.ChannelModel) ([]model.ChannelModel, error) {
	credentialIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ProviderCredentialID != "" {
			credentialIDs = append(credentialIDs, item.ProviderCredentialID)
		}
	}
	healthy, err := s.repo.HealthyProviderCredentialIDs(credentialIDs)
	if err != nil {
		return nil, err
	}
	result := make([]model.ChannelModel, 0, len(items))
	for _, item := range items {
		_, _, managed := kuaiziProviderFamilyForModel(item.ModelKey)
		if managed && (item.ProviderCredentialID == "" || !healthy[item.ProviderCredentialID]) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) SystemChannel(id string) (*model.ModelChannel, error) {
	return s.repo.SystemChannel(id)
}

func (s *Service) AdminSystemChannelPage(actor *model.User, query AdminListQuery) (*AdminChannelPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	channels, total, err := s.repo.AdminSystemChannels(query.Keyword, query.Type, query.Status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	result := make([]PublicModelChannel, 0, len(channels))
	for _, channel := range channels {
		items, itemErr := s.repo.ChannelModels(channel.ID, true)
		if itemErr != nil {
			return nil, itemErr
		}
		voices, voiceErr := s.repo.ChannelVoices(channel.ID, true)
		if voiceErr != nil {
			return nil, voiceErr
		}
		public := publicChannel(channel, true, items, true)
		public.Voices, err = publicChannelVoices(voices, true, true)
		if err != nil {
			return nil, err
		}
		result = append(result, public)
	}
	return &AdminChannelPage{Channels: result, Total: total, Page: page, Limit: limit}, nil
}

func normalizeAdminPage(page int, limit int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return page, limit
}

func (s *Service) CreateSystemChannel(actor *model.User, req ChannelRequest) (*PublicModelChannel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := channelFromRequest(req, model.ModelChannel{ID: newID(), UserID: actor.ID, Scope: model.ChannelScopeSystem, Enabled: true})
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(&channel); err != nil {
		return nil, err
	}
	if err := s.syncInitialChannelModels(&channel, req.Models); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultChannelVoicesForChannel(&channel); err != nil {
		return nil, err
	}
	items, err := s.repo.ChannelModels(channel.ID, true)
	if err != nil {
		return nil, err
	}
	public := publicChannel(channel, true, items, true)
	voices, err := s.repo.ChannelVoices(channel.ID, true)
	if err != nil {
		return nil, err
	}
	public.Voices, err = publicChannelVoices(voices, true, true)
	if err != nil {
		return nil, err
	}
	return &public, nil
}

func (s *Service) UpdateSystemChannel(actor *model.User, id string, req ChannelRequest) (*PublicModelChannel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.repo.AdminSystemChannel(id)
	if err != nil {
		return nil, err
	}
	req = mergeChannelRequest(req, *channel)
	next, err := channelFromRequest(req, *channel)
	if err != nil {
		return nil, err
	}
	next.ID = channel.ID
	next.UserID = channel.UserID
	next.Scope = model.ChannelScopeSystem
	next.CreatedAt = channel.CreatedAt
	if req.APIKey == "" {
		next.APIKey = channel.APIKey
	}
	if err := s.repo.Save(&next); err != nil {
		return nil, err
	}
	if err := s.syncInitialChannelModels(&next, req.Models); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultChannelVoicesForChannel(&next); err != nil {
		return nil, err
	}
	items, err := s.repo.ChannelModels(next.ID, true)
	if err != nil {
		return nil, err
	}
	public := publicChannel(next, true, items, true)
	voices, err := s.repo.ChannelVoices(next.ID, true)
	if err != nil {
		return nil, err
	}
	public.Voices, err = publicChannelVoices(voices, true, true)
	if err != nil {
		return nil, err
	}
	return &public, nil
}

func (s *Service) DeleteSystemChannel(actor *model.User, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	channel, err := s.repo.AdminSystemChannel(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("系统渠道不存在或已删除")
		}
		return err
	}
	// 保留主体供历史账单和调用日志关联，但从所有业务查询中隐藏并清除密钥。
	err = s.repo.DeleteSystemChannel(channel.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BadAuthRequest("系统渠道不存在或已删除")
	}
	return err
}

func (s *Service) LogAPICall(log model.ApiCallLog) error {
	if log.ID == "" {
		log.ID = newID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	s.estimateCallCost(&log)
	return s.repo.SaveProviderCall(&log, "", false)
}

func (s *Service) logProviderCall(log model.ApiCallLog, leaseOwner string, asyncCreate bool) error {
	if log.ID == "" {
		log.ID = newID()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	s.estimateCallCost(&log)
	return s.repo.SaveProviderCall(&log, leaseOwner, asyncCreate)
}

func (s *Service) APICallLogs(actor *model.User, limit int) ([]model.ApiCallLog, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	return s.repo.ApiCallLogs(actor.ID, actor.Role == model.UserRoleAdmin, limit)
}

func channelFromRequest(req ChannelRequest, channel model.ModelChannel) (model.ModelChannel, error) {
	name := strings.TrimSpace(req.Name)
	baseURL := strings.TrimSpace(req.BaseURL)
	interfaceType := model.ChannelInterfaceType(strings.TrimSpace(req.InterfaceType))
	if name == "" {
		return channel, BadAuthRequest("请填写渠道名称")
	}
	if baseURL == "" {
		return channel, BadAuthRequest("请填写 Base URL")
	}
	if !validChannelInterfaceType(interfaceType) {
		return channel, BadAuthRequest("请选择有效的接口类型")
	}
	if _, err := ValidateOutboundURL(baseURL); err != nil {
		return channel, err
	}
	models := uniqueNonEmpty(req.Models)
	modelsJSON, _ := json.Marshal(models)
	channel.Name = name
	channel.BaseURL = strings.TrimRight(baseURL, "/")
	if req.APIKey != "" {
		channel.APIKey = req.APIKey
	}
	if interfaceType == model.ChannelInterfaceKlingVideo {
		if _, err := parseKlingCredentials(channel.APIKey); err != nil {
			return channel, BadAuthRequest(err.Error())
		}
	}
	// 系统渠道均由后端按已声明的接口类型分发；具体鉴权由对应渠道执行器负责。
	channel.APIFormat = "openai"
	channel.InterfaceType = interfaceType
	if req.UseGlobalConcurrency != nil && *req.UseGlobalConcurrency {
		channel.ConcurrencyLimit = 0
	} else if req.ConcurrencyLimit != nil {
		if *req.ConcurrencyLimit < minChannelConcurrencyLimit || *req.ConcurrencyLimit > maxChannelConcurrencyLimit {
			return channel, BadAuthRequest("最大并发数必须是 1-999 的整数")
		}
		channel.ConcurrencyLimit = *req.ConcurrencyLimit
	} else if req.UseGlobalConcurrency != nil {
		return channel, BadAuthRequest("请填写渠道最大并发数")
	}
	channel.ModelsJSON = string(modelsJSON)
	if req.Enabled != nil {
		channel.Enabled = *req.Enabled
	}
	return channel, nil
}

func mergeChannelRequest(req ChannelRequest, channel model.ModelChannel) ChannelRequest {
	if strings.TrimSpace(req.Name) == "" {
		req.Name = channel.Name
	}
	if strings.TrimSpace(req.BaseURL) == "" {
		req.BaseURL = channel.BaseURL
	}
	if req.Models == nil {
		req.Models = channelModelNames(channel)
	}
	if strings.TrimSpace(req.InterfaceType) == "" {
		req.InterfaceType = string(channel.InterfaceType)
		if req.InterfaceType == "" {
			req.InterfaceType = string(inferChannelInterfaceType(req.Models))
		}
	}
	return req
}

func validChannelInterfaceType(value model.ChannelInterfaceType) bool {
	switch value {
	case model.ChannelInterfaceChatCompletion, model.ChannelInterfaceOpenAIResponse, model.ChannelInterfaceOpenAIImage, model.ChannelInterfaceAPIMartImage, model.ChannelInterfaceNewAPIVideo, model.ChannelInterfaceXAIVideo, model.ChannelInterfaceAIOpenVideoVolcengine, model.ChannelInterfaceMiniMaxSpeech, model.ChannelInterfaceMiniMaxVideo, model.ChannelInterfaceKlingVideo:
		return true
	default:
		return false
	}
}

func inferChannelInterfaceType(models []string) model.ChannelInterfaceType {
	for _, name := range models {
		value := strings.ToLower(name)
		if strings.Contains(value, "video") || strings.Contains(value, "seedance") || strings.Contains(value, "sora") || strings.Contains(value, "veo") || strings.Contains(value, "kling") || strings.Contains(value, "wan") || strings.Contains(value, "hailuo") {
			return model.ChannelInterfaceNewAPIVideo
		}
	}
	for _, name := range models {
		value := strings.ToLower(name)
		if strings.Contains(value, "image") || strings.Contains(value, "seedream") || strings.Contains(value, "dall-e") || strings.Contains(value, "flux") || strings.Contains(value, "imagen") {
			return model.ChannelInterfaceOpenAIImage
		}
	}
	return model.ChannelInterfaceChatCompletion
}

func publicChannel(channel model.ModelChannel, admin bool, channelModels []model.ChannelModel, hasMembership bool) PublicModelChannel {
	models := make([]string, 0, len(channelModels))
	modelCosts := make([]PublicChannelModelPrice, 0, len(channelModels))
	for _, item := range channelModels {
		if !item.Enabled {
			continue
		}
		watermarkCapability := publicWatermarkCapability(channel, item)
		if !admin && (item.Capability == "image" || item.Capability == "video") && watermarkCapability == "" {
			continue
		}
		pricingReady := channelModelPricingReady(item)
		if !admin && !pricingReady {
			continue
		}
		models = append(models, item.ModelKey)
		if item.Enabled && (admin && item.PriceConfigured || pricingReady) {
			tiers := make([]PublicChannelModelPriceTier, 0, len(item.PriceTiers))
			for _, tier := range item.PriceTiers {
				tiers = append(tiers, PublicChannelModelPriceTier{Resolution: tier.Resolution, InputVariant: tier.InputVariant, UnitPriceMicrocredits: tier.UnitPriceMicrocredits})
			}
			modelCosts = append(modelCosts, PublicChannelModelPrice{
				Model: item.ModelKey, DisplayName: item.DisplayName, MarketingCopy: item.MarketingCopy,
				PromotionBadge: item.PromotionBadge, EstimatedDurationSeconds: item.EstimatedDurationSeconds, BrandKey: item.BrandKey,
				AccessPolicy: item.AccessPolicy, Accessible: item.AccessPolicy == model.ModelAccessAuthenticated || hasMembership,
				Capability: item.Capability, WatermarkCapability: watermarkCapability,
				BillingMode: item.BillingMode, PriceStrategy: item.PriceStrategy,
				UnitPriceMicrocredits: item.UnitPriceMicrocredits, PriceTiers: tiers,
				ProviderCapabilities: publicProviderModelCapabilities(channel.InterfaceType, item.ModelKey),
			})
		}
	}
	if admin && len(models) == 0 {
		_ = json.Unmarshal([]byte(channel.ModelsJSON), &models)
	}
	apiKey := ""
	baseURL := channel.BaseURL
	if channel.Scope == model.ChannelScopeSystem {
		if !admin {
			apiKey = "system"
			baseURL = "/api/ai/system/" + channel.ID
		}
	} else if admin {
		apiKey = channel.APIKey
	}
	interfaceType := channel.InterfaceType
	if !validChannelInterfaceType(interfaceType) {
		interfaceType = inferChannelInterfaceType(models)
	}
	return PublicModelChannel{
		ID:               channel.ID,
		UserID:           channel.UserID,
		Scope:            channel.Scope,
		Enabled:          channel.Enabled,
		Name:             channel.Name,
		BaseURL:          baseURL,
		APIKey:           apiKey,
		APIFormat:        channel.APIFormat,
		InterfaceType:    interfaceType,
		ConcurrencyLimit: channel.ConcurrencyLimit,
		Models:           models,
		ModelCosts:       modelCosts,
		HasAPIKey:        strings.TrimSpace(channel.APIKey) != "",
		CreatedAt:        channel.CreatedAt,
		UpdatedAt:        channel.UpdatedAt,
	}
}

func channelModelPricingReady(item model.ChannelModel) bool {
	if !item.PriceConfigured {
		return false
	}
	_, spec, managed := kuaiziProviderFamilyForModel(item.ModelKey)
	if !managed || spec.Capability != "video" {
		return true
	}
	configured := make(map[string]bool, len(item.PriceTiers))
	for _, tier := range item.PriceTiers {
		if tier.UnitPriceMicrocredits <= 0 {
			return false
		}
		variant := strings.ToLower(strings.TrimSpace(tier.InputVariant))
		if variant == "" {
			variant = "standard"
		}
		configured[channelModelPriceTierKey(tier.Resolution, variant)] = true
	}
	for _, resolution := range spec.Resolutions {
		for _, variant := range []string{"standard", "reference_video"} {
			if !configured[channelModelPriceTierKey(resolution, variant)] {
				return false
			}
		}
	}
	return true
}

func publicProviderModelCapabilities(interfaceType model.ChannelInterfaceType, modelKey string) *PublicProviderCapabilities {
	if interfaceType == model.ChannelInterfaceAPIMartImage {
		return publicAPIMartImageCapabilities(modelKey)
	}
	capabilities, ok := kuaiziProviderModelSpec(modelKey)
	if !ok {
		return nil
	}
	inputVariants := []string{}
	if capabilities.Capability == "video" {
		inputVariants = []string{"standard", "reference_video"}
	}
	return &PublicProviderCapabilities{
		ModelKey: capabilities.ModelKey, DisplayName: capabilities.DisplayName,
		UpstreamMode: capabilities.UpstreamMode, Capability: capabilities.Capability,
		Resolutions: append([]string{}, capabilities.Resolutions...), InputVariants: inputVariants, Ratios: append([]string{}, capabilities.Ratios...),
		Qualities: append([]string{}, capabilities.Qualities...), OutputCounts: append([]int{}, capabilities.OutputCounts...),
		DurationMin: capabilities.DurationMin, DurationMax: capabilities.DurationMax,
		SupportsSmartDuration: capabilities.SupportsSmartDuration, SupportsGeneratedAudio: capabilities.SupportsGeneratedAudio,
		WatermarkCapability: capabilities.WatermarkCapability, SupportsAudioOnly: capabilities.SupportsAudioOnly,
		RequiresAdaptiveFrames: capabilities.RequiresAdaptiveFrames,
		MaxImages:              capabilities.MaxImages, MaxVideos: capabilities.MaxVideos, MaxAudios: capabilities.MaxAudios,
		MaxVideoDurationSeconds: capabilities.MaxVideoDurationSeconds, MaxAudioDurationSeconds: capabilities.MaxAudioDurationSeconds,
		Tools:                     append([]string{}, capabilities.Tools...),
		SupportsTokenUsageBilling: kuaiziModelSupportsTokenUsageBilling(modelKey),
	}
}

func publicAPIMartImageCapabilities(modelKey string) *PublicProviderCapabilities {
	profile, err := apimartImageProfile(modelKey)
	if err != nil {
		return nil
	}
	qualities := []string{}
	if profile.supportsQuality {
		qualities = []string{"low", "medium", "high"}
	}
	return &PublicProviderCapabilities{
		ModelKey: modelKey, DisplayName: profile.label, UpstreamMode: modelKey, Capability: "image",
		Resolutions: append([]string{}, profile.resolutions...), InputVariants: []string{},
		Ratios: apimartPublishedAspectRatios(profile), Qualities: qualities, OutputCounts: []int{1},
		WatermarkCapability: model.WatermarkCapabilityUnsupported,
		MaxImages:           profile.maxReferenceImages,
		Tools:               []string{},
	}
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
