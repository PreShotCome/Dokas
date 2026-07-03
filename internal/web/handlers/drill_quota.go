package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/riverqueue/river"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/drill"
)

// errDrillQuotaExceeded is the marker returned by enforceDrillQuota so callers
// (web / v1 / sample paths) render the plan-appropriate response.
var errDrillQuotaExceeded = errors.New("drill quota exceeded")

// drillQuotaError is the caller-friendly version — includes the numbers so the
// error message is actionable ("5 of 5 drills used today").
type drillQuotaError struct {
	Plan   string
	Limit  int
	Window time.Duration
}

func (e *drillQuotaError) Error() string {
	return fmt.Sprintf("the %s plan is limited to %d drills per day", e.Plan, e.Limit)
}

// enforceDrillQuota counts drills created in the last 24h against the account's
// plan cap. Every create path (sample, web, v1, admin, scheduler) is expected
// to pass through it so a single tenant cannot monopolise the shared River
// queue by hammering /drills.
func enforceDrillQuota(ctx context.Context, drills *drill.Store, acct *account.Account) error {
	if acct == nil {
		return nil // no auth context — upstream handler will 401
	}
	limits := account.LimitsFor(acct.Plan)
	if limits.DrillsPerDay == account.Unlimited {
		return nil
	}
	since := time.Now().Add(-24 * time.Hour)
	n, err := drills.CountDrillsSince(ctx, acct.ID, since)
	if err != nil {
		// Count failure — fail open so a transient DB blip does not deny a
		// paying customer's drill. Log at the caller.
		return err
	}
	if n >= limits.DrillsPerDay {
		return &drillQuotaError{Plan: string(acct.Plan), Limit: limits.DrillsPerDay, Window: 24 * time.Hour}
	}
	return nil
}

// asDrillQuotaError returns the typed quota error when err is one, so handlers
// can render the specific 429 / paywall response.
func asDrillQuotaError(err error) (*drillQuotaError, bool) {
	var qe *drillQuotaError
	if errors.As(err, &qe) {
		return qe, true
	}
	return nil, false
}

// errDumpTooLarge is the marker returned by enforceDumpSize when the on-disk
// source exceeds the account's plan cap.
var errDumpTooLarge = errors.New("dump exceeds plan size limit")

// dumpTooLargeError names the actual + allowed sizes so the message is
// actionable.
type dumpTooLargeError struct {
	Plan          string
	ActualBytes   int64
	AllowedBytes  int64
}

func (e *dumpTooLargeError) Error() string {
	return fmt.Sprintf("that dump is %s — the %s plan accepts up to %s per drill",
		humanBytes(e.ActualBytes), e.Plan, humanBytes(e.AllowedBytes))
}

// enforceDumpSize stats the local dump source and rejects if it exceeds the
// plan's MaxDumpBytes. Called at create time — before enqueue — so an
// oversized dump never burns the 30-minute restore timeout and then gets
// retried by River. postgres_dump_local sources are always local files; other
// source kinds skip this check today.
func enforceDumpSize(sourceKind, sourceURI string, plan account.Plan) error {
	if sourceKind != "postgres_dump_local" || sourceURI == "" {
		return nil
	}
	limits := account.LimitsFor(plan)
	if limits.MaxDumpBytes <= 0 {
		return nil
	}
	fi, err := os.Stat(sourceURI)
	if err != nil {
		// Missing / unreadable source is a distinct failure — leave it to the
		// runner's fetch step, which produces a better-targeted error.
		return nil
	}
	total := fi.Size()
	if fi.IsDir() {
		// pg_dump -Fd directory archive: sum the top-level regular file sizes.
		entries, _ := os.ReadDir(sourceURI)
		total = 0
		for _, e := range entries {
			if info, err := e.Info(); err == nil && !info.IsDir() {
				total += info.Size()
			}
		}
	}
	if total > limits.MaxDumpBytes {
		return &dumpTooLargeError{Plan: string(plan), ActualBytes: total, AllowedBytes: limits.MaxDumpBytes}
	}
	return nil
}

// asDumpTooLargeError returns the typed size error when err is one.
func asDumpTooLargeError(err error) (*dumpTooLargeError, bool) {
	var de *dumpTooLargeError
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

func humanBytes(n int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
		tb = int64(1) << 40
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.1f TB", float64(n)/float64(tb))
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// drillInsertOpts returns River insert options for a drill's jobs, priority
// keyed off the account's plan: paid plans preempt trial jobs when workers
// are scarce. Without this, one trial account hammering /databases/sample-drill
// can starve every paying customer on the shared 8-worker queue.
func drillInsertOpts(plan account.Plan) *river.InsertOpts {
	// River priorities are 1 (highest) through 4 (lowest); the scheduler
	// picks lower priority numbers first from the queue.
	priority := 2
	switch plan {
	case account.PlanScale:
		priority = 1
	case account.PlanPro:
		priority = 1
	case account.PlanStarter:
		priority = 2
	case account.PlanTrial:
		priority = 4
	}
	return &river.InsertOpts{Priority: priority}
}

