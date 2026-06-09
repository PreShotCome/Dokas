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
	tests := []struct {
		plan Plan
		want Limits
	}{
		{PlanTrial, Limits{Databases: 10, Seats: 10, APIKeys: 5, Webhooks: 5, Heartbeats: 20}},
		{PlanStarter, Limits{Databases: 10, Seats: 10, APIKeys: 5, Webhooks: 5, Heartbeats: 20}},
		{PlanPro, Limits{}},
		{Plan("garbage"), Limits{Databases: 1, Seats: 2, APIKeys: 1, Webhooks: 1, Heartbeats: 1}},
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
		{PlanTrial, "daily", true},
		{PlanTrial, "hourly", false},
		{PlanStarter, "weekly", true},
		{PlanStarter, "daily", false},
		{PlanStarter, "hourly", false},
		{PlanPro, "daily", true},
		{PlanPro, "hourly", false},
		{Plan("garbage"), "daily", false},
	}
	for _, tc := range tests {
		if got := CadenceAllowed(tc.plan, tc.cadence); got != tc.want {
			t.Errorf("CadenceAllowed(%q,%q) = %v, want %v", tc.plan, tc.cadence, got, tc.want)
		}
	}
	if got := TopCadence(PlanTrial); got != "daily" {
		t.Errorf("TopCadence(trial) = %q, want daily", got)
	}
	if got := TopCadence(PlanStarter); got != "weekly" {
		t.Errorf("TopCadence(starter) = %q, want weekly", got)
	}
	if got := TopCadence(PlanPro); got != "daily" {
		t.Errorf("TopCadence(pro) = %q, want daily", got)
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
