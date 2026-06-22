package readiness

import (
	"testing"
	"time"
)

func TestCompute(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }

	tests := []struct {
		name      string
		stat      Stat
		wantGrade string
		wantTone  string
	}{
		{
			name:      "never drilled is unverified F",
			stat:      Stat{Cadence: "weekly"},
			wantGrade: "F", wantTone: "red",
		},
		{
			name:      "fresh weekly with perfect history is A",
			stat:      Stat{Cadence: "weekly", LastSuccessAt: ago(2 * 24 * time.Hour), LastStatus: "succeeded", Recent: 8, RecentPassed: 8},
			wantGrade: "A", wantTone: "green",
		},
		{
			name:      "last drill failed is red regardless of score",
			stat:      Stat{Cadence: "daily", LastSuccessAt: ago(24 * time.Hour), LastStatus: "failed", Recent: 10, RecentPassed: 9},
			wantTone:  "red",
			wantGrade: "",
		},
		{
			name:      "stale success on a weekly cadence drops the tone",
			stat:      Stat{Cadence: "weekly", LastSuccessAt: ago(40 * 24 * time.Hour), LastStatus: "succeeded", Recent: 4, RecentPassed: 4},
			wantTone:  "red", // ~5 intervals stale → freshness 0
			wantGrade: "",
		},
	}
	for _, tc := range tests {
		got := Compute(tc.stat, now)
		if tc.wantGrade != "" && got.Grade != tc.wantGrade {
			t.Errorf("%s: grade = %q, want %q (score=%d)", tc.name, got.Grade, tc.wantGrade, got.Value)
		}
		if got.Tone != tc.wantTone {
			t.Errorf("%s: tone = %q, want %q (score=%d, summary=%q)", tc.name, got.Tone, tc.wantTone, got.Value, got.Summary)
		}
		if got.Value < 0 || got.Value > 100 {
			t.Errorf("%s: score %d out of range", tc.name, got.Value)
		}
	}
}
