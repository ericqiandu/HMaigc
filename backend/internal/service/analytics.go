package service

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type AnalyticsQuery struct {
	From       string
	To         string
	UserID     string
	Model      string
	ChannelID  string
	Capability string
}

type AnalyticsOverview struct {
	From     time.Time             `json:"from"`
	To       time.Time             `json:"to"`
	KPI      AnalyticsKPI          `json:"kpi"`
	Trend    []AnalyticsTrendPoint `json:"trend"`
	Models   []AnalyticsModelRow   `json:"models"`
	Users    []AnalyticsUserRow    `json:"users"`
	Failures []AnalyticsFailureRow `json:"failures"`
}

type AnalyticsKPI struct {
	ActiveUsers                 int     `json:"activeUsers"`
	DAU                         int     `json:"dau"`
	WAU                         int     `json:"wau"`
	MAU                         int     `json:"mau"`
	GenerationTasks             int     `json:"generationTasks"`
	UpstreamRequests            int     `json:"upstreamRequests"`
	SuccessRate                 float64 `json:"successRate"`
	P95DurationMs               int64   `json:"p95DurationMs"`
	CurrentQueuedTasks          int64   `json:"currentQueuedTasks"`
	EstimatedCostMicros         int64   `json:"estimatedCostMicros"`
	CostAvailable               bool    `json:"costAvailable"`
	Currency                    string  `json:"currency"`
	SettledRevenueMicrocredits  int64   `json:"settledRevenueMicrocredits"`
	SettledBaseCostMicrocredits int64   `json:"settledBaseCostMicrocredits"`
	GrossProfitMicrocredits     int64   `json:"grossProfitMicrocredits"`
	SettledBillingOrders        int     `json:"settledBillingOrders"`
	PendingAmountMicrocredits   int64   `json:"pendingAmountMicrocredits"`
	PendingBillingOrders        int     `json:"pendingBillingOrders"`
	ReviewAmountMicrocredits    int64   `json:"reviewAmountMicrocredits"`
	ReviewBillingOrders         int     `json:"reviewBillingOrders"`
}

type AnalyticsTrendPoint struct {
	Day                string  `json:"day"`
	Tasks              int     `json:"tasks"`
	Requests           int     `json:"requests"`
	ActiveUsers        int     `json:"activeUsers"`
	RequestSuccessRate float64 `json:"requestSuccessRate"`
}

type AnalyticsModelRow struct {
	Model               string  `json:"model"`
	Capability          string  `json:"capability"`
	Tasks               int     `json:"tasks"`
	Requests            int     `json:"requests"`
	UniqueUsers         int     `json:"uniqueUsers"`
	TaskSuccessRate     float64 `json:"taskSuccessRate"`
	RequestSuccessRate  float64 `json:"requestSuccessRate"`
	P50DurationMs       int64   `json:"p50DurationMs"`
	P95DurationMs       int64   `json:"p95DurationMs"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CachedTokens        int64   `json:"cachedTokens"`
	UsageAvailable      bool    `json:"usageAvailable"`
	MediaCount          int     `json:"mediaCount"`
	VideoSeconds        int     `json:"videoSeconds"`
	EstimatedCostMicros int64   `json:"estimatedCostMicros"`
	CostAvailable       bool    `json:"costAvailable"`
	Currency            string  `json:"currency"`
}

type AnalyticsUserRow struct {
	UserID        string `json:"userId"`
	Name          string `json:"name"`
	ActiveDays    int    `json:"activeDays"`
	Tasks         int    `json:"tasks"`
	AgentMessages int    `json:"agentMessages"`
	CanvasDays    int    `json:"canvasDays"`
	Assets        int    `json:"assets"`
	Resources     int    `json:"resources"`
	CommonModel   string `json:"commonModel"`
}

type AnalyticsFailureRow struct {
	Type       string    `json:"type"`
	Model      string    `json:"model"`
	Count      int       `json:"count"`
	LastError  string    `json:"lastError"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type APICallLogQuery struct {
	AnalyticsQuery
	Keyword string
	Status  string
	IDs     []string
	Page    int
	Limit   int
}

type APICallLogPage struct {
	Logs  []model.ApiCallLog `json:"logs"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}

type ModelPricingRequest struct {
	ChannelID              string                    `json:"channelId"`
	Model                  string                    `json:"model"`
	Capability             string                    `json:"capability"`
	Currency               string                    `json:"currency"`
	InputPerMillionMicros  int64                     `json:"inputPerMillionMicros"`
	OutputPerMillionMicros int64                     `json:"outputPerMillionMicros"`
	CachedPerMillionMicros int64                     `json:"cachedPerMillionMicros"`
	ExpectedInputTokens    int64                     `json:"expectedInputTokens"`
	ExpectedOutputTokens   int64                     `json:"expectedOutputTokens"`
	ExpectedCachedTokens   int64                     `json:"expectedCachedTokens"`
	MaxOutputTokens        int64                     `json:"maxOutputTokens"`
	PerRequestMicros       int64                     `json:"perRequestMicros"`
	PerMediaMicros         int64                     `json:"perMediaMicros"`
	PerVideoSecondMicros   int64                     `json:"perVideoSecondMicros"`
	Tiers                  []ModelPricingTierRequest `json:"tiers"`
}

type ModelPricingTierRequest struct {
	Specification      string `json:"specification"`
	UsageMetric        string `json:"usageMetric"`
	IncludedQuantity   int64  `json:"includedQuantity"`
	SupplierCostMicros int64  `json:"supplierCostMicros"`
}

func (s *Service) AdminAnalytics(actor *model.User, query AnalyticsQuery) (*AnalyticsOverview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeAnalyticsFilter(query)
	tasks, err := s.repo.AnalyticsTasks(filter)
	if err != nil {
		return nil, err
	}
	logs, err := s.repo.AnalyticsAPICallLogs(filter)
	if err != nil {
		return nil, err
	}
	billingOrders, err := s.repo.AnalyticsBillingOrders(filter)
	if err != nil {
		return nil, err
	}
	billingSummary, err := summarizeAnalyticsBilling(billingOrders)
	if err != nil {
		return nil, err
	}
	if filter.ChannelID != "" {
		tasks = tasksWithLoggedRequests(tasks, logs)
	}
	activityFilter := filter
	rollingFrom := filter.To.AddDate(0, 0, -30)
	if rollingFrom.Before(activityFilter.From) {
		activityFilter.From = rollingFrom
	}
	activities, err := s.repo.AnalyticsActivities(activityFilter)
	if err != nil {
		return nil, err
	}
	rollingTasks := tasks
	rollingLogs := logs
	if activityFilter.From.Before(filter.From) {
		rollingTasks, err = s.repo.AnalyticsTasks(activityFilter)
		if err != nil {
			return nil, err
		}
		if hasCreationDimensionFilter(filter) {
			rollingLogs, err = s.repo.AnalyticsAPICallLogs(activityFilter)
			if err != nil {
				return nil, err
			}
		}
		if filter.ChannelID != "" {
			rollingTasks = tasksWithLoggedRequests(rollingTasks, rollingLogs)
		}
	}
	users, err := s.repo.Users()
	if err != nil {
		return nil, err
	}
	queued, err := s.repo.CurrentQueuedTaskCount()
	if err != nil {
		return nil, err
	}
	result := buildAnalyticsOverview(filter, tasks, rollingTasks, rollingLogs, logs, activities, users)
	result.KPI.CurrentQueuedTasks = queued
	result.KPI.SettledRevenueMicrocredits = billingSummary.settledRevenueMicrocredits
	result.KPI.SettledBaseCostMicrocredits = billingSummary.settledBaseCostMicrocredits
	result.KPI.GrossProfitMicrocredits = billingSummary.settledRevenueMicrocredits - billingSummary.settledBaseCostMicrocredits
	result.KPI.SettledBillingOrders = billingSummary.settledBillingOrders
	result.KPI.PendingAmountMicrocredits = billingSummary.pendingAmountMicrocredits
	result.KPI.PendingBillingOrders = billingSummary.pendingBillingOrders
	result.KPI.ReviewAmountMicrocredits = billingSummary.reviewAmountMicrocredits
	result.KPI.ReviewBillingOrders = billingSummary.reviewBillingOrders
	return result, nil
}

type analyticsBillingSummary struct {
	settledRevenueMicrocredits  int64
	settledBaseCostMicrocredits int64
	settledBillingOrders        int
	pendingAmountMicrocredits   int64
	pendingBillingOrders        int
	reviewAmountMicrocredits    int64
	reviewBillingOrders         int
}

func summarizeAnalyticsBilling(orders []model.BillingOrder) (analyticsBillingSummary, error) {
	var result analyticsBillingSummary
	for _, order := range orders {
		switch order.Status {
		case model.BillingStatusSettled:
			baseAmount, err := analyticsBillingBaseAmount(order)
			if err != nil {
				return analyticsBillingSummary{}, err
			}
			result.settledRevenueMicrocredits, err = addAnalyticsMicrocredits(result.settledRevenueMicrocredits, order.AmountMicrocredits, order.ID)
			if err != nil {
				return analyticsBillingSummary{}, err
			}
			result.settledBaseCostMicrocredits, err = addAnalyticsMicrocredits(result.settledBaseCostMicrocredits, baseAmount, order.ID)
			if err != nil {
				return analyticsBillingSummary{}, err
			}
			result.settledBillingOrders++
		case model.BillingStatusReserved, model.BillingStatusRunning:
			var err error
			result.pendingAmountMicrocredits, err = addAnalyticsMicrocredits(result.pendingAmountMicrocredits, order.AmountMicrocredits, order.ID)
			if err != nil {
				return analyticsBillingSummary{}, err
			}
			result.pendingBillingOrders++
		case model.BillingStatusUncertain:
			var err error
			result.reviewAmountMicrocredits, err = addAnalyticsMicrocredits(result.reviewAmountMicrocredits, order.AmountMicrocredits, order.ID)
			if err != nil {
				return analyticsBillingSummary{}, err
			}
			result.reviewBillingOrders++
		}
	}
	return result, nil
}

func analyticsBillingBaseAmount(order model.BillingOrder) (int64, error) {
	if order.UnitPriceMicrocredits < 0 || order.Quantity <= 0 {
		return 0, errors.New("计费订单基础金额参数无效: " + order.ID)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if order.UnitPriceMicrocredits > maxInt64/order.Quantity {
		return 0, errors.New("计费订单基础金额溢出: " + order.ID)
	}
	return order.UnitPriceMicrocredits * order.Quantity, nil
}

func addAnalyticsMicrocredits(total int64, amount int64, orderID string) (int64, error) {
	if amount < 0 {
		return 0, errors.New("计费订单金额无效: " + orderID)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if total > maxInt64-amount {
		return 0, errors.New("计费统计金额溢出: " + orderID)
	}
	return total + amount, nil
}

func (s *Service) AdminAPICallLogs(actor *model.User, query APICallLogQuery) (*APICallLogPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeAnalyticsFilter(query.AnalyticsQuery)
	logs, total, err := s.repo.QueryAPICallLogs(repository.APICallLogFilter{AnalyticsFilter: filter, Keyword: query.Keyword, Status: query.Status, Page: query.Page, Limit: query.Limit})
	if err != nil {
		return nil, err
	}
	// 历史日志允许读取逻辑删除渠道的名称，但不读取或返回渠道密钥。
	channels, err := s.repo.HistoricalSystemChannelReferences()
	if err != nil {
		return nil, err
	}
	channelNames := make(map[string]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.ID] = channel.Name
	}
	for index := range logs {
		if logs[index].ChannelID == "" {
			logs[index].ChannelName = "自定义渠道"
		} else if name := channelNames[logs[index].ChannelID]; name != "" {
			logs[index].ChannelName = name
		} else {
			logs[index].ChannelName = "已删除渠道"
		}
	}
	page, limit := query.Page, query.Limit
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return &APICallLogPage{Logs: logs, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminAPICallLog(actor *model.User, id string) (*model.ApiCallLog, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.APICallLog(strings.TrimSpace(id))
}

func (s *Service) AdminAPICallLogsCSV(actor *model.User, query APICallLogQuery) ([]byte, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeAnalyticsFilter(query.AnalyticsQuery)
	ids := uniqueNonEmpty(query.IDs)
	if len(query.IDs) > 0 && len(ids) == 0 {
		return nil, BadAuthRequest("请选择要导出的请求明细")
	}
	if len(ids) > 200 {
		return nil, BadAuthRequest("单次最多导出 200 条已选请求明细")
	}
	logs, err := s.repo.ExportAPICallLogs(repository.APICallLogFilter{AnalyticsFilter: filter, Keyword: query.Keyword, Status: query.Status, IDs: ids}, 10_000)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"时间", "用户ID", "渠道ID", "任务ID", "计费单ID", "能力", "请求阶段", "模型", "状态", "HTTP状态", "耗时毫秒", "渠道并发上限", "供应商任务ID", "成本规格", "输入字符数", "输入图片数", "输入视频数", "输入视频时长毫秒", "成本核算错误", "错误码", "错误"})
	for _, log := range logs {
		_ = writer.Write([]string{log.CreatedAt.UTC().Format(time.RFC3339), log.UserID, log.ChannelID, log.TaskID, log.BillingOrderID, log.Capability, log.RequestKind, log.Model, string(log.Status), strconv.Itoa(log.StatusCode), strconv.FormatInt(log.DurationMs, 10), strconv.Itoa(log.ConcurrencyLimit), log.ProviderRequestID, log.PricingSpecification, strconv.Itoa(log.InputCharacterCount), strconv.Itoa(log.InputImageCount), strconv.Itoa(log.InputVideoCount), strconv.FormatInt(log.InputVideoDurationMs, 10), log.CostCalculationError, log.ErrorCode, log.Error})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) AdminAnalyticsCSV(actor *model.User, query AnalyticsQuery) ([]byte, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	filter := normalizeAnalyticsFilter(query)
	logs, err := s.repo.AnalyticsAPICallLogs(filter)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"时间", "用户ID", "渠道ID", "任务ID", "能力", "请求阶段", "模型", "状态", "状态码", "耗时毫秒", "输入Token", "输出Token", "缓存Token", "媒体数量", "视频秒数", "估算费用(微单位)", "币种", "错误类型"})
	for _, log := range logs {
		cost := ""
		if log.CostAvailable {
			cost = strconv.FormatInt(log.EstimatedCostMicros, 10)
		}
		inputTokens, outputTokens, cachedTokens := "", "", ""
		if log.UsageAvailable {
			inputTokens = strconv.FormatInt(log.InputTokens, 10)
			outputTokens = strconv.FormatInt(log.OutputTokens, 10)
			cachedTokens = strconv.FormatInt(log.CachedTokens, 10)
		}
		_ = writer.Write([]string{log.CreatedAt.Format(time.RFC3339), log.UserID, log.ChannelID, log.TaskID, log.Capability, log.RequestKind, log.Model, string(log.Status), strconv.Itoa(log.StatusCode), strconv.FormatInt(log.DurationMs, 10), inputTokens, outputTokens, cachedTokens, strconv.Itoa(log.MediaCount), strconv.Itoa(log.VideoSeconds), cost, log.Currency, classifyAPICallError(log)})
	}
	writer.Flush()
	return buffer.Bytes(), writer.Error()
}

func (s *Service) AdminModelPricings(actor *model.User) ([]model.ModelPricing, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.repo.ModelPricings()
}

func (s *Service) SaveModelPricing(actor *model.User, id string, req ModelPricingRequest) (*model.ModelPricing, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	req.ChannelID = strings.TrimSpace(req.ChannelID)
	req.Model = strings.TrimSpace(req.Model)
	req.Capability = normalizeCapability(req.Capability)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Model == "" || req.Capability == "" {
		return nil, BadAuthRequest("请填写模型并选择能力类型")
	}
	if req.Currency == "" || len(req.Currency) > 12 || hasNegativePricing(req) {
		return nil, BadAuthRequest("价格配置格式无效")
	}
	tiers, err := normalizeModelPricingTiers(req.Tiers)
	if err != nil {
		return nil, err
	}
	pricing := &model.ModelPricing{ID: newID(), CreatedAt: time.Now()}
	if id != "" {
		current, err := s.repo.ModelPricingByID(id)
		if err != nil {
			return nil, err
		}
		pricing = current
	} else {
		current, err := s.repo.ModelPricingByScope(req.ChannelID, req.Model, req.Capability)
		switch {
		case err == nil:
			pricing = current
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return nil, err
		}
	}
	pricing.ChannelID = req.ChannelID
	pricing.Model = req.Model
	pricing.Capability = req.Capability
	pricing.Currency = req.Currency
	pricing.InputPerMillionMicros = req.InputPerMillionMicros
	pricing.OutputPerMillionMicros = req.OutputPerMillionMicros
	pricing.CachedPerMillionMicros = req.CachedPerMillionMicros
	pricing.ExpectedInputTokens = req.ExpectedInputTokens
	pricing.ExpectedOutputTokens = req.ExpectedOutputTokens
	pricing.ExpectedCachedTokens = req.ExpectedCachedTokens
	pricing.MaxOutputTokens = req.MaxOutputTokens
	pricing.PerRequestMicros = req.PerRequestMicros
	pricing.PerMediaMicros = req.PerMediaMicros
	pricing.PerVideoSecondMicros = req.PerVideoSecondMicros
	pricing.UpdatedAt = time.Now()
	tierModels := make([]model.ModelPricingTier, 0, len(tiers))
	now := time.Now()
	for _, tier := range tiers {
		tierModels = append(tierModels, model.ModelPricingTier{
			ID: newID(), ModelPricingID: pricing.ID, Specification: tier.Specification,
			UsageMetric: tier.UsageMetric, IncludedQuantity: tier.IncludedQuantity,
			SupplierCostMicros: tier.SupplierCostMicros, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := s.repo.SaveModelPricing(pricing, tierModels); err != nil {
		return nil, err
	}
	return pricing, nil
}

func (s *Service) DeleteModelPricing(actor *model.User, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	return s.repo.DeleteModelPricing(id)
}

func hasNegativePricing(req ModelPricingRequest) bool {
	return req.InputPerMillionMicros < 0 || req.OutputPerMillionMicros < 0 || req.CachedPerMillionMicros < 0 ||
		req.ExpectedInputTokens < 0 || req.ExpectedOutputTokens < 0 || req.ExpectedCachedTokens < 0 || req.MaxOutputTokens < 0 ||
		req.PerRequestMicros < 0 || req.PerMediaMicros < 0 || req.PerVideoSecondMicros < 0
}

func normalizeModelPricingTiers(requests []ModelPricingTierRequest) ([]ModelPricingTierRequest, error) {
	tiers := make([]ModelPricingTierRequest, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	seenUsageMetrics := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		request.Specification = strings.TrimSpace(request.Specification)
		request.UsageMetric = strings.ToLower(strings.TrimSpace(request.UsageMetric))
		key := strings.ToLower(request.Specification)
		if request.Specification == "" || len(request.Specification) > 64 || request.SupplierCostMicros < 0 {
			return nil, BadAuthRequest("规格成本配置格式无效")
		}
		if request.IncludedQuantity < 0 {
			return nil, BadAuthRequest("输入图片免费数量不能小于 0")
		}
		if _, exists := seen[key]; exists {
			return nil, BadAuthRequest("同一成本规格不能重复")
		}
		if request.UsageMetric != "" {
			if request.UsageMetric != inputImageUsageMetric {
				return nil, BadAuthRequest("不支持的用量成本指标：" + request.UsageMetric)
			}
			if request.SupplierCostMicros <= 0 {
				return nil, BadAuthRequest("输入图片超额成本必须大于 0")
			}
			if _, exists := seenUsageMetrics[request.UsageMetric]; exists {
				return nil, BadAuthRequest("同一用量成本不能重复")
			}
			seenUsageMetrics[request.UsageMetric] = struct{}{}
		} else if request.IncludedQuantity != 0 {
			return nil, BadAuthRequest("普通成本规格不能配置免费数量")
		}
		seen[key] = struct{}{}
		tiers = append(tiers, request)
	}
	return tiers, nil
}

func normalizeAnalyticsFilter(query AnalyticsQuery) repository.AnalyticsFilter {
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	if parsed, ok := parseAnalyticsTime(query.From); ok {
		from = parsed
	}
	if parsed, ok := parseAnalyticsTime(query.To); ok {
		to = parsed
		if len(strings.TrimSpace(query.To)) == len("2006-01-02") {
			to = to.AddDate(0, 0, 1)
		}
	}
	if !to.After(from) {
		to = from.AddDate(0, 0, 1)
	}
	if to.Sub(from) > 366*24*time.Hour {
		from = to.AddDate(-1, 0, 0)
	}
	return repository.AnalyticsFilter{From: from, To: to, UserID: strings.TrimSpace(query.UserID), Model: strings.TrimSpace(query.Model), ChannelID: strings.TrimSpace(query.ChannelID), Capability: normalizeCapability(query.Capability)}
}

func parseAnalyticsTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func normalizeCapability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "text", "image", "video", "audio", "vision":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func buildAnalyticsOverview(filter repository.AnalyticsFilter, tasks []model.Task, rollingTasks []model.Task, rollingLogs []model.ApiCallLog, logs []model.ApiCallLog, activities []model.UserDailyActivity, users []model.User) *AnalyticsOverview {
	result := &AnalyticsOverview{From: filter.From, To: filter.To, Trend: []AnalyticsTrendPoint{}, Models: []AnalyticsModelRow{}, Users: []AnalyticsUserRow{}, Failures: []AnalyticsFailureRow{}}
	result.KPI.GenerationTasks = len(tasks)
	result.KPI.UpstreamRequests = len(logs)
	result.KPI.SuccessRate = successRateLogs(logs)
	durations := make([]int64, 0, len(logs))
	activeUsers := map[string]bool{}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			if activity.Day.Before(filter.From) || !activity.Day.Before(filter.To) || !meaningfulActivity(activity) {
				continue
			}
			activeUsers[activity.UserID] = true
		}
	}
	for _, task := range tasks {
		activeUsers[task.UserID] = true
	}
	for _, log := range logs {
		activeUsers[log.UserID] = true
	}
	result.KPI.ActiveUsers = len(activeUsers)
	rollingActivities := activities
	if hasCreationDimensionFilter(filter) {
		rollingActivities = nil
	}
	result.KPI.DAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -1), filter.To)
	result.KPI.WAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -7), filter.To)
	result.KPI.MAU = rollingActiveUsers(rollingActivities, rollingTasks, rollingLogs, filter.To.AddDate(0, 0, -30), filter.To)
	currency := ""
	for _, log := range logs {
		durations = append(durations, log.DurationMs)
		if log.CostAvailable {
			result.KPI.CostAvailable = true
			result.KPI.EstimatedCostMicros += log.EstimatedCostMicros
			currency = mergeCurrency(currency, log.Currency)
		}
	}
	result.KPI.Currency = currency
	result.KPI.P95DurationMs = percentile(durations, 0.95)
	result.Trend = buildAnalyticsTrend(filter, tasks, logs, activities)
	result.Models = buildAnalyticsModels(tasks, logs)
	result.Users = buildAnalyticsUsers(filter, tasks, logs, activities, users)
	result.Failures = buildAnalyticsFailures(logs)
	return result
}

func buildAnalyticsTrend(filter repository.AnalyticsFilter, tasks []model.Task, logs []model.ApiCallLog, activities []model.UserDailyActivity) []AnalyticsTrendPoint {
	points := map[string]*AnalyticsTrendPoint{}
	for day := time.Date(filter.From.Year(), filter.From.Month(), filter.From.Day(), 0, 0, 0, 0, time.UTC); day.Before(filter.To); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		points[key] = &AnalyticsTrendPoint{Day: key}
	}
	requestTotals := map[string]int{}
	requestSuccess := map[string]int{}
	activeByDay := map[string]map[string]bool{}
	for _, task := range tasks {
		key := task.CreatedAt.UTC().Format("2006-01-02")
		if point := points[key]; point != nil {
			point.Tasks++
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][task.UserID] = true
		}
	}
	for _, log := range logs {
		key := log.CreatedAt.UTC().Format("2006-01-02")
		if point := points[key]; point != nil {
			point.Requests++
			requestTotals[key]++
			if log.Status == model.ApiCallStatusSucceeded {
				requestSuccess[key]++
			}
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][log.UserID] = true
		}
	}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			key := activity.Day.UTC().Format("2006-01-02")
			if points[key] == nil || !meaningfulActivity(activity) {
				continue
			}
			if activeByDay[key] == nil {
				activeByDay[key] = map[string]bool{}
			}
			activeByDay[key][activity.UserID] = true
		}
	}
	keys := make([]string, 0, len(points))
	for key := range points {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]AnalyticsTrendPoint, 0, len(keys))
	for _, key := range keys {
		point := points[key]
		point.ActiveUsers = len(activeByDay[key])
		point.RequestSuccessRate = ratio(requestSuccess[key], requestTotals[key])
		result = append(result, *point)
	}
	return result
}

func buildAnalyticsModels(tasks []model.Task, logs []model.ApiCallLog) []AnalyticsModelRow {
	type accumulator struct {
		row            AnalyticsModelRow
		users          map[string]bool
		taskSuccess    int
		taskTotal      int
		requestSuccess int
		durations      []int64
	}
	items := map[string]*accumulator{}
	get := func(modelName string, capability string) *accumulator {
		if modelName == "" {
			modelName = "未识别"
		}
		key := modelName + "\x00" + capability
		if items[key] == nil {
			items[key] = &accumulator{row: AnalyticsModelRow{Model: modelName, Capability: capability}, users: map[string]bool{}}
		}
		return items[key]
	}
	for _, task := range tasks {
		capability := capabilityFromTaskType(task.Type)
		item := get(task.Model, capability)
		item.row.Tasks++
		item.users[task.UserID] = true
		if task.Status != model.TaskStatusCancelled {
			item.taskTotal++
			if task.Status == model.TaskStatusSucceeded {
				item.taskSuccess++
			}
		}
	}
	for _, log := range logs {
		item := get(log.Model, log.Capability)
		item.row.Requests++
		item.users[log.UserID] = true
		item.durations = append(item.durations, log.DurationMs)
		if log.Status == model.ApiCallStatusSucceeded {
			item.requestSuccess++
		}
		if log.UsageAvailable {
			item.row.UsageAvailable = true
			item.row.InputTokens += log.InputTokens
			item.row.OutputTokens += log.OutputTokens
			item.row.CachedTokens += log.CachedTokens
		}
		item.row.MediaCount += log.MediaCount
		item.row.VideoSeconds += log.VideoSeconds
		if log.CostAvailable {
			item.row.CostAvailable = true
			item.row.EstimatedCostMicros += log.EstimatedCostMicros
			item.row.Currency = mergeCurrency(item.row.Currency, log.Currency)
		}
	}
	result := make([]AnalyticsModelRow, 0, len(items))
	for _, item := range items {
		item.row.UniqueUsers = len(item.users)
		item.row.TaskSuccessRate = ratio(item.taskSuccess, item.taskTotal)
		item.row.RequestSuccessRate = ratio(item.requestSuccess, item.row.Requests)
		item.row.P50DurationMs = percentile(item.durations, 0.5)
		item.row.P95DurationMs = percentile(item.durations, 0.95)
		result = append(result, item.row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tasks == result[j].Tasks {
			return result[i].Requests > result[j].Requests
		}
		return result[i].Tasks > result[j].Tasks
	})
	return result
}

func buildAnalyticsUsers(filter repository.AnalyticsFilter, tasks []model.Task, logs []model.ApiCallLog, activities []model.UserDailyActivity, users []model.User) []AnalyticsUserRow {
	names := map[string]string{}
	for _, user := range users {
		names[user.ID] = firstNonEmpty(user.DisplayName, user.Username)
	}
	rows := map[string]*AnalyticsUserRow{}
	models := map[string]map[string]int{}
	get := func(userID string) *AnalyticsUserRow {
		if rows[userID] == nil {
			rows[userID] = &AnalyticsUserRow{UserID: userID, Name: firstNonEmpty(names[userID], userID)}
		}
		return rows[userID]
	}
	if !hasCreationDimensionFilter(filter) {
		for _, activity := range activities {
			if activity.Day.Before(filter.From) || !activity.Day.Before(filter.To) || !meaningfulActivity(activity) {
				continue
			}
			row := get(activity.UserID)
			row.ActiveDays++
			row.AgentMessages += activity.AgentMessageCount
			if activity.CanvasActive {
				row.CanvasDays++
			}
			row.Assets += activity.AssetCount
			row.Resources += activity.ResourceCount
		}
	}
	for _, task := range tasks {
		if models[task.UserID] == nil {
			models[task.UserID] = map[string]int{}
		}
		models[task.UserID][task.Model]++
		get(task.UserID).Tasks++
	}
	if hasCreationDimensionFilter(filter) {
		activeDays := map[string]map[string]bool{}
		for _, log := range logs {
			get(log.UserID)
			if activeDays[log.UserID] == nil {
				activeDays[log.UserID] = map[string]bool{}
			}
			activeDays[log.UserID][log.CreatedAt.UTC().Format("2006-01-02")] = true
		}
		for userID, days := range activeDays {
			get(userID).ActiveDays = len(days)
		}
	}
	result := make([]AnalyticsUserRow, 0, len(rows))
	for userID, row := range rows {
		bestCount := 0
		for modelName, count := range models[userID] {
			if modelName != "" && count > bestCount {
				row.CommonModel, bestCount = modelName, count
			}
		}
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tasks == result[j].Tasks {
			return result[i].ActiveDays > result[j].ActiveDays
		}
		return result[i].Tasks > result[j].Tasks
	})
	return result
}

func buildAnalyticsFailures(logs []model.ApiCallLog) []AnalyticsFailureRow {
	items := map[string]*AnalyticsFailureRow{}
	for _, log := range logs {
		if log.Status != model.ApiCallStatusFailed {
			continue
		}
		typeName := classifyAPICallError(log)
		modelName := firstNonEmpty(log.Model, "未识别")
		key := typeName + "\x00" + modelName
		if items[key] == nil {
			items[key] = &AnalyticsFailureRow{Type: typeName, Model: modelName}
		}
		item := items[key]
		item.Count++
		if log.CreatedAt.After(item.LastSeenAt) {
			item.LastSeenAt = log.CreatedAt
			item.LastError = truncateRunes(log.Error, 180)
		}
	}
	result := make([]AnalyticsFailureRow, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Count > result[j].Count })
	return result
}

func classifyAPICallError(log model.ApiCallLog) string {
	value := strings.ToLower(log.Error)
	switch {
	case log.ErrorCode == contentModerationErrorCode:
		return "内容审核"
	case strings.Contains(value, "timeout"), strings.Contains(value, "超时"), log.StatusCode == 408, log.StatusCode == 504, log.StatusCode == 524:
		return "超时"
	case log.StatusCode == 401 || log.StatusCode == 403 || strings.Contains(value, "unauthorized"):
		return "鉴权失败"
	case log.StatusCode == 429 || strings.Contains(value, "rate limit"):
		return "限流"
	case log.StatusCode >= 400 && log.StatusCode < 500:
		return "请求参数"
	case log.StatusCode >= 500:
		return "上游服务"
	case value != "":
		return "网络或客户端"
	default:
		return "未知错误"
	}
}

func meaningfulActivity(activity model.UserDailyActivity) bool {
	return activity.TaskCount > 0 || activity.AgentMessageCount > 0 || activity.CanvasActive || activity.AssetCount > 0 || activity.ResourceCount > 0
}

func rollingActiveUsers(activities []model.UserDailyActivity, tasks []model.Task, logs []model.ApiCallLog, from time.Time, to time.Time) int {
	users := map[string]bool{}
	for _, activity := range activities {
		if !activity.Day.Before(from) && activity.Day.Before(to) && meaningfulActivity(activity) {
			users[activity.UserID] = true
		}
	}
	for _, task := range tasks {
		if !task.CreatedAt.Before(from) && task.CreatedAt.Before(to) {
			users[task.UserID] = true
		}
	}
	for _, log := range logs {
		if !log.CreatedAt.Before(from) && log.CreatedAt.Before(to) {
			users[log.UserID] = true
		}
	}
	return len(users)
}

func successRateLogs(logs []model.ApiCallLog) float64 {
	succeeded, failed := 0, 0
	for _, log := range logs {
		if log.Status == model.ApiCallStatusSucceeded {
			succeeded++
		} else if log.Status == model.ApiCallStatusFailed {
			failed++
		}
	}
	return ratio(succeeded, succeeded+failed)
}

func ratio(value int, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(value) * 100 / float64(total)
}

func percentile(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	items := append([]int64(nil), values...)
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	index := int(float64(len(items)-1)*quantile + 0.5)
	return items[index]
}

func mergeCurrency(current string, next string) string {
	if next == "" {
		return current
	}
	if current == "" || current == next {
		return next
	}
	return "MIXED"
}

func capabilityFromTaskType(taskType string) string {
	value := strings.ToLower(taskType)
	for _, capability := range []string{"vision", "video", "image", "audio", "text"} {
		if strings.Contains(value, capability) {
			return capability
		}
	}
	if strings.Contains(value, "storyboard") || strings.Contains(value, "agent") {
		return "text"
	}
	return ""
}

func hasCreationDimensionFilter(filter repository.AnalyticsFilter) bool {
	return filter.Model != "" || filter.ChannelID != "" || filter.Capability != ""
}

func tasksWithLoggedRequests(tasks []model.Task, logs []model.ApiCallLog) []model.Task {
	ids := map[string]bool{}
	for _, log := range logs {
		if log.TaskID != "" {
			ids[log.TaskID] = true
		}
	}
	result := make([]model.Task, 0, len(tasks))
	for _, task := range tasks {
		if ids[task.ID] {
			result = append(result, task)
		}
	}
	return result
}

func (s *Service) estimateCallCost(log *model.ApiCallLog) {
	if log.Status == model.ApiCallStatusFailed && !log.UsageAvailable {
		return
	}
	pricing, err := s.repo.ModelPricing(log.ChannelID, log.Model, log.Capability)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		return
	}
	if strings.EqualFold(strings.TrimSpace(log.Model), miniMaxH3Model) && log.RequestKind == "create" {
		cost, costErr := miniMaxH3EstimatedCost(log, pricing)
		if costErr != nil {
			log.CostAvailable = false
			log.CostCalculationError = costErr.Error()
			log.Currency = pricing.Currency
			return
		}
		log.EstimatedCostMicros = cost
		log.CostAvailable = true
		log.CostCalculationError = ""
		log.Currency = pricing.Currency
		return
	}
	if log.Capability == "audio" && (strings.HasPrefix(strings.ToLower(strings.TrimSpace(log.Model)), "speech-2.8-") || strings.EqualFold(strings.TrimSpace(log.Model), miniMaxVoiceCloningPricingModel)) {
		cost, costErr := miniMaxAudioEstimatedCost(log, pricing)
		if costErr != nil {
			log.CostAvailable = false
			log.CostCalculationError = costErr.Error()
			log.Currency = pricing.Currency
			return
		}
		log.EstimatedCostMicros = cost
		log.CostAvailable = true
		log.CostCalculationError = ""
		log.Currency = pricing.Currency
		return
	}
	cost := int64(0)
	if log.Billable {
		cost = pricing.PerRequestMicros
	}
	cost += log.InputTokens * pricing.InputPerMillionMicros / 1_000_000
	cost += log.OutputTokens * pricing.OutputPerMillionMicros / 1_000_000
	cost += log.CachedTokens * pricing.CachedPerMillionMicros / 1_000_000
	mediaUnitCost := pricing.PerMediaMicros
	if log.Capability == "image" && log.MediaCount > 0 {
		baseTiers := make([]model.ModelPricingTier, 0, len(pricing.Tiers))
		for _, tier := range pricing.Tiers {
			if strings.TrimSpace(tier.UsageMetric) == "" {
				baseTiers = append(baseTiers, tier)
			}
		}
		if len(baseTiers) > 0 {
			if strings.TrimSpace(log.PricingSpecification) == "" {
				log.CostAvailable = false
				log.CostCalculationError = "图片请求缺少分辨率与画质成本规格"
				log.Currency = pricing.Currency
				return
			}
			matched := false
			for _, tier := range baseTiers {
				if strings.EqualFold(strings.TrimSpace(tier.Specification), strings.TrimSpace(log.PricingSpecification)) {
					mediaUnitCost = tier.SupplierCostMicros
					matched = true
					break
				}
			}
			if !matched {
				log.CostAvailable = false
				log.CostCalculationError = "图片请求未配置成本规格 " + strings.TrimSpace(log.PricingSpecification)
				log.Currency = pricing.Currency
				return
			}
		}
	}
	cost += int64(log.MediaCount) * mediaUnitCost
	cost += int64(log.VideoSeconds) * pricing.PerVideoSecondMicros
	usageTier, usageTierErr := supplierInputImageUsageTier(pricing.Tiers)
	if usageTierErr != nil {
		log.CostAvailable = false
		log.CostCalculationError = usageTierErr.Error()
		log.Currency = pricing.Currency
		return
	}
	usageCostFactAvailable := log.Billable
	if mediaUnitCost > 0 {
		usageCostFactAvailable = log.MediaCount > 0
	}
	if usageTier != nil && usageCostFactAvailable {
		adjustment, adjustmentErr := calculateMediaInputUsageAdjustment(
			usageTier.UsageMetric, int64(log.InputImageCount), usageTier.IncludedQuantity, usageTier.SupplierCostMicros,
		)
		if adjustmentErr != nil {
			log.CostAvailable = false
			log.CostCalculationError = adjustmentErr.Error()
			log.Currency = pricing.Currency
			return
		}
		const maxInt64 = int64(^uint64(0) >> 1)
		if cost < 0 || adjustment.Amount > maxInt64-cost {
			log.CostAvailable = false
			log.CostCalculationError = "上游成本金额溢出"
			log.Currency = pricing.Currency
			return
		}
		cost += adjustment.Amount
	}
	log.EstimatedCostMicros = cost
	log.CostAvailable = true
	log.CostCalculationError = ""
	log.Currency = pricing.Currency
}

func (s *Service) EnrichAPICallLog(log *model.ApiCallLog, responseBody []byte) {
	if log == nil || len(responseBody) == 0 {
		return
	}
	// 非 2xx 响应正文完全由上游控制，可能回显请求密钥、提示词或其他私密输入。
	// HTTP 状态码已经提供了可审计的失败事实，因此禁止再从正文补充日志字段。
	if log.StatusCode != 0 && (log.StatusCode < http.StatusOK || log.StatusCode >= http.StatusMultipleChoices) {
		return
	}
	payload, ok := analyticsResponsePayload(responseBody)
	if !ok {
		return
	}
	if data, ok := payload["data"].(map[string]any); ok {
		for key, value := range data {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	if data, ok := payload["data"].([]any); ok && len(data) > 0 {
		if first, ok := data[0].(map[string]any); ok {
			for key, value := range first {
				if _, exists := payload[key]; !exists {
					payload[key] = value
				}
			}
		}
	}
	if log.Status == model.ApiCallStatusFailed {
		errorCode, errorMessage := providerFailureDetails(payload)
		log.ErrorCode = errorCode
		if log.Model == kuaiziGPTImage2Model {
			log.Error = "GPT Image 2 上游请求失败，请按错误码和任务 ID 核对"
		} else if errorMessage != "" {
			log.Error = errorMessage
		}
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage != nil {
		log.UsageAvailable = true
		log.InputTokens = firstInt64(usage, "input_tokens", "prompt_tokens")
		log.OutputTokens = firstInt64(usage, "output_tokens", "completion_tokens")
		if details, ok := usage["input_tokens_details"].(map[string]any); ok {
			log.CachedTokens = firstInt64(details, "cached_tokens")
		}
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok && log.CachedTokens == 0 {
			log.CachedTokens = firstInt64(details, "cached_tokens")
		}
	}
	if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
		log.UsageAvailable = true
		log.InputTokens = firstInt64(usageMetadata, "promptTokenCount")
		log.OutputTokens = firstInt64(usageMetadata, "candidatesTokenCount")
		log.CachedTokens = firstInt64(usageMetadata, "cachedContentTokenCount")
	}
	log.ProviderRequestID = firstNonEmpty(stringField(payload, "task_id"), stringField(payload, "id"), stringField(payload, "request_id"), stringField(payload, "trace_id"))
	if log.Capability == "image" {
		if data, ok := payload["data"].([]any); ok {
			log.MediaCount = len(data)
		} else if images, ok := payload["images"].([]any); ok {
			log.MediaCount = len(images)
		} else if imageURLs, ok := payload["image_urls"].([]any); ok {
			log.MediaCount = len(imageURLs)
		} else if strings.TrimSpace(stringField(payload, "image_url")) != "" {
			log.MediaCount = 1
		}
	}
	if log.Capability == "audio" && log.Status == model.ApiCallStatusSucceeded {
		log.MediaCount = 1
	}
}

func analyticsResponsePayload(responseBody []byte) (map[string]any, bool) {
	if json.Valid(responseBody) {
		var payload map[string]any
		if json.Unmarshal(responseBody, &payload) == nil && payload != nil {
			return payload, true
		}
		return nil, false
	}
	aggregated := make(map[string]any)
	for _, line := range bytes.Split(responseBody, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, []byte("[DONE]")) || !json.Valid(data) {
			continue
		}
		var event map[string]any
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		for key, value := range event {
			if key == "usage" {
				aggregated[key] = value
				continue
			}
			if _, exists := aggregated[key]; !exists {
				aggregated[key] = value
			}
		}
	}
	return aggregated, len(aggregated) > 0
}

func firstInt64(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case json.Number:
			parsed, _ := value.Int64()
			return parsed
		}
	}
	return 0
}

func (s *Service) recordActivity(userID string, event string, count int) {
	_ = s.repo.RecordUserActivity(userID, event, count, time.Now())
}
