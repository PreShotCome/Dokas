package handlers

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// reports renders the month-over-month drill reporting page. The lookback
// window is 12 months on Pro, 3 months on every other plan.
func (h *Handlers) reports(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	isPro := lc.Account.Plan == account.PlanPro
	months := 3
	if isPro {
		months = 12
	}
	// Snap to the first day of the (months-ago) calendar month — so a
	// 3-month window always returns exactly 3 month buckets, regardless of
	// the day-of-month the request fires on. AddDate(0,-N,0) from a wall
	// clock can return partial months on either edge (Feb-24 → May-24
	// yields four buckets, two of which are partial and incomparable).
	since := monthsAgoUTC(time.Now(), months)

	monthly, err := h.drills.MonthlyStats(r.Context(), lc.Account.ID, since)
	if err != nil {
		h.logger().Error("reports: monthly stats failed", "err", err, "account_id", lc.Account.ID)
		http.Error(w, "report unavailable — please try again in a moment", http.StatusInternalServerError)
		return
	}
	dbs, err := h.drills.DatabaseStats(r.Context(), lc.Account.ID, since)
	if err != nil {
		h.logger().Error("reports: database stats failed", "err", err, "account_id", lc.Account.ID)
		http.Error(w, "report unavailable — please try again in a moment", http.StatusInternalServerError)
		return
	}

	view := templates.ReportsView{
		Ctx:          lc,
		Months:       monthly,
		Databases:    dbs,
		WindowMonths: months,
		IsPro:        isPro,
	}
	for _, m := range monthly {
		view.TotalDrills += m.Total
		view.Succeeded += m.Succeeded
		view.Failed += m.Failed
	}
	render(w, r, templates.Reports(view))
}

// reportsExport streams the 12-month drill report as CSV. Pro only.
func (h *Handlers) reportsExport(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if lc.Account.Plan != account.PlanPro {
		http.Error(w, "CSV export is a Pro-plan feature", http.StatusForbidden)
		return
	}
	since := monthsAgoUTC(time.Now(), 12)
	monthly, err := h.drills.MonthlyStats(r.Context(), lc.Account.ID, since)
	if err != nil {
		http.Error(w, "report unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+branding.Slug+`-drill-report.csv"`)
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"month", "total_drills", "succeeded", "failed", "avg_duration_seconds"}); err != nil {
		h.logger().Error("csv header write failed", "err", err, "account_id", lc.Account.ID)
		return
	}
	for _, m := range monthly {
		if err := cw.Write([]string{
			m.Month.Format("2006-01"),
			strconv.Itoa(m.Total),
			strconv.Itoa(m.Succeeded),
			strconv.Itoa(m.Failed),
			strconv.FormatFloat(m.AvgSeconds, 'f', 1, 64),
		}); err != nil {
			h.logger().Error("csv row write failed", "err", err, "account_id", lc.Account.ID)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		h.logger().Error("csv flush failed — client likely got a truncated file",
			"err", err, "account_id", lc.Account.ID)
	}
}

// monthsAgoUTC returns the first UTC instant of the calendar month that is
// `months` before `now`'s month. It is the deterministic lower bound for a
// rolling N-month report: a 3-month window always yields exactly 3 month
// buckets, with no partial-month edges to distort month-over-month deltas.
func monthsAgoUTC(now time.Time, months int) time.Time {
	t := now.UTC()
	return time.Date(t.Year(), t.Month()-time.Month(months), 1, 0, 0, 0, 0, time.UTC)
}
