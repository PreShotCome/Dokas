package account

import (
	"testing"
	"time"
)

func TestTrialState(t *testing.T) {
	future := time.Now().Add(48 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)
	active := Account{Plan: PlanTrial, TrialEndsAt: &future}
	lapsed := Account{Plan: PlanTrial, TrialEndsAt: &past}
	paid := Account{Plan: PlanStarter, TrialEndsAt: &past}

	if !TrialActive(active) || TrialLapsed(active) {
		t.Error("future trial should be active, not lapsed")
	}
	if TrialActive(lapsed) || !TrialLapsed(lapsed) {
		t.Error("past trial should be lapsed, not active")
	}
	if TrialActive(paid) || TrialLapsed(paid) {
		t.Error("a paid account is neither in nor past trial")
	}
	if got := TrialDaysLeft(active); got != 2 {
		t.Errorf("TrialDaysLeft = %d, want 2", got)
	}
	if got := TrialDaysLeft(lapsed); got != 0 {
		t.Errorf("TrialDaysLeft(lapsed) = %d, want 0", got)
	}
}

func TestLimitsFor(t *testing.T) {
	const gb = int64(1) << 30
	tests := []struct {
		plan Plan
		want Limits
	}{
		// Active trials get ONE real database + a small team footprint. The
		// point of the trial is to prove the product on the user's own dump
		// before they subscribe, not to host a production fleet for free.
		{PlanTrial, Limits{Databases: 1, Seats: 2, APIKeys: 2, Webhooks: 2, Heartbeats: 3,
			DrillsPerDay: 5, MaxDumpBytes: 5 * gb}},
		{PlanStarter, Limits{Databases: 5, Seats: 3, APIKeys: 3, Webhooks: 3, Heartbeats: 10,
			DrillsPerDay: 20, MaxDumpBytes: 20 * gb}},
		{PlanPro, Limits{Databases: 25, Seats: 10, APIKeys: 10, Webhooks: 10, Heartbeats: 25,
			DrillsPerDay: 100, MaxDumpBytes: 200 * gb}},
		// Scale is uncapped on Databases/Seats/etc but drills and dumps still
		// carry hard ceilings — protects the shared queue.
		{PlanScale, Limits{DrillsPerDay: 500, MaxDumpBytes: 1024 * gb}},
		{Plan("garbage"), Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1, Heartbeats: 1,
			DrillsPerDay: 2, MaxDumpBytes: 1 * gb}},
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
		{PlanTrial, "monthly", true},
		{PlanTrial, "weekly", true},
		{PlanTrial, "daily", false}, // daily is Scale-only now
		{PlanStarter, "monthly", true},
		{PlanStarter, "weekly", true},
		{PlanStarter, "daily", false},
		{PlanPro, "weekly", true},
		{PlanPro, "daily", false}, // Growth tops out at weekly
		{PlanScale, "daily", true},
		{PlanScale, "hourly", false}, // hourly is Enterprise/custom, not self-serve
		{Plan("garbage"), "daily", false},
	}
	for _, tc := range tests {
		if got := CadenceAllowed(tc.plan, tc.cadence); got != tc.want {
			t.Errorf("CadenceAllowed(%q,%q) = %v, want %v", tc.plan, tc.cadence, got, tc.want)
		}
	}
	if got := TopCadence(PlanTrial); got != "weekly" {
		t.Errorf("TopCadence(trial) = %q, want weekly", got)
	}
	if got := TopCadence(PlanStarter); got != "weekly" {
		t.Errorf("TopCadence(starter) = %q, want weekly", got)
	}
	if got := TopCadence(PlanPro); got != "weekly" {
		t.Errorf("TopCadence(pro) = %q, want weekly", got)
	}
	if got := TopCadence(PlanScale); got != "daily" {
		t.Errorf("TopCadence(scale) = %q, want daily", got)
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
