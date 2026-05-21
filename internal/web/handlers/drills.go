package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/assertions"
	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
	"github.com/preshotcome/anything/internal/evidence"
	"github.com/preshotcome/anything/internal/web/templates"
)

// --- /databases ---

func (h *Handlers) targetsList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	targets, err := h.drills.ListTargets(r.Context(), lc.Account.ID)
	if err != nil {
		render(w, r, templates.TargetsError("Could not load databases."))
		return
	}
	render(w, r, templates.TargetsPage(lc, targets))
}

func (h *Handlers) targetNewPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.TargetNewForm(h.layoutCtx(r), templates.TargetFormValues{}, ""))
}

func (h *Handlers) targetCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	u, acct := lc.User, lc.Account
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	values := templates.TargetFormValues{
		Name:      strings.TrimSpace(r.PostFormValue("name")),
		SourceURI: strings.TrimSpace(r.PostFormValue("source_uri")),
	}
	if values.Name == "" || values.SourceURI == "" {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.TargetNewForm(lc, values, "Name and source path are required."))
		return
	}
	if _, err := os.Stat(values.SourceURI); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.TargetNewForm(lc, values, "Source file not found: "+values.SourceURI))
		return
	}

	t, err := h.drills.CreateTarget(r.Context(), drill.Target{
		AccountID:       acct.ID,
		CreatedByUserID: u.ID,
		Name:            values.Name,
		SourceKind:      "postgres_dump_local",
		SourceURI:       values.SourceURI,
	})
	if err != nil {
		http.Error(w, "create target: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "target.created",
		TargetKind: "database_target", TargetID: t.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	// Land on the detail page so the next step — adding assertions — is right
	// in front of the user.
	http.Redirect(w, r, "/databases/"+t.ID.String(), http.StatusSeeOther)
}

func (h *Handlers) targetDetail(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := h.drills.GetTarget(r.Context(), lc.Account.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	asserts, _ := h.drills.ListTargetAssertions(r.Context(), t.ID)
	render(w, r, templates.TargetDetail(lc, t, asserts, ""))
}

func (h *Handlers) assertionCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	u, acct := lc.User, lc.Account
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := h.drills.GetTarget(r.Context(), acct.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind := strings.TrimSpace(r.PostFormValue("kind"))
	cfg, errMsg := buildAssertionConfig(kind,
		strings.TrimSpace(r.PostFormValue("table")),
		strings.TrimSpace(r.PostFormValue("column")),
		strings.TrimSpace(r.PostFormValue("min_rows")))
	if errMsg != "" {
		asserts, _ := h.drills.ListTargetAssertions(r.Context(), t.ID)
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.TargetDetail(lc, t, asserts, errMsg))
		return
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "encode config", http.StatusInternalServerError)
		return
	}
	if _, err := h.drills.CreateAssertion(r.Context(), t.ID, kind, raw); err != nil {
		http.Error(w, "create assertion: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "assertion.created",
		TargetKind: "database_target", TargetID: t.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/databases/"+t.ID.String(), http.StatusSeeOther)
}

func (h *Handlers) assertionDelete(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	u, acct := lc.User, lc.Account
	targetID := chi.URLParam(r, "id")
	assertionID, err := uuid.Parse(chi.URLParam(r, "assertion_id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.drills.DeleteAssertion(r.Context(), acct.ID, assertionID); err != nil &&
		!errors.Is(err, drill.ErrNotFound) {
		http.Error(w, "delete assertion", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "assertion.deleted",
		TargetKind: "database_target", TargetID: targetID,
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/databases/"+targetID, http.StatusSeeOther)
}

// buildAssertionConfig turns the assertion form fields into a validated config
// map for the given kind. Returns a user-facing error string on bad input.
func buildAssertionConfig(kind, table, column, minRowsInput string) (map[string]any, string) {
	if !assertions.ValidKind(kind) {
		return nil, "Pick an assertion kind."
	}
	cfg := map[string]any{"table": table}
	switch kind {
	case assertions.KindRowCount:
		minRows := 1
		if minRowsInput != "" {
			n, err := atoiInRange(minRowsInput, 0, 1_000_000_000)
			if err != nil {
				return nil, "Minimum rows must be a non-negative integer."
			}
			minRows = n
		}
		cfg["min_rows"] = minRows
	case assertions.KindColumnExists, assertions.KindNoNulls:
		cfg["column"] = column
	}
	if err := assertions.ValidateConfig(kind, cfg); err != nil {
		return nil, "Table and column must be valid SQL identifiers."
	}
	return cfg, ""
}

// --- /drills ---

func (h *Handlers) drillsList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	ds, err := h.drills.ListDrills(r.Context(), lc.Account.ID, 100)
	if err != nil {
		render(w, r, templates.DrillsErrorPage(lc, "Could not load drills."))
		return
	}
	targets, _ := h.drills.ListTargets(r.Context(), lc.Account.ID)
	idemKey := uuid.NewString()
	render(w, r, templates.DrillsPage(lc, ds, targets, idemKey))
}

func (h *Handlers) drillCreate(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	idem := strings.TrimSpace(r.PostFormValue("idempotency_key"))
	targetIDStr := strings.TrimSpace(r.PostFormValue("target_id"))
	if idem == "" {
		http.Error(w, "idempotency_key is required", http.StatusBadRequest)
		return
	}
	targetID, err := uuid.Parse(targetIDStr)
	if err != nil {
		http.Error(w, "invalid target", http.StatusBadRequest)
		return
	}

	// Target must belong to the current account.
	target, err := h.drills.GetTarget(r.Context(), acct.ID, targetID)
	if err != nil {
		http.Error(w, "target not found", http.StatusNotFound)
		return
	}

	drillID, reused, err := h.drills.CreateDrillIdempotent(r.Context(), acct.ID, u.ID, target.ID, idem)
	if err != nil {
		http.Error(w, "create drill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !reused {
		if err := h.orch.EnqueueDrill(r.Context(), drillID); err != nil {
			http.Error(w, "enqueue: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, "/drills/"+drillID.String(), http.StatusSeeOther)
}

func (h *Handlers) drillDetail(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), lc.Account.ID, drillID)
	if err != nil {
		if errors.Is(err, drill.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "load drill", http.StatusInternalServerError)
		return
	}
	target, _ := h.drills.GetTargetByID(r.Context(), dr.TargetID)
	steps, _ := h.drills.ListSteps(r.Context(), dr.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), dr.ID)

	// Re-verify the evidence signature on every detail view so the page
	// shows a live tamper-check, not a cached claim.
	var verify evidence.VerifyResult
	if dr.EvidencePath != nil && *dr.EvidencePath != "" {
		verify, _ = h.evidence.Verify(r.Context(), dr.ID, *dr.EvidencePath)
	}
	render(w, r, templates.DrillDetail(lc, dr, target, steps, ars, verify))
}

func (h *Handlers) drillStepsPartial(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), acct.ID, drillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	steps, _ := h.drills.ListSteps(r.Context(), dr.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), dr.ID)

	if dr.Status == drill.StatusSucceeded || dr.Status == drill.StatusFailed {
		w.Header().Set("HX-Trigger", "drill-terminal")
	}
	render(w, r, templates.DrillStepsPartial(dr, steps, ars))
}

func (h *Handlers) drillEvidence(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), acct.ID, drillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dr.EvidencePath == nil || *dr.EvidencePath == "" {
		http.Error(w, "evidence not yet generated", http.StatusNotFound)
		return
	}
	f, err := h.evidence.Open(r.Context(), *dr.EvidencePath)
	if err != nil {
		http.Error(w, "open evidence: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "evidence.downloaded",
		TargetKind: "drill", TargetID: dr.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="restore-drill-`+dr.ID.String()+`.pdf"`)
	// Read into memory so we can ServeContent with a seekable reader; drill
	// PDFs are small (single page).
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read evidence: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func atoiInRange(s string, lo, hi int) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
		if n > hi {
			return 0, errors.New("too large")
		}
	}
	if n < lo {
		return 0, errors.New("too small")
	}
	return n, nil
}
