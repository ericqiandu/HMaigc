package service

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestUserAssetSummariesExposeMediaPreviewURLs(t *testing.T) {
	svc := newAssetDeletionTestService(t)
	now := time.Now().UTC()
	fixtures := []model.Asset{
		{
			ID: "image-asset", UserID: "user-1", Kind: "image", Title: "分镜图",
			PayloadJSON: `{"coverUrl":"","data":{"dataUrl":"/api/resources/image-1/file?direct=1"}}`,
			CreatedAt:   now, UpdatedAt: now,
		},
		{
			ID: "video-asset", UserID: "user-1", Kind: "video", Title: "成片",
			PayloadJSON: `{"kind":"video","coverUrl":"/api/resources/poster-1/file?direct=1","data":{"url":"/api/resources/video-1/file?direct=1"}}`,
			CreatedAt:   now, UpdatedAt: now,
		},
	}
	for index := range fixtures {
		if err := svc.repo.UpsertAsset(&fixtures[index]); err != nil {
			t.Fatal(err)
		}
	}

	summaries, err := svc.UserAssetSummaries("user-1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	var response []struct {
		ID         string `json:"id"`
		PreviewURL string `json:"previewUrl"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	previewByID := make(map[string]string, len(response))
	for _, summary := range response {
		previewByID[summary.ID] = summary.PreviewURL
	}
	if previewByID["image-asset"] != "/api/resources/image-1/file?direct=1" {
		t.Fatalf("image preview URL = %q", previewByID["image-asset"])
	}
	if previewByID["video-asset"] != "/api/resources/poster-1/file?direct=1" {
		t.Fatalf("video preview URL = %q", previewByID["video-asset"])
	}
}
