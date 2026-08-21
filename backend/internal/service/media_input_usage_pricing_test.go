package service

import (
	"strings"
	"testing"
)

func TestCalculateMediaInputUsageAdjustment(t *testing.T) {
	tests := []struct {
		name     string
		actual   int64
		included int64
		unit     int64
		billable int64
		amount   int64
	}{
		{name: "no input", actual: 0, included: 1, unit: 20_000, billable: 0, amount: 0},
		{name: "inside allowance", actual: 1, included: 1, unit: 20_000, billable: 0, amount: 0},
		{name: "overage", actual: 4, included: 1, unit: 20_000, billable: 3, amount: 60_000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := calculateMediaInputUsageAdjustment(inputImageUsageMetric, test.actual, test.included, test.unit)
			if err != nil {
				t.Fatal(err)
			}
			if result.BillableQuantity != test.billable || result.Amount != test.amount {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestCalculateMediaInputUsageAdjustmentRejectsInvalidFacts(t *testing.T) {
	tests := []struct {
		name     string
		metric   string
		actual   int64
		included int64
		unit     int64
		want     string
	}{
		{name: "unsupported metric", metric: "audio_seconds", actual: 1, included: 0, unit: 1, want: "不支持"},
		{name: "negative actual", metric: inputImageUsageMetric, actual: -1, included: 0, unit: 1, want: "实际数量"},
		{name: "negative included", metric: inputImageUsageMetric, actual: 1, included: -1, unit: 1, want: "免费数量"},
		{name: "zero unit", metric: inputImageUsageMetric, actual: 1, included: 0, unit: 0, want: "单价"},
		{name: "overflow", metric: inputImageUsageMetric, actual: 1<<62 + 1, included: 0, unit: 4, want: "溢出"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := calculateMediaInputUsageAdjustment(test.metric, test.actual, test.included, test.unit)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want message containing %q", err, test.want)
			}
		})
	}
}
