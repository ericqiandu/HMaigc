package agentruntime

import (
	"strings"
	"testing"
)

func TestProductionPlanDraftValidatesReferenceAssetsAndShotBindings(t *testing.T) {
	draft := ProductionPlanDraft{
		Title: "雨夜怀表", TargetDurationMS: 30_000, Script: "顾棠拨动怀表，时间短暂倒流。",
		References: []ReferenceAssetDraft{
			{ReferenceKey: "character", Role: "character", Title: "顾棠", ImagePrompt: "顾棠角色参考图"},
			{ReferenceKey: "costume", Role: "costume", Title: "顾棠服装", ImagePrompt: "墨绿色风衣服装参考图"},
			{ReferenceKey: "prop-watch", Role: "prop", Title: "月牙怀表", ImagePrompt: "黄铜月牙怀表参考图"},
			{ReferenceKey: "clock-shop", Role: "scene", Title: "钟表铺", ImagePrompt: "雨夜钟表铺场景参考图"},
		},
		Shots: []ShotPlanDraft{{
			ShotKey: "shot-1", Order: 1, DurationMS: 30_000, ScriptText: "顾棠在钟表铺拨动怀表。",
			ImagePrompt: "顾棠在钟表铺拿着月牙怀表", VideoPrompt: "镜头缓慢推进",
			ReferenceKeys: []string{"character", "costume", "prop-watch", "clock-shop"}, Dependencies: []string{},
		}},
	}
	if err := draft.Validate(); err != nil {
		t.Fatalf("valid reference plan rejected: %v", err)
	}

	draft.Shots[0].ReferenceKeys = append(draft.Shots[0].ReferenceKeys, "missing-reference")
	err := draft.Validate()
	if err == nil || !strings.Contains(err.Error(), "reference missing-reference is missing") {
		t.Fatalf("missing reference error = %v", err)
	}
}
