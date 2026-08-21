package service

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newBillingQuoteTestService(t *testing.T, models ...model.ChannelModel) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.ModelChannel{},
		&model.ChannelModel{},
		&model.ChannelModelPriceTier{},
		&model.ModelPricing{},
		&model.ModelPricingTier{},
		&model.SuperResolutionPricingRule{},
		&model.SystemSetting{},
		&model.MembershipPlan{},
		&model.MembershipSubscription{},
		&model.TeamMember{},
		&model.BillingOrder{},
		&model.CreditLedgerEntry{},
	); err != nil {
		t.Fatal(err)
	}
	if len(models) > 0 {
		if err := db.Create(&models).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &Service{repo: repository.New(db)}, db
}

func TestQuoteTaskBillingAddsInputImageUsageAdjustment(t *testing.T) {
	item := model.ChannelModel{
		ID: "seedream-pro", ChannelID: "channel", ModelKey: "seedream-5-0-pro", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 120_000, PriceConfigured: true, Enabled: true, PriceVersion: 4,
		PriceTiers: []model.ChannelModelPriceTier{{
			ID: "user-input-overage", UsageMetric: inputImageUsageMetric, IncludedQuantity: 1, UnitPriceMicrocredits: 4_000,
		}},
	}
	svc, db := newBillingQuoteTestService(t, item)
	pricing := model.ModelPricing{
		ID: "supplier-price", ChannelID: item.ChannelID, Model: item.ModelKey, Capability: item.Capability,
		Currency: "CNY", PerRequestMicros: 600_000,
		Tiers: []model.ModelPricingTier{{
			ID: "supplier-input-overage", Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric,
			IncludedQuantity: 1, SupplierCostMicros: 20_000,
		}},
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}
	request := TaskBillingQuoteRequest{
		Type: "canvas_image", Operation: "generate", BatchCount: 2,
		Input: TaskBillingQuoteInput{Mode: "image", ReferenceImageCount: 3, Config: TaskBillingQuoteConfig{
			ChannelID: item.ChannelID, Model: item.ModelKey,
		}},
	}

	quote, err := svc.QuoteTaskBilling("user", request)
	if err != nil {
		t.Fatal(err)
	}
	if quote.PerTaskAmountMicrocredits != 128_000 || quote.AmountMicrocredits != 256_000 {
		t.Fatalf("quote = %#v", quote)
	}
	if quote.UsageAdjustment == nil {
		t.Fatal("quote usage adjustment is nil")
	}
	if quote.UsageAdjustment.ActualQuantity != 3 || quote.UsageAdjustment.IncludedQuantity != 1 ||
		quote.UsageAdjustment.BillableQuantity != 2 || quote.UsageAdjustment.UnitPriceMicrocredits != 4_000 ||
		quote.UsageAdjustment.PerTaskAmountMicrocredits != 8_000 || quote.UsageAdjustment.AmountMicrocredits != 16_000 {
		t.Fatalf("usage adjustment = %#v", quote.UsageAdjustment)
	}

	oneReference := request
	oneReference.Input.ReferenceImageCount = 1
	oneReferenceQuote, err := svc.QuoteTaskBilling("user", oneReference)
	if err != nil {
		t.Fatal(err)
	}
	if oneReferenceQuote.PerTaskAmountMicrocredits != 120_000 || oneReferenceQuote.UsageAdjustment == nil || oneReferenceQuote.UsageAdjustment.BillableQuantity != 0 {
		t.Fatalf("one-reference quote = %#v", oneReferenceQuote)
	}
	if quote.QuoteFingerprint == oneReferenceQuote.QuoteFingerprint {
		t.Fatal("changing reference-image count preserved the quote fingerprint")
	}
}

func TestQuoteTaskBillingRejectsNegativeReferenceImageCount(t *testing.T) {
	item := model.ChannelModel{
		ID: "image", ChannelID: "channel", ModelKey: "image", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true,
	}
	svc, _ := newBillingQuoteTestService(t, item)
	_, err := svc.QuoteTaskBilling("user", TaskBillingQuoteRequest{
		Type: "canvas_image", BatchCount: 1,
		Input: TaskBillingQuoteInput{Mode: "image", ReferenceImageCount: -1, Config: TaskBillingQuoteConfig{
			ChannelID: item.ChannelID, Model: item.ModelKey,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "参考图片数量") {
		t.Fatalf("error = %v", err)
	}
}

func TestQuoteTaskBillingPricesFixedVideoBatchWithoutWalletWrites(t *testing.T) {
	item := model.ChannelModel{
		ID: "video-fixed", ChannelID: "channel", ModelKey: "video-fixed", Capability: "video",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 1_000_000, PriceConfigured: true, Enabled: true, PriceVersion: 7,
	}
	svc, db := newBillingQuoteTestService(t, item)

	quote, err := svc.QuoteTaskBilling("user", TaskBillingQuoteRequest{
		Type: "canvas_video", Operation: "generate", BatchCount: 4,
		Input: TaskBillingQuoteInput{Mode: "video", Config: TaskBillingQuoteConfig{
			ChannelID: "channel", Model: item.ModelKey, VideoSeconds: "6", VideoQuality: "720p",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.PerTaskAmountMicrocredits != 1_000_000 || quote.AmountMicrocredits != 4_000_000 || quote.TaskCount != 4 || quote.Quantity != 1 {
		t.Fatalf("fixed video quote = %#v", quote)
	}
	if quote.PriceVersion != 7 || quote.BillingMode != "fixed_request" || quote.QuoteFingerprint == "" {
		t.Fatalf("fixed video quote facts = %#v", quote)
	}
	var orderCount int64
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	var ledgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if orderCount != 0 || ledgerCount != 0 {
		t.Fatalf("read-only quote wrote orders=%d ledgers=%d", orderCount, ledgerCount)
	}
}

func TestQuoteTaskBillingPricesImageResolutionBatch(t *testing.T) {
	item := model.ChannelModel{
		ID: "image", ChannelID: "channel", ModelKey: "gpt-image-2", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		PriceConfigured: true, Enabled: true, PriceVersion: 3,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "image-1k", Resolution: "1K", UnitPriceMicrocredits: 398_000},
			{ID: "image-2k", Resolution: "2K", UnitPriceMicrocredits: 803_000},
		},
	}
	svc, _ := newBillingQuoteTestService(t, item)

	quote, err := svc.QuoteTaskBilling("user", TaskBillingQuoteRequest{
		Type: "canvas_image", Operation: "generate", BatchCount: 3,
		Input: TaskBillingQuoteInput{Mode: "image", Config: TaskBillingQuoteConfig{
			ChannelID: "channel", Model: item.ModelKey, Size: "2048x2048", Quality: "medium",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.PerTaskAmountMicrocredits != 803_000 || quote.AmountMicrocredits != 2_409_000 || quote.TaskCount != 3 {
		t.Fatalf("image quote = %#v", quote)
	}
	if quote.PricingResolution != "2K" || quote.Quantity != 1 {
		t.Fatalf("image pricing facts = %#v", quote)
	}
}

func TestQuoteTaskBillingPricesSeedanceReferenceVideoPerSecond(t *testing.T) {
	item := model.ChannelModel{
		ID: "seedance", ChannelID: "channel", ModelKey: "doubao-seedance-2-5-260628", Capability: "video",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "per_second", PriceStrategy: "video_resolution",
		PriceConfigured: true, Enabled: true, PriceVersion: 5,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "standard", Resolution: "720P", InputVariant: "standard", UnitPriceMicrocredits: 1_510_000},
			{ID: "reference", Resolution: "720P", InputVariant: "reference_video", UnitPriceMicrocredits: 1_630_000},
		},
	}
	svc, _ := newBillingQuoteTestService(t, item)

	quote, err := svc.QuoteTaskBilling("user", TaskBillingQuoteRequest{
		Type: "canvas_video", Operation: "generate", BatchCount: 2,
		Input: TaskBillingQuoteInput{Mode: "video", ReferenceVideoCount: 1, Config: TaskBillingQuoteConfig{
			ChannelID: "channel", Model: item.ModelKey, VideoSeconds: "6", VideoQuality: "720p",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if quote.PerTaskAmountMicrocredits != 9_780_000 || quote.AmountMicrocredits != 19_560_000 {
		t.Fatalf("reference video quote = %#v", quote)
	}
	if quote.PricingInputVariant != "reference_video" || quote.PricingResolution != "720P" || quote.Quantity != 6 {
		t.Fatalf("reference video pricing facts = %#v", quote)
	}
}

func TestQuoteTaskBillingRejectsInvalidBatchAndUnavailablePricing(t *testing.T) {
	priced := model.ChannelModel{
		ID: "priced", ChannelID: "channel", ModelKey: "priced", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "image_resolution",
		PriceConfigured: true, Enabled: true,
		PriceTiers: []model.ChannelModelPriceTier{{ID: "one", Resolution: "1K", UnitPriceMicrocredits: 45_000}},
	}
	unpriced := model.ChannelModel{
		ID: "unpriced", ChannelID: "channel", ModelKey: "unpriced", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "flat",
		PriceConfigured: false, Enabled: true,
	}
	memberOnly := model.ChannelModel{
		ID: "member", ChannelID: "channel", ModelKey: "member", Capability: "image",
		AccessPolicy: model.ModelAccessMember, BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 1, PriceConfigured: true, Enabled: true,
	}
	svc, _ := newBillingQuoteTestService(t, priced, unpriced, memberOnly)

	base := TaskBillingQuoteRequest{Type: "canvas_image", Operation: "generate", Input: TaskBillingQuoteInput{
		Mode: "image", Config: TaskBillingQuoteConfig{ChannelID: "channel", Model: priced.ModelKey, Size: "1024x1024"},
	}}
	for _, batchCount := range []int64{0, 16} {
		request := base
		request.BatchCount = batchCount
		if _, err := svc.QuoteTaskBilling("user", request); err == nil || !strings.Contains(err.Error(), "生成数量") {
			t.Fatalf("batchCount %d error = %v", batchCount, err)
		}
	}

	missingTier := base
	missingTier.BatchCount = 1
	missingTier.Input.Config.Size = "4096x4096"
	if _, err := svc.QuoteTaskBilling("user", missingTier); err == nil || !strings.Contains(err.Error(), "分辨率") {
		t.Fatalf("missing tier error = %v", err)
	}

	unpricedRequest := base
	unpricedRequest.BatchCount = 1
	unpricedRequest.Input.Config.Model = unpriced.ModelKey
	if _, err := svc.QuoteTaskBilling("user", unpricedRequest); err == nil || !strings.Contains(err.Error(), "积分价格") {
		t.Fatalf("unpriced model error = %v", err)
	}

	memberRequest := base
	memberRequest.BatchCount = 1
	memberRequest.Input.Config.Model = memberOnly.ModelKey
	if _, err := svc.QuoteTaskBilling("user", memberRequest); err == nil || !strings.Contains(err.Error(), "会员") {
		t.Fatalf("member-only model error = %v", err)
	}
}

func TestQuoteTaskBillingIncludesSuperResolutionAndStableChargedFactFingerprint(t *testing.T) {
	item := model.ChannelModel{
		ID: "video", ChannelID: "channel", ModelKey: "video", Capability: "video",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "per_second", PriceStrategy: "video_resolution",
		PriceConfigured: true, Enabled: true, PriceVersion: 8,
		PriceTiers: []model.ChannelModelPriceTier{
			{ID: "video-480", Resolution: "480P", UnitPriceMicrocredits: 200_000},
			{ID: "video-720", Resolution: "720P", UnitPriceMicrocredits: 300_000},
		},
	}
	svc, db := newBillingQuoteTestService(t, item)
	rule := model.SuperResolutionPricingRule{
		ID: "sr", Edition: "professional", Resolution: "4K", FPSMinExclusive: 30, FPSMaxInclusive: 60,
		UnitPriceMicrocredits: 50_000, PriceConfigured: true, Enabled: true, PriceVersion: 2,
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	request := TaskBillingQuoteRequest{
		Type: "canvas_video", Operation: "generate", BatchCount: 2,
		Input: TaskBillingQuoteInput{Mode: "video", Config: TaskBillingQuoteConfig{
			ChannelID: "channel", Model: item.ModelKey, VideoSeconds: "4", VideoQuality: "720p",
			SuperResolutionEnabled: true, SuperResolutionResolution: "4K",
			SuperResolutionVersion: "professional", SuperResolutionFramesPerSecond: 60,
		}},
	}

	first, err := svc.QuoteTaskBilling("user", request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.QuoteTaskBilling("user", request)
	if err != nil {
		t.Fatal(err)
	}
	if first.PerTaskAmountMicrocredits != 1_400_000 || first.AmountMicrocredits != 2_800_000 || first.EnhancementAmountMicrocredits != 400_000 {
		t.Fatalf("super-resolution quote = %#v", first)
	}
	if first.QuoteFingerprint == "" || first.QuoteFingerprint != second.QuoteFingerprint {
		t.Fatalf("stable fingerprints = %q / %q", first.QuoteFingerprint, second.QuoteFingerprint)
	}

	changed := request
	changed.Input.Config.VideoQuality = "480p"
	changedQuote, err := svc.QuoteTaskBilling("user", changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedQuote.QuoteFingerprint == first.QuoteFingerprint {
		t.Fatal("charged resolution change preserved the quote fingerprint")
	}

	differentBatch := request
	differentBatch.BatchCount = 3
	differentBatchQuote, err := svc.QuoteTaskBilling("user", differentBatch)
	if err != nil {
		t.Fatal(err)
	}
	if differentBatchQuote.QuoteFingerprint != first.QuoteFingerprint {
		t.Fatal("batch aggregation changed the single-task quote fingerprint")
	}
}

func TestCreateTaskRejectsChangedMediaQuoteBeforeCommercialWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "quote-create.db")+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{
		ID: "channel", Scope: model.ChannelScopeSystem, Enabled: true,
		InterfaceType: model.ChannelInterfaceOpenAIImage,
	}
	item := model.ChannelModel{
		ID: "image", ChannelID: channel.ID, ModelKey: "gpt-image-2", Capability: "image",
		AccessPolicy: model.ModelAccessAuthenticated, BillingMode: "fixed_request", PriceStrategy: "flat",
		UnitPriceMicrocredits: 100_000, PriceConfigured: true, Enabled: true, PriceVersion: 1,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.CreditAccount{UserID: "user", AvailableMicrocredits: 10_000_000}).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	quoteRequest := TaskBillingQuoteRequest{
		Type: "canvas_image", Operation: "generate", BatchCount: 1,
		Input: TaskBillingQuoteInput{Mode: "image", Config: TaskBillingQuoteConfig{
			ChannelID: channel.ID, Model: item.ModelKey, Size: "1024x1024", Quality: "low",
		}},
	}
	oldQuote, err := svc.QuoteTaskBilling("user", quoteRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", item.ID).Updates(map[string]any{
		"unit_price_microcredits": 120_000,
		"price_version":           2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	createRequest := CreateTaskRequest{
		Type: "canvas_image", Operation: "generate", Prompt: "a product photo",
		QuotePriceVersion: oldQuote.PriceVersion, QuoteFingerprint: oldQuote.QuoteFingerprint,
		Input: map[string]any{
			"mode": "image",
			"config": map[string]any{
				"channelId": channel.ID, "model": item.ModelKey, "size": "1024x1024", "quality": "low", "count": "1",
			},
		},
	}
	_, err = svc.CreateTask("user", createRequest)
	var changed *QuoteChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("CreateTask() error = %v, want QuoteChangedError", err)
	}
	if changed.CurrentQuote.PriceVersion != 2 || changed.CurrentQuote.PerTaskAmountMicrocredits != 120_000 {
		t.Fatalf("current quote = %#v", changed.CurrentQuote)
	}
	var taskCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	var orderCount int64
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	var ledgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || ledgerCount != 0 {
		t.Fatalf("quote conflict wrote tasks=%d orders=%d ledgers=%d", taskCount, orderCount, ledgerCount)
	}

	current := changed.CurrentQuote
	createRequest.QuotePriceVersion = current.PriceVersion
	createRequest.QuoteFingerprint = current.QuoteFingerprint
	task, err := svc.CreateTask("user", createRequest)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.BillingOrderID == "" {
		t.Fatalf("created task = %#v", task)
	}
	var order model.BillingOrder
	if err := db.First(&order, "id = ?", task.BillingOrderID).Error; err != nil {
		t.Fatal(err)
	}
	if order.AmountMicrocredits != 120_000 || order.PriceVersion != 2 {
		t.Fatalf("reserved order = %#v", order)
	}
}
