package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeImagePromptSpecialistClient struct {
	requests []imagePromptSpecialistRequestV1
	failure  error
}

func (client *fakeImagePromptSpecialistClient) Execute(_ context.Context, request imagePromptSpecialistRequestV1) (imagePromptSpecialistExecution, error) {
	client.requests = append(client.requests, request)
	if client.failure != nil {
		return imagePromptSpecialistExecution{}, client.failure
	}
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	response := imagePromptSpecialistResponseFixture(request, now)
	return imagePromptSpecialistExecution{Response: response, ResponseDigest: strings.Repeat("9", 64)}, nil
}

func TestDecodeCanvasImagePromptCommandRejectsPromptAndUnknownFields(t *testing.T) {
	valid := map[string]interface{}{
		"imagePromptCommand": map[string]interface{}{
			"schemaVersion": "canvas-image-command/v1",
			"command":       "portraitTexture",
			"objective":     "优化人像质感",
			"parameters":    map[string]interface{}{"preserveIdentity": true},
			"sourceNodeId":  "source-1",
			"nodeRevision":  float64(3),
		},
		"mode":   "image",
		"prompt": "",
	}
	command, present, err := decodeCanvasImagePromptCommand(valid)
	if err != nil || !present || command.Command != imagePromptCommandPortraitTexture {
		t.Fatalf("decode valid command = %#v, %v, %v", command, present, err)
	}

	withPrompt := cloneTaskInput(t, valid)
	withPrompt["prompt"] = "前端拼接的最终执行提示词"
	if _, _, err := decodeCanvasImagePromptCommand(withPrompt); err == nil {
		t.Fatal("specialist command accepted browser final prompt")
	}

	withUnknown := cloneTaskInput(t, valid)
	withUnknown["imagePromptCommand"].(map[string]interface{})["fallbackPrompt"] = "legacy"
	if _, _, err := decodeCanvasImagePromptCommand(withUnknown); err == nil {
		t.Fatal("specialist command accepted an unknown field")
	}
}

func TestResolveCanvasImageTaskPromptAllowsOnlyServerResolvedCommandObjective(t *testing.T) {
	prompt, err := resolveCanvasImageTaskPrompt("", "优化人像质感", true)
	if err != nil || prompt != "优化人像质感" {
		t.Fatalf("command prompt = %q, %v", prompt, err)
	}
	if _, err := resolveCanvasImageTaskPrompt("浏览器最终提示词", "优化人像质感", true); err == nil {
		t.Fatal("structured command accepted browser prompt")
	}
	if _, err := resolveCanvasImageTaskPrompt("", "", false); err == nil {
		t.Fatal("ordinary task accepted empty prompt")
	}
}

func TestPrepareCanvasImagePromptSpecialistFreezesPromptEvidenceIdempotently(t *testing.T) {
	svc, db, task, input := newImagePromptSpecialistTaskFixture(t)
	client := &fakeImagePromptSpecialistClient{}
	svc.imagePromptSpecialistClient = client
	svc.imagePromptSpecialistEvidenceResolver = func(model.Task, model.BillingOrder, canvasGenerationInput) (imagePromptSpecialistEvidenceV1, error) {
		return imagePromptSpecialistEvidenceV1{References: []imagePromptSpecialistReferenceV1{{ResourceID: "resource-1", MediaType: "image", SemanticRole: "source", Ordinal: 1, ContentDigest: strings.Repeat("1", 64), AccessURL: "https://cdn.example.com/source.png"}}}, nil
	}

	evidence, err := svc.prepareCanvasImagePromptSpecialist(context.Background(), task, input)
	if err != nil {
		t.Fatalf("prepare specialist: %v", err)
	}
	if input.Prompt != evidence.Response.ImagePrompt || input.Prompt == task.Prompt {
		t.Fatalf("provider prompt = %q, task label = %q", input.Prompt, task.Prompt)
	}
	if len(client.requests) != 1 {
		t.Fatalf("specialist calls = %d", len(client.requests))
	}
	request := client.requests[0]
	if request.CommercialContext.GenerationTaskID != task.ID || request.CommercialContext.BillingOrderID != task.BillingOrderID || request.Scope.NodeID != "target-1" || request.Scope.NodeRevision != 3 {
		t.Fatalf("request commercial/scope facts = %#v", request)
	}
	if request.PromptModel.CatalogRecordID != "prompt-model-1" || request.PromptModel.ModelKey != "gpt-5.5" {
		t.Fatalf("prompt model = %#v", request.PromptModel)
	}

	input.Prompt = ""
	if _, err := svc.prepareCanvasImagePromptSpecialist(context.Background(), task, input); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	var count int64
	if err := db.Model(&model.Result{}).Where("task_id = ? AND kind = ?", task.ID, imagePromptEvidenceResultKind).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("evidence result count = %d", count)
	}
	var stored model.Result
	if err := db.Where("task_id = ? AND kind = ?", task.ID, imagePromptEvidenceResultKind).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored.Payload, evidence.Response.ImagePrompt) || !strings.Contains(stored.Payload, evidence.ResponseDigest) {
		t.Fatalf("stored evidence = %s", stored.Payload)
	}
}

func TestPrepareCanvasImagePromptSpecialistFailsBeforeProviderPrompt(t *testing.T) {
	svc, db, task, input := newImagePromptSpecialistTaskFixture(t)
	svc.imagePromptSpecialistClient = &fakeImagePromptSpecialistClient{failure: &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "gateway down"}}
	svc.imagePromptSpecialistEvidenceResolver = func(model.Task, model.BillingOrder, canvasGenerationInput) (imagePromptSpecialistEvidenceV1, error) {
		return imagePromptSpecialistEvidenceV1{References: []imagePromptSpecialistReferenceV1{{ResourceID: "resource-1", MediaType: "image", SemanticRole: "source", Ordinal: 1, ContentDigest: strings.Repeat("1", 64), AccessURL: "https://cdn.example.com/source.png"}}}, nil
	}
	_, err := svc.prepareCanvasImagePromptSpecialist(context.Background(), task, input)
	var failure *imagePromptSpecialistFailure
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorUnavailable {
		t.Fatalf("prepare error = %#v", err)
	}
	if input.Prompt != "" {
		t.Fatalf("provider prompt must remain empty after failure: %q", input.Prompt)
	}
	var count int64
	if err := db.Model(&model.Result{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failure persisted success evidence count = %d", count)
	}
}

func TestImagePromptSpecialistFailureProvesBillableProviderWasNotStarted(t *testing.T) {
	timeout := &imagePromptSpecialistFailure{Code: imagePromptErrorTimeout, Reason: "internal specialist timeout"}
	if !billableProviderDispatchNotStarted(fmt.Errorf("prepare image prompt: %w", timeout)) {
		t.Fatal("wrapped specialist failure lost its pre-provider dispatch evidence")
	}
	if billableProviderDispatchNotStarted(errors.New("image provider timeout")) {
		t.Fatal("untyped provider failure was incorrectly classified as pre-dispatch")
	}
}

func TestPrepareCanvasImagePromptSpecialistRejectsChangedFrozenModel(t *testing.T) {
	svc, _, task, input := newImagePromptSpecialistTaskFixture(t)
	client := &fakeImagePromptSpecialistClient{}
	svc.imagePromptSpecialistClient = client
	input.PromptModel.ConfigurationRevision = "2026-09-07T11:00:00Z"
	_, err := svc.prepareCanvasImagePromptSpecialist(context.Background(), task, input)
	var failure *imagePromptSpecialistFailure
	if !errors.As(err, &failure) || failure.Code != imagePromptErrorModelBindingInvalid {
		t.Fatalf("prepare error = %#v", err)
	}
	if len(client.requests) != 0 {
		t.Fatal("changed model binding reached specialist")
	}
}

func TestAttachImagePromptEvidenceCannotDiscardSuccessfulProviderResult(t *testing.T) {
	evidence := &imagePromptTaskEvidenceV1{SchemaVersion: imagePromptResponseVersion}
	providerResult := map[string]interface{}{
		"images":              []interface{}{map[string]interface{}{"dataUrl": "data:image/png;base64,AA=="}},
		"imagePromptEvidence": "untrusted-provider-field",
	}

	attached, err := attachImagePromptEvidence(providerResult, evidence)
	if err != nil {
		t.Fatalf("attach evidence after provider success: %v", err)
	}
	if attached["imagePromptEvidence"] != evidence {
		t.Fatalf("trusted evidence was not attached: %#v", attached["imagePromptEvidence"])
	}
	if _, exists := attached["images"]; !exists {
		t.Fatal("successful provider asset was discarded while attaching evidence")
	}
}

func newImagePromptSpecialistTaskFixture(t *testing.T) (*Service, *gorm.DB, model.Task, *canvasGenerationInput) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.ProviderAccount{}, &model.ProviderEndpointVersion{}, &model.ProviderCredential{}, &model.ProviderCredentialVersion{}, &model.BillingOrder{}, &model.Task{}, &model.Result{}, &model.TaskLog{}); err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	account := model.ProviderAccount{ID: "prompt-account", ProviderKind: kuaiziProviderKind, Name: "Prompt Provider", Enabled: true, CreatedAt: updatedAt, UpdatedAt: updatedAt}
	endpoint := model.ProviderEndpointVersion{ID: "prompt-endpoint-v1", ProviderAccountID: account.ID, BaseURL: "https://aiopenapi.kuaizi.cn", Status: "active", Version: 1, CreatedAt: updatedAt}
	credential := model.ProviderCredential{ID: "prompt-credential", ProviderAccountID: account.ID, Family: kuaiziAccountCredentialFamily, Enabled: true, HealthStatus: "healthy", CreatedAt: updatedAt, UpdatedAt: updatedAt}
	version := model.ProviderCredentialVersion{ID: "prompt-credential-v1", ProviderCredentialID: credential.ID, KeyCipher: "encrypted", Status: "active", Version: 1, CreatedAt: updatedAt}
	promptModel := model.ChannelModel{ID: "prompt-model-1", ChannelID: deterministicKuaiziChatChannelID("gpt"), ProviderCredentialID: credential.ID, ModelKey: "gpt-5.5", Capability: "text", Enabled: true, UpdatedAt: updatedAt}
	for _, row := range []interface{}{&account, &endpoint, &credential, &version} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&promptModel).Error; err != nil {
		t.Fatal(err)
	}
	order := model.BillingOrder{ID: "bill-1", UserID: "user-1", TaskID: "task-1", ChannelID: "image-channel", ChannelModelID: "image-model-record", Model: "gpt-image-2", Capability: "image", BillingMode: "fixed_request", PriceVersion: 7, Quantity: 1, AmountMicrocredits: 100, Status: model.BillingStatusReserved}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "task-1", UserID: "user-1", ProjectID: "canvas-1", Type: "canvas_image", Status: model.TaskStatusRunning, Prompt: "优化人像质感", BillingOrderID: order.ID, InputJSON: `{}`, CreatedAt: updatedAt, UpdatedAt: updatedAt}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{repo: repository.New(db)}
	binding := imagePromptSpecialistModelBindingV1{CatalogRecordID: promptModel.ID, ModelKey: promptModel.ModelKey, ConfigurationRevision: updatedAt.Format(time.RFC3339Nano), ProviderEndpointVersionID: endpoint.ID, ProviderCredentialVersionID: version.ID}
	command := &canvasImagePromptCommandV1{SchemaVersion: canvasImageCommandVersion, Command: imagePromptCommandPortraitTexture, Objective: "优化人像质感", Parameters: map[string]interface{}{"preserveIdentity": true}, SourceNodeID: "source-1", NodeRevision: 3}
	input := &canvasGenerationInput{Mode: "image", Prompt: "", ImagePromptCommand: command, PromptModel: &binding, ReferenceImages: []providerMedia{{ID: "resource-1", StorageKey: "resource:resource-1"}}, Metadata: map[string]interface{}{"nodeId": "target-1"}}
	return svc, db, task, input
}

func cloneTaskInput(t *testing.T, input map[string]interface{}) map[string]interface{} {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
