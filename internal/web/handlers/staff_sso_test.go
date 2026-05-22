package handlers

import (
	"testing"
	"time"
)

func TestStaffSSOFresh(t *testing.T) {
	now := time.Now()
	tp := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	tests := []struct {
		name string
		at   *time.Time
		want bool
	}{
		{"never verified", nil, false},
		{"just verified", tp(0), true},
		{"within window", tp(-30 * time.Minute), true},
		{"at window edge", tp(-staffSSOWindow), false},
		{"past window", tp(-2 * time.Hour), false},
		{"slight clock skew counts as fresh", tp(time.Minute), true},
	}
	for _, tc := range tests {
		if got := staffSSOFresh(tc.at, now); got != tc.want {
			t.Errorf("%s: staffSSOFresh = %v, want %v", tc.name, got, tc.want)
		}
	}
}
