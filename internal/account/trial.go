// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package account

import "time"

// TrialDuration is how long a new account has full access before it must
// subscribe. Kept here for reference — the window is set in SQL at account
// creation (CreatePersonalAccount) and on the migration backfill.
const TrialDuration = 14 * 24 * time.Hour

// TrialActive reports whether a trial-plan account is still inside its
// full-access window. A trial account with no end date is now treated as
// lapsed (fail-closed) — the trial backfill migration always sets a date,
// so a missing value is a bug we'd rather catch as a paywall than silently
// extend access forever. Unlimited accounts never have a trial to speak of.
func TrialActive(a Account) bool {
	if a.Unlimited {
		return false
	}
	return a.Plan == PlanTrial && a.TrialEndsAt != nil && time.Now().Before(*a.TrialEndsAt)
}

// TrialLapsed reports whether a trial account's window has closed without it
// subscribing. Writes should be blocked for a lapsed account. A trial
// account with NO end date counts as lapsed for the same fail-closed
// reason. Unlimited accounts are never treated as lapsed.
func TrialLapsed(a Account) bool {
	if a.Unlimited {
		return false
	}
	if a.Plan != PlanTrial {
		return false
	}
	return a.TrialEndsAt == nil || time.Now().After(*a.TrialEndsAt)
}

// TrialDaysLeft is the whole days remaining in the trial, rounded up and
// never negative. Returns 0 for paid plans and lapsed trials.
func TrialDaysLeft(a Account) int {
	if a.Plan != PlanTrial || a.TrialEndsAt == nil {
		return 0
	}
	remaining := time.Until(*a.TrialEndsAt)
	if remaining <= 0 {
		return 0
	}
	days := int(remaining / (24 * time.Hour))
	if remaining%(24*time.Hour) > 0 {
		days++
	}
	return days
}
