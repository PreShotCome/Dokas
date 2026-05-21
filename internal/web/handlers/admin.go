package handlers

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/report"
	"github.com/preshotcome/anything/internal/web/templates"
)

func (h *Handlers) adminHome(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.AdminHome(h.layoutCtx(r)))
}

// adminUserSearch looks up users by an email substring.
func (h *Handlers) adminUserSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var results []templates.AdminUser
	if q != "" {
		rows, err := h.pool.Query(r.Context(), `
			SELECT id, email::text, is_staff, created_at, deleted_at IS NOT NULL
			  FROM users
			 WHERE email ILIKE '%' || $1 || '%'
			 ORDER BY created_at DESC
			 LIMIT 50
		`, q)
		if err != nil {
			http.Error(w, "search: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var u templates.AdminUser
			if err := rows.Scan(&u.ID, &u.Email, &u.IsStaff, &u.CreatedAt, &u.Deleted); err != nil {
				http.Error(w, "scan: "+err.Error(), http.StatusInternalServerError)
				return
			}
			results = append(results, u)
		}
	}
	render(w, r, templates.AdminUsers(h.layoutCtx(r), q, results))
}

// adminUserDetail shows a user's accounts and the drills they've started.
func (h *Handlers) adminUserDetail(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, err := h.sessions.LoadUserByID(r.Context(), userID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	accounts, _ := h.accounts.ListAccountsForUser(r.Context(), userID)
	drills, _ := h.drills.ListDrillsByCreator(r.Context(), userID, 25)

	view := templates.AdminUserDetailView{
		Ctx:    h.layoutCtx(r),
		User:   templates.AdminUser{ID: target.ID, Email: target.Email, IsStaff: target.IsStaff, CreatedAt: target.CreatedAt},
		Drills: drills,
	}
	for _, a := range accounts {
		view.Accounts = append(view.Accounts, templates.AdminAccount{
			ID: a.ID, Name: a.Name, Role: string(a.Role),
		})
	}
	render(w, r, templates.AdminUserDetail(view))
}

// adminImpersonate starts a safe impersonation: a reason is required, the
// action is audited against the real staff actor, and the session swaps to
// the target user.
func (h *Handlers) adminImpersonate(w http.ResponseWriter, r *http.Request) {
	staff, _ := auth.FromContext(r.Context())
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(r.PostFormValue("reason"))
	if len(reason) < 3 {
		http.Error(w, "an impersonation reason (3+ chars) is required", http.StatusBadRequest)
		return
	}
	if userID == staff.ID {
		http.Error(w, "cannot impersonate yourself", http.StatusBadRequest)
		return
	}

	if err := h.sessions.StartImpersonation(r.Context(), r, staff.ID, userID); err != nil {
		http.Error(w, "impersonate: "+err.Error(), http.StatusConflict)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &staff.ID, Action: "staff.impersonation_started",
		TargetKind: "user", TargetID: userID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"reason": reason},
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// impersonateStop ends an impersonation. It lives outside the staff gate —
// the effective user mid-impersonation is the (non-staff) target.
func (h *Handlers) impersonateStop(w http.ResponseWriter, r *http.Request) {
	imp, ok := auth.ImpersonationFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if err := h.sessions.StopImpersonation(r.Context(), r); err != nil {
		http.Error(w, "stop: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &imp.StaffUserID, Action: "staff.impersonation_stopped",
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// adminDrillReplay re-runs a drill: it enqueues a fresh drill for the same
// target, attributed to the staff user.
func (h *Handlers) adminDrillReplay(w http.ResponseWriter, r *http.Request) {
	staff, _ := auth.FromContext(r.Context())
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrillByID(r.Context(), drillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	newID, _, err := h.drills.CreateDrillIdempotent(r.Context(), dr.AccountID, staff.ID, dr.TargetID, uuid.NewString())
	if err != nil {
		http.Error(w, "replay: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.orch.EnqueueDrill(r.Context(), newID); err != nil {
		http.Error(w, "enqueue: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &staff.ID, Action: "staff.drill_replayed",
		TargetKind: "drill", TargetID: drillID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"replay_drill_id": newID.String()},
	})
	http.Redirect(w, r, "/admin/drills/"+newID.String(), http.StatusSeeOther)
}

// adminDrillDetail is the staff cross-account drill view.
func (h *Handlers) adminDrillDetail(w http.ResponseWriter, r *http.Request) {
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrillByID(r.Context(), drillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, _ := h.drills.GetTargetByID(r.Context(), dr.TargetID)
	steps, _ := h.drills.ListSteps(r.Context(), dr.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), dr.ID)
	var verify evidence.VerifyResult
	if dr.EvidencePath != nil && *dr.EvidencePath != "" {
		verify, _ = h.evidence.Verify(r.Context(), dr.ID, *dr.EvidencePath)
	}
	render(w, r, templates.AdminDrillDetail(h.layoutCtx(r), dr, target, steps, ars, verify))
}

// adminEvidenceRegen re-renders and re-signs a drill's evidence PDF —
// recovery for a corrupted or lost evidence file.
func (h *Handlers) adminEvidenceRegen(w http.ResponseWriter, r *http.Request) {
	staff, _ := auth.FromContext(r.Context())
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrillByID(r.Context(), drillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target, _ := h.drills.GetTargetByID(r.Context(), dr.TargetID)
	steps, _ := h.drills.ListSteps(r.Context(), dr.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), dr.ID)

	var buf bytes.Buffer
	if err := report.Render(&buf, report.Data{
		Drill: dr, Target: target, Steps: steps, Assertions: ars,
		GeneratedAt: time.Now().UTC(),
	}); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
		return
	}
	path, err := h.evidence.Finalize(r.Context(), dr.ID, buf.Bytes())
	if err != nil {
		http.Error(w, "finalize: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := h.drills.MarkEvidence(r.Context(), dr.ID, path); err != nil {
		http.Error(w, "mark evidence: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		ActorID: &staff.ID, Action: "staff.evidence_regenerated",
		TargetKind: "drill", TargetID: drillID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/admin/drills/"+drillID.String(), http.StatusSeeOther)
}
