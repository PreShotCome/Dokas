package account

import "testing"

func TestLimitsFor(t *testing.T) {
	tests := []struct {
		plan Plan
		want Limits
	}{
		{PlanTrial, Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1}},
		{PlanStarter, Limits{Databases: 10, Seats: 10, APIKeys: 5, Webhooks: 5}},
		{PlanPro, Limits{}},
		{Plan("garbage"), Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1}},
	}
	for _, tc := range tests {
		if got := LimitsFor(tc.plan); got != tc.want {
			t.Errorf("LimitsFor(%q) = %+v, want %+v", tc.plan, got, tc.want)
		}
	}
}

func TestCadenceGating(t *testing.T) {
	tests := []struct {
		plan    Plan
		cadence string
		want    bool
	}{
		{PlanTrial, "off", true},
		{PlanTrial, "weekly", true},
		{PlanTrial, "daily", false},
		{PlanStarter, "daily", true},
		{PlanStarter, "hourly", false},
		{PlanPro, "hourly", true},
	}
	for _, tc := range tests {
		if got := CadenceAllowed(tc.plan, tc.cadence); got != tc.want {
			t.Errorf("CadenceAllowed(%q,%q) = %v, want %v", tc.plan, tc.cadence, got, tc.want)
		}
	}
	if got := TopCadence(PlanTrial); got != "weekly" {
		t.Errorf("TopCadence(trial) = %q, want weekly", got)
	}
	if got := TopCadence(PlanPro); got != "hourly" {
		t.Errorf("TopCadence(pro) = %q, want hourly", got)
	}
}

func TestAtLimit(t *testing.T) {
	tests := []struct {
		name         string
		count, limit int
		want         bool
	}{
		{"below cap", 0, 1, false},
		{"one below cap", 4, 5, false},
		{"at cap", 1, 1, true},
		{"over cap", 6, 5, true},
		{"unlimited never blocks", 9999, Unlimited, false},
	}
	for _, tc := range tests {
		if got := AtLimit(tc.count, tc.limit); got != tc.want {
			t.Errorf("%s: AtLimit(%d,%d) = %v, want %v",
				tc.name, tc.count, tc.limit, got, tc.want)
		}
	}
}
