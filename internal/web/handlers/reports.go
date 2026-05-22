package handlers

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/preshotcome/anything/internal/account"
	"github.com/preshotcome/anything/internal/web/templates"
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
	since := time.Now().UTC().AddDate(0, -months, 0)

	monthly, _ := h.drills.MonthlyStats(r.Context(), lc.Account.ID, since)
	dbs, _ := h.drills.DatabaseStats(r.Context(), lc.Account.ID, since)

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
	since := time.Now().UTC().AddDate(0, -12, 0)
	monthly, err := h.drills.MonthlyStats(r.Context(), lc.Account.ID, since)
	if err != nil {
		http.Error(w, "report unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="soteria-drill-report.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"month", "total_drills", "succeeded", "failed", "avg_duration_seconds"})
	for _, m := range monthly {
		_ = cw.Write([]string{
			m.Month.Format("2006-01"),
			strconv.Itoa(m.Total),
			strconv.Itoa(m.Succeeded),
			strconv.Itoa(m.Failed),
			strconv.FormatFloat(m.AvgSeconds, 'f', 1, 64),
		})
	}
	cw.Flush()
}
