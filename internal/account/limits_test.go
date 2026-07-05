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
		// Active trials get ONE real database and unlimited seats — walk-
		// through pricing gives them the daily-cadence experience of Growth
		// so they see what they'd upgrade to.
		{PlanTrial, Limits{Databases: 1, Seats: Unlimited, APIKeys: 2, Webhooks: 2, Heartbeats: 3,
			DrillsPerDay: 5, MaxDumpBytes: 5 * gb}},
		{PlanStarter, Limits{Databases: 5, Seats: Unlimited, APIKeys: 10, Webhooks: 10, Heartbeats: 25,
			DrillsPerDay: 30, MaxDumpBytes: 50 * gb}},
		{PlanPro, Limits{Databases: 25, Seats: Unlimited, APIKeys: Unlimited,
			Webhooks: Unlimited, Heartbeats: 50,
			DrillsPerDay: 150, MaxDumpBytes: 500 * gb}},
		// Grounded (DB value 'scale') is the top self-serve tier. Databases
		// capped at 100; drills/dump size still carry hard ceilings so a
		// single tenant can't starve the shared queue.
		{PlanScale, Limits{Databases: 100, Seats: Unlimited, APIKeys: Unlimited,
			Webhooks: Unlimited, Heartbeats: Unlimited,
			DrillsPerDay: 500, MaxDumpBytes: 2 * 1024 * gb}},
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
		{PlanTrial, "daily", false}, // trial stays weekly — daily is the reason to convert
		{PlanStarter, "monthly", true},
		{PlanStarter, "weekly", true},
		{PlanStarter, "daily", true}, // daily is the paid baseline now
		{PlanPro, "weekly", true},
		{PlanPro, "daily", true},     // Growth includes daily
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
	if got := TopCadence(PlanStarter); got != "daily" {
		t.Errorf("TopCadence(starter) = %q, want daily", got)
	}
	if got := TopCadence(PlanPro); got != "daily" {
		t.Errorf("TopCadence(pro) = %q, want daily", got)
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
