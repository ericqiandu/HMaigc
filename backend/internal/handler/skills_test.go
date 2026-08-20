package handler

import (
	"testing"

	"infinite-canvas/backend/internal/service"
)

func TestParsePositiveSkillQueryRejectsMalformedAndOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"", "abc", "0", "-1", "61"} {
		if _, err := parsePositiveSkillQuery(raw, "每页数量", 60); err == nil {
			t.Fatalf("invalid query value %q was accepted", raw)
		}
	}
	value, err := parsePositiveSkillQuery("60", "每页数量", 60)
	if err != nil || value != 60 {
		t.Fatalf("valid query value rejected: value=%d err=%v", value, err)
	}
}

func TestParsePositiveSkillQueryRejectsPageAboveCatalogLimit(t *testing.T) {
	if _, err := parsePositiveSkillQuery("1000001", "页码", service.SkillCatalogMaximumPage); err == nil {
		t.Fatal("page above catalog limit was accepted")
	}
}
