package model

import (
	"testing"
	"time"
)

func TestRelativeAgeCoversAllRanges(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name       string
		createdUTC float64
		want       string
	}{
		{name: "missing", createdUTC: 0, want: "unknown"},
		{name: "negative", createdUTC: -1, want: "unknown"},
		{name: "future", createdUTC: float64(now.Add(time.Hour).Unix()), want: "0m ago"},
		{name: "minutes", createdUTC: float64(now.Add(-30 * time.Minute).Unix()), want: "30m ago"},
		{name: "hours", createdUTC: float64(now.Add(-2 * time.Hour).Unix()), want: "2h ago"},
		{name: "days", createdUTC: float64(now.Add(-3 * 24 * time.Hour).Unix()), want: "3d ago"},
		{name: "weeks", createdUTC: float64(now.Add(-2 * 7 * 24 * time.Hour).Unix()), want: "2w ago"},
		{name: "months", createdUTC: float64(now.Add(-60 * 24 * time.Hour).Unix()), want: "2mo ago"},
		{name: "years", createdUTC: float64(now.Add(-2 * 365 * 24 * time.Hour).Unix()), want: "2y ago"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := RelativeAge(test.createdUTC, now); got != test.want {
				t.Fatalf("RelativeAge(%v) = %q, want %q", test.createdUTC, got, test.want)
			}
		})
	}
}
