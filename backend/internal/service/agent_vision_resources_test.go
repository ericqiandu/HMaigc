package service

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const testWebPBase64 = "UklGRioAAABXRUJQVlA4IB4AAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA"

func TestAgentVisionResourcesAcceptAuthorizedSourcesAndPreserveRequestOrder(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	imageBytes := encodeVisionTestImage(t, "png", 12, 8)
	attachment := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-attachment", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, imageBytes)
	canvas := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-canvas", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, imageBytes)
	project := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-project", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, imageBytes)

	payload := fmt.Sprintf(`{"nodes":[{"id":"image-node","type":"image","metadata":{"storageKey":"resource:%s","content":"/api/resources/%s/file","status":"success"}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`, canvas.ID, canvas.ID)
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	createVisionTestProjectAsset(t, db, "runtime-project", project)

	configuration := agentruntime.RunConfiguration{Attachments: []agentruntime.ResourceAttachment{{
		ResourceID: attachment.ID, Name: "附件.png", Kind: "image", MIMEType: "image/png", SizeBytes: attachment.Size,
	}}}
	resources, err := svc.agentVisionResourcesForRun(
		agentRuntimeServiceScope(), configuration, []string{project.ID, attachment.ID, canvas.ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 3 || resources[0].Resource.ID != project.ID || resources[1].Resource.ID != attachment.ID || resources[2].Resource.ID != canvas.ID {
		t.Fatalf("ordered vision resources = %#v", resources)
	}
	for _, resource := range resources {
		if resource.Width != 12 || resource.Height != 8 || resource.Fact.SizeBytes != int64(len(imageBytes)) || resource.Resource.Size != int64(len(imageBytes)) {
			t.Fatalf("probed vision resource = %#v", resource)
		}
	}
}

func TestAgentVisionResourcesDecodeSupportedImageHeadersWithoutDimensionMetadata(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	fixtures := []struct {
		id       string
		format   string
		mimeType string
		data     []byte
	}{
		{id: "vision-jpeg", format: "jpeg", mimeType: "image/jpeg", data: encodeVisionTestImage(t, "jpeg", 7, 5)},
		{id: "vision-png", format: "png", mimeType: "image/png", data: encodeVisionTestImage(t, "png", 7, 5)},
		{id: "vision-gif", format: "gif", mimeType: "image/gif", data: encodeVisionTestImage(t, "gif", 7, 5)},
		{id: "vision-webp", format: "webp", mimeType: "image/webp", data: decodeVisionTestBase64(t, testWebPBase64)},
	}
	configuration := agentruntime.RunConfiguration{}
	resourceIDs := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		resource := createLocalVisionTestResource(t, svc, db, model.Resource{
			ID: fixture.id, UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: fixture.mimeType,
		}, fixture.data)
		configuration.Attachments = append(configuration.Attachments, agentruntime.ResourceAttachment{
			ResourceID: resource.ID, Name: fixture.format, Kind: "image", MIMEType: fixture.mimeType, SizeBytes: resource.Size,
		})
		resourceIDs = append(resourceIDs, resource.ID)
	}
	resources, err := svc.agentVisionResourcesForRun(agentRuntimeServiceScope(), configuration, resourceIDs)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != len(fixtures) {
		t.Fatalf("supported image resources = %#v", resources)
	}
	for _, resource := range resources {
		if resource.Width < 1 || resource.Height < 1 {
			t.Fatalf("image dimensions were not decoded = %#v", resource)
		}
	}
}

func TestAgentVisionResourcesRejectUnauthorizedOrInvalidFacts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *Service, *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string)
	}{
		{name: "duplicate ids", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-duplicate", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			configuration := visionAttachmentConfiguration(resource)
			return agentRuntimeServiceScope(), configuration, []string{resource.ID, resource.ID}
		}},
		{name: "cross tenant", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-foreign", UserID: "other-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "not ready", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-pending", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusPending, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "not image", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-audio", UserID: "runtime-user", Kind: "audio", Status: model.ResourceStatusReady, MimeType: "audio/wav"}, encodeVisionTestImage(t, "png", 2, 2))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "spoofed canvas binding", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-spoofed-canvas", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			payload := fmt.Sprintf(`{"nodes":[{"id":"image-node","type":"image","metadata":{"storageKey":"resource:%s","content":"https://attacker.example/image.png","status":"success"}}],"connections":[],"viewport":{"x":0,"y":0,"k":1}}`, resource.ID)
			if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("payload_json", payload).Error; err != nil {
				t.Fatal(err)
			}
			return agentRuntimeServiceScope(), agentruntime.RunConfiguration{}, []string{resource.ID}
		}},
		{name: "mime mismatch", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-mime-mismatch", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/jpeg"}, encodeVisionTestImage(t, "png", 2, 2))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "unsupported mime", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-bmp", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/bmp"}, []byte("BM-not-an-accepted-image"))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "size limit", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-too-large", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Update("size", int64(32*1024*1024+1)).Error; err != nil {
				t.Fatal(err)
			}
			resource.Size = 32*1024*1024 + 1
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "dimension limit", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-too-wide", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 8193, 1))
			return agentRuntimeServiceScope(), visionAttachmentConfiguration(resource), []string{resource.ID}
		}},
		{name: "deleted project link", setup: func(t *testing.T, svc *Service, db *gorm.DB) (agentruntime.Scope, agentruntime.RunConfiguration, []string) {
			resource := createLocalVisionTestResource(t, svc, db, model.Resource{ID: "vision-deleted-link", UserID: "runtime-user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png"}, encodeVisionTestImage(t, "png", 2, 2))
			linkID := createVisionTestProjectAsset(t, db, "runtime-project", resource)
			if err := db.Delete(&model.ProjectAssetLink{}, "id = ?", linkID).Error; err != nil {
				t.Fatal(err)
			}
			return agentRuntimeServiceScope(), agentruntime.RunConfiguration{}, []string{resource.ID}
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
			scope, configuration, resourceIDs := testCase.setup(t, svc, db)
			if _, err := svc.agentVisionResourcesForRun(scope, configuration, resourceIDs); err == nil {
				t.Fatal("invalid vision resource facts were accepted")
			}
		})
	}
}

func TestAgentVisionResourcesUseExactTeamTenant(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeTeamMembership(t, db, "runtime-vision-team", 1_000)
	if err := db.Model(&model.Project{}).Where("id = ?", "runtime-project").Update("team_id", "runtime-vision-team").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", "runtime-canvas").Update("team_id", "runtime-vision-team").Error; err != nil {
		t.Fatal(err)
	}
	resource := createLocalVisionTestResource(t, svc, db, model.Resource{
		ID: "vision-team-resource", UserID: "runtime-user", TeamID: "runtime-vision-team", Kind: "image",
		Status: model.ResourceStatusReady, MimeType: "image/png",
	}, encodeVisionTestImage(t, "png", 4, 3))
	configuration := visionAttachmentConfiguration(resource)
	personalScope := agentRuntimeServiceScope()
	if _, err := svc.agentVisionResourcesForRun(personalScope, configuration, []string{resource.ID}); err == nil {
		t.Fatal("personal scope accepted team vision resource")
	}
	teamScope := agentRuntimeServiceScope()
	teamScope.TenantKind = agentruntime.TenantTeam
	teamScope.TenantID = resource.TeamID
	resources, err := svc.agentVisionResourcesForRun(teamScope, configuration, []string{resource.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Resource.TeamID != resource.TeamID {
		t.Fatalf("team vision resources = %#v", resources)
	}
}

func visionAttachmentConfiguration(resource model.Resource) agentruntime.RunConfiguration {
	return agentruntime.RunConfiguration{Attachments: []agentruntime.ResourceAttachment{{
		ResourceID: resource.ID, Name: resource.ID, Kind: resource.Kind, MIMEType: resource.MimeType, SizeBytes: resource.Size,
	}}}
}

func createLocalVisionTestResource(t *testing.T, svc *Service, db *gorm.DB, resource model.Resource, data []byte) model.Resource {
	t.Helper()
	resource.Provider = "local"
	resource.ObjectKey = filepath.ToSlash(filepath.Join("vision-tests", resource.ID+".bin"))
	resource.Size = int64(len(data))
	resource.CreatedAt = time.Now().UTC()
	resource.UpdatedAt = resource.CreatedAt
	filePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resource.ObjectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	return resource
}

func createVisionTestProjectAsset(t *testing.T, db *gorm.DB, projectID string, resource model.Resource) string {
	t.Helper()
	now := time.Now().UTC()
	assetID := "asset-" + resource.ID
	versionID := "version-" + resource.ID
	linkID := "link-" + resource.ID
	asset := model.Asset{ID: assetID, UserID: resource.UserID, Kind: "image", Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: versionID, Title: resource.ID, CreatedAt: now, UpdatedAt: now}
	version := model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: model.AssetVersionStatusConfirmed, CreatedAt: now, UpdatedAt: now}
	representation := model.AssetRepresentation{ID: "representation-" + resource.ID, TaskID: "task-" + resource.ID, AssetVersionID: versionID, ResourceID: resource.ID, MediaType: "image", Role: "primary", CreatedAt: now}
	link := model.ProjectAssetLink{ID: linkID, ProjectID: projectID, AssetID: assetID, CreatedAt: now}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&version).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&representation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	return linkID
}

func encodeVisionTestImage(t *testing.T, format string, width int, height int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: 20, G: 80, B: 160, A: 255})
		}
	}
	var output bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 80})
	case "png":
		err = png.Encode(&output, canvas)
	case "gif":
		err = gif.Encode(&output, canvas, nil)
	default:
		t.Fatalf("unsupported test image format %q", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func decodeVisionTestBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
