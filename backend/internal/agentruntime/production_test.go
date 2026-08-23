package agentruntime

import (
	"strings"
	"testing"
)

func TestProductionPlanAllowsVideoOnlyDeliverable(t *testing.T) {
	draft := productionPlanDraftForDeliverableTest([]ProductionShotDeliverable{ProductionShotDeliverableVideoClip})
	draft.Shots[0].VideoPrompt = "原创抽象光影，镜头缓慢推进"

	if err := draft.Validate(); err != nil {
		t.Fatalf("video-only production plan validation error = %v", err)
	}
}

func TestProductionPlanAllowsStoryboardOnlyAndDualDeliverables(t *testing.T) {
	tests := []struct {
		name         string
		deliverables []ProductionShotDeliverable
		imagePrompt  string
		videoPrompt  string
	}{
		{
			name: "storyboard only", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableStoryboardImage},
			imagePrompt: "蓝色光带在黑色空间中汇聚",
		},
		{
			name: "storyboard and video",
			deliverables: []ProductionShotDeliverable{
				ProductionShotDeliverableStoryboardImage,
				ProductionShotDeliverableVideoClip,
			},
			imagePrompt: "蓝色光带在黑色空间中汇聚",
			videoPrompt: "镜头缓慢推进，光带逐渐消散",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := productionPlanDraftForDeliverableTest(test.deliverables)
			draft.Shots[0].ImagePrompt = test.imagePrompt
			draft.Shots[0].VideoPrompt = test.videoPrompt
			if err := draft.Validate(); err != nil {
				t.Fatalf("production plan validation error = %v", err)
			}
		})
	}
}

func TestProductionPlanRejectsInvalidDeliverableContracts(t *testing.T) {
	tests := []struct {
		name          string
		deliverables  []ProductionShotDeliverable
		imagePrompt   string
		videoPrompt   string
		referenceKeys []string
		wantError     string
	}{
		{name: "missing deliverable", wantError: "deliverables are invalid"},
		{
			name: "duplicate deliverable",
			deliverables: []ProductionShotDeliverable{
				ProductionShotDeliverableVideoClip,
				ProductionShotDeliverableVideoClip,
			},
			videoPrompt: "镜头推进", wantError: "deliverable video_clip is duplicated",
		},
		{
			name: "unknown deliverable", deliverables: []ProductionShotDeliverable{"audio_clip"},
			wantError: "deliverable audio_clip is invalid",
		},
		{
			name: "video prompt missing", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableVideoClip},
			wantError: "video prompt does not match deliverables",
		},
		{
			name: "video plan has unused image prompt", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableVideoClip},
			imagePrompt: "不应生成的分镜图", videoPrompt: "镜头推进", wantError: "image prompt does not match deliverables",
		},
		{
			name: "video plan has whitespace image prompt", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableVideoClip},
			imagePrompt: "  ", videoPrompt: "镜头推进", wantError: "image prompt does not match deliverables",
		},
		{
			name: "storyboard prompt missing", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableStoryboardImage},
			wantError: "image prompt does not match deliverables",
		},
		{
			name: "storyboard plan has unused video prompt", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableStoryboardImage},
			imagePrompt: "分镜图", videoPrompt: "不应生成的视频", wantError: "video prompt does not match deliverables",
		},
		{
			name: "storyboard plan has whitespace video prompt", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableStoryboardImage},
			imagePrompt: "分镜图", videoPrompt: "  ", wantError: "video prompt does not match deliverables",
		},
		{
			name: "video plan ignores references", deliverables: []ProductionShotDeliverable{ProductionShotDeliverableVideoClip},
			videoPrompt: "镜头推进", referenceKeys: []string{"hero"}, wantError: "references require storyboard_image",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			draft := productionPlanDraftForDeliverableTest(test.deliverables)
			draft.Shots[0].ImagePrompt = test.imagePrompt
			draft.Shots[0].VideoPrompt = test.videoPrompt
			draft.Shots[0].ReferenceKeys = test.referenceKeys
			if len(test.referenceKeys) > 0 {
				draft.References = []ReferenceAssetDraft{{
					ReferenceKey: "hero", Role: "character", Title: "主角", ImagePrompt: "主角参考图",
				}}
			}
			err := draft.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("production plan validation error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func productionPlanDraftForDeliverableTest(deliverables []ProductionShotDeliverable) ProductionPlanDraft {
	return ProductionPlanDraft{
		Title: "5秒原创抽象光影", TargetDurationMS: 5_000, Script: "抽象光带汇聚并消散。",
		Shots: []ShotPlanDraft{{
			ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "光带聚合",
			Deliverables: deliverables, Dependencies: []string{},
		}},
	}
}
