package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnrichAPICallLogCountsAsyncImageOutputs(t *testing.T) {
	log := model.ApiCallLog{
		Capability: "image",
		Status:     model.ApiCallStatusSucceeded,
		StatusCode: 200,
	}

	(&Service{}).EnrichAPICallLog(&log, []byte(`{
		"code": 0,
		"data": {
			"task_id": "provider-task-1",
			"status": "succeeded",
			"image_urls": ["https://assets.example.com/result.jpg"]
		}
	}`))

	if log.MediaCount != 1 {
		t.Fatalf("media count = %d, want 1", log.MediaCount)
	}
}

func TestEstimateCallCostChargesAsyncImageUsageAtSuccessfulOutputOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ModelPricing{}, &model.ModelPricingTier{}); err != nil {
		t.Fatal(err)
	}
	pricing := model.ModelPricing{
		ID: "pricing", ChannelID: "channel", Model: "async-image", Capability: "image", Currency: "CNY", PerMediaMicros: 600_000,
		Tiers: []model.ModelPricingTier{{
			ID: "input-overage", Specification: "INPUT_IMAGE", UsageMetric: inputImageUsageMetric,
			IncludedQuantity: 1, SupplierCostMicros: 20_000,
		}},
	}
	if err := db.Create(&pricing).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	logs := []model.ApiCallLog{
		{ChannelID: "channel", Model: "async-image", Capability: "image", Status: model.ApiCallStatusSucceeded, Billable: true, RequestKind: "create", InputImageCount: 3},
		{ChannelID: "channel", Model: "async-image", Capability: "image", Status: model.ApiCallStatusSucceeded, RequestKind: "poll", InputImageCount: 3},
		{ChannelID: "channel", Model: "async-image", Capability: "image", Status: model.ApiCallStatusSucceeded, RequestKind: "poll", InputImageCount: 3, MediaCount: 1},
	}
	wantCosts := []int64{0, 0, 640_000}
	var total int64
	for index := range logs {
		svc.estimateCallCost(&logs[index])
		if !logs[index].CostAvailable || logs[index].EstimatedCostMicros != wantCosts[index] || logs[index].CostCalculationError != "" {
			t.Fatalf("log %d cost = %#v, want %d", index, logs[index], wantCosts[index])
		}
		total += logs[index].EstimatedCostMicros
	}
	if total != 640_000 {
		t.Fatalf("total cost = %d, want 640000", total)
	}
}
