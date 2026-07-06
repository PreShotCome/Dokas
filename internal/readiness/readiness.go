// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package readiness turns a database's drill history into a single
// recovery-readiness score — a glanceable answer to "if I had to restore this
// right now, how confident should I be?". It is pure and dependency-free so it
// is trivially testable; callers supply the aggregated stats.
package readiness

import "time"

// Stat is the aggregated drill history for one database that the score is
// computed from.
type Stat struct {
	Cadence       string     // "off" | "monthly" | "weekly" | "daily" | "hourly"
	LastSuccessAt *time.Time // most recent passing drill, nil if none ever passed
	LastStatus    string     // status of the most recent terminal drill ("" = none)
	Recent        int        // terminal drills in the scoring window
	RecentPassed  int        // of those, how many passed
}

// Score is the verdict for a database.
type Score struct {
	Value   int    // 0–100
	Grade   string // A–F
	Label   string // "Ready" | "At risk" | "Failing" | "Unverified"
	Tone    string // "green" | "amber" | "red" — drives the badge colour
	Summary string // one-line human explanation
}

// cadenceInterval is the expected gap between drills for a cadence. "off" has
// no schedule, so we hold it to a 30-day freshness bar rather than nothing.
func cadenceInterval(cadence string) time.Duration {
	switch cadence {
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default: // "off" / unknown
		return 30 * 24 * time.Hour
	}
}

// Compute scores a database from its stats. The score blends three signals:
// freshness of the last passing drill against the expected cadence (0–60),
// the recent pass rate (0–30), and the most recent drill's outcome (0–10).
func Compute(s Stat, now time.Time) Score {
	// Never drilled — unverified, the worst case for a backup product.
	if s.LastSuccessAt == nil && s.LastStatus == "" {
		return Score{Value: 0, Grade: "F", Label: "Unverified", Tone: "red",
			Summary: "No drill has run yet — this backup is unverified."}
	}

	interval := cadenceInterval(s.Cadence)

	// Freshness: full marks within one interval of the last success, decaying
	// to zero by three intervals. No passing drill at all scores zero here.
	freshness := 0.0
	if s.LastSuccessAt != nil {
		ratio := float64(now.Sub(*s.LastSuccessAt)) / float64(interval)
		switch {
		case ratio <= 1:
			freshness = 60
		case ratio >= 3:
			freshness = 0
		default:
			freshness = 60 * (3 - ratio) / 2
		}
	}

	// Pass rate over the recent window (neutral 15/30 when there's no history).
	passRate := 15.0
	if s.Recent > 0 {
		passRate = 30 * float64(s.RecentPassed) / float64(s.Recent)
	}

	// Most recent outcome.
	last := 5.0
	switch s.LastStatus {
	case "succeeded":
		last = 10
	case "failed":
		last = 0
	}

	value := int(freshness + passRate + last + 0.5)
	if value > 100 {
		value = 100
	}
	return decorate(value, s, now, interval)
}

// decorate attaches the grade, label, tone, and a summary to a raw score.
func decorate(value int, s Stat, now time.Time, interval time.Duration) Score {
	var grade, label, tone string
	switch {
	case value >= 90:
		grade, label, tone = "A", "Ready", "green"
	case value >= 80:
		grade, label, tone = "B", "Ready", "green"
	case value >= 70:
		grade, label, tone = "C", "At risk", "amber"
	case value >= 60:
		grade, label, tone = "D", "At risk", "amber"
	default:
		grade, label, tone = "F", "Failing", "red"
	}

	summary := "Backup verified recently."
	switch {
	case s.LastStatus == "failed":
		summary = "The most recent drill failed — investigate before you rely on this backup."
		tone, label = "red", "Failing"
	case s.LastSuccessAt == nil:
		summary = "No passing drill on record yet."
	case now.Sub(*s.LastSuccessAt) > 3*interval:
		summary = "Last verified " + humanizeAgo(now.Sub(*s.LastSuccessAt)) + " ago — overdue for a drill."
	case now.Sub(*s.LastSuccessAt) > interval:
		summary = "Last verified " + humanizeAgo(now.Sub(*s.LastSuccessAt)) + " ago — a fresh drill is due."
	}
	return Score{Value: value, Grade: grade, Label: label, Tone: tone, Summary: summary}
}

func humanizeAgo(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "under an hour"
	case d < 48*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return itoa(h) + " hours"
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day"
		}
		return itoa(days) + " days"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
