package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/assertions"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/evidence"
	"github.com/preshotcome/dokaz/internal/readiness"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// --- /databases ---

func (h *Handlers) targetsList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	targets, err := h.drills.ListTargets(r.Context(), lc.Account.ID, h.databaseScope(r, lc))
	if err != nil {
		render(w, r, templates.TargetsError("Could not load databases."))
		return
	}
	render(w, r, templates.TargetsPage(lc, targets, h.readinessScores(r, lc.Account.ID, targets)))
}

// readinessScores computes the recovery-readiness score for each target,
// keyed by target ID string for the template. A stats-query failure is
// non-fatal — the page still renders, just without badges.
func (h *Handlers) readinessScores(r *http.Request, accountID uuid.UUID, targets []drill.Target) map[string]readiness.Score {
	stats, err := h.drills.ReadinessStats(r.Context(), accountID)
	if err != nil {
		h.logger().Warn("readiness stats", "err", err)
		return nil
	}
	now := time.Now()
	out := make(map[string]readiness.Score, len(targets))
	for _, t := range targets {
		if t.IsSample {
			continue // the demo dataset isn't a real backup to grade
		}
		st := stats[t.ID]
		out[t.ID.String()] = readiness.Compute(readiness.Stat{
			Cadence:       t.DrillCadence,
			LastSuccessAt: st.LastSuccessAt,
			LastStatus:    st.LastStatus,
			Recent:        st.Recent,
			RecentPassed:  st.RecentPassed,
		}, now)
	}
	return out
}

func (h *Handlers) targetNewPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.TargetNewForm(h.layoutCtx(r), templates.TargetFormValues{}, ""))
}

// resolveSourcePath validates a customer-supplied dump path. The path must
// resolve to an existing file *inside* the configured backups directory:
// confinement stops a target from pointing the drill runner at arbitrary
// server files (and the errors never echo the path, so they can't be used as
// a filesystem oracle). Returns the cleaned absolute path.
func (h *Handlers) resolveSourcePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("a source file path is required")
	}
	root, err := filepath.Abs(h.sourceDir)
	if err != nil {
		return "", errors.New("the server's backups directory is misconfigured")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", errors.New("that source path is not valid")
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("source files must live inside the server's backups directory")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", errors.New("no readable file was found at that path")
	}
	if info.IsDir() {
		// A directory is valid only as a pg_dump -Fd archive (has a toc.dat).
		if _, err := os.Stat(filepath.Join(abs, "toc.dat")); err != nil {
			return "", errors.New("the source directory is not a pg_dump -Fd archive")
		}
	}
	return abs, nil
}

func (h *Handlers) targetCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	u, acct := lc.User, lc.Account

	// Free/trial accounts may drill the sample dataset only — adding a real
	// backup requires a paid plan.
	if h.blockFreeRealWeb(w, r) {
		return
	}

	// Plan-limit counting spans the whole account regardless of team, so this
	// list is deliberately unscoped.
	existing, _ := h.drills.ListTargets(r.Context(), acct.ID, drill.ScopeAll())
	real := 0
	for _, t := range existing {
		if !t.IsSample {
			real++
		}
	}
	if h.enforceLimit(w, r, lc, "databases", real,
		account.EffectiveLimits(*acct).Databases) {
		return
	}

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
	cleanPath, err := h.resolveSourcePath(values.SourceURI)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, templates.TargetNewForm(lc, values, err.Error()))
		return
	}

	t, err := h.drills.CreateTarget(r.Context(), drill.Target{
		AccountID:       acct.ID,
		CreatedByUserID: u.ID,
		Name:            values.Name,
		SourceKind:      "postgres_dump_local",
		SourceURI:       cleanPath,
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
	t, err := h.drills.GetTarget(r.Context(), lc.Account.ID, id, h.databaseScope(r, lc))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	asserts, _ := h.drills.ListTargetAssertions(r.Context(), t.ID)
	render(w, r, templates.TargetDetail(lc, t, asserts, ""))
}

// targetScheduleUpdate sets a target's recurring drill cadence. The chosen
// cadence is validated against the account's plan — frequency is a paid axis.
func (h *Handlers) targetScheduleUpdate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := h.drills.GetTarget(r.Context(), lc.Account.ID, id, h.databaseScope(r, lc))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cadence := strings.TrimSpace(r.PostFormValue("cadence"))
	if !account.CadenceAllowedForAccount(*lc.Account, cadence) {
		asserts, _ := h.drills.ListTargetAssertions(r.Context(), t.ID)
		w.WriteHeader(http.StatusForbidden)
		render(w, r, templates.TargetDetail(lc, t, asserts,
			"That drill frequency is not included in your plan — see Pricing to upgrade."))
		return
	}
	var nextAt *time.Time
	if cadence != "off" {
		n := time.Now().UTC().Add(drill.CadenceInterval(cadence))
		nextAt = &n
	}
	if err := h.drills.SetTargetSchedule(r.Context(), lc.Account.ID, id, cadence, nextAt); err != nil {
		http.Error(w, "save schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "target.schedule_updated",
		TargetKind: "database_target", TargetID: id.String(),
		Metadata: map[string]any{"cadence": cadence},
	})
	http.Redirect(w, r, "/databases/"+id.String(), http.StatusSeeOther)
}

func (h *Handlers) assertionCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	u, acct := lc.User, lc.Account
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := h.drills.GetTarget(r.Context(), acct.ID, id, h.databaseScope(r, lc))
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
	// Gate the delete on the database being visible to this member's team
	// scope, so a member can't strip assertions off another team's database.
	if tid, perr := uuid.Parse(targetID); perr == nil {
		if _, gerr := h.drills.GetTarget(r.Context(), acct.ID, tid, h.databaseScope(r, lc)); gerr != nil {
			http.NotFound(w, r)
			return
		}
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
	scope := h.databaseScope(r, lc)
	ds, err := h.drills.ListDrills(r.Context(), lc.Account.ID, scope, 100)
	if err != nil {
		render(w, r, templates.DrillsErrorPage(lc, "Could not load drills."))
		return
	}
	targets, _ := h.drills.ListTargets(r.Context(), lc.Account.ID, scope)
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

	// Target must belong to the current account and be visible in the
	// caller's team scope (a member can't drill another team's database).
	target, err := h.drills.GetTarget(r.Context(), acct.ID, targetID,
		h.databaseScopeCtx(r.Context(), acct.ID, u.ID))
	if err != nil {
		http.Error(w, "target not found", http.StatusNotFound)
		return
	}

	// Drilling a real backup needs a paid plan (or an active trial); the
	// sample is always allowed.
	if !target.IsSample && h.blockFreeRealWeb(w, r) {
		return
	}
	// Enforce the per-day drill cap. All origins go through this — sample,
	// web, API, scheduler — so a trial hitting /drills in a loop cannot
	// starve a paying customer's scheduled drills.
	if err := enforceDrillQuota(r.Context(), h.drills, acct); err != nil {
		if qe, ok := asDrillQuotaError(err); ok {
			http.Error(w, qe.Error()+". Try again tomorrow or upgrade for a higher cap.", http.StatusTooManyRequests)
			return
		}
		h.logger().Warn("drill quota check failed — allowing", "err", err)
	}
	// Dump-size cap: stat the source now so a 500 GB dump doesn't burn the
	// 30-min restore timeout and get retried by River.
	if !target.IsSample {
		if err := enforceDumpSize(target.SourceKind, target.SourceURI, acct); err != nil {
			if de, ok := asDumpTooLargeError(err); ok {
				http.Error(w, de.Error()+".", http.StatusRequestEntityTooLarge)
				return
			}
			h.logger().Warn("dump size check failed — allowing", "err", err)
		}
	}

	drillID, reused, err := h.drills.CreateDrillIdempotent(r.Context(), acct.ID, u.ID, target.ID, idem)
	if err != nil {
		http.Error(w, "create drill: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !reused {
		if err := h.orch.EnqueueDrill(r.Context(), drillID, drillInsertOpts(acct)); err != nil {
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
	dr, err := h.drills.GetDrill(r.Context(), lc.Account.ID, drillID, h.databaseScope(r, lc))
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
		verify, _ = h.evidence.Verify(r.Context(), dr.ID, dr.AccountID, *dr.EvidencePath)
	}
	// Active + expired share links to render the "Share with an auditor"
	// section. A soft failure surfaces an empty list, not an error page.
	shareLinks, _ := h.share.ListForDrill(r.Context(), dr.ID)
	render(w, r, templates.DrillDetail(lc, dr, target, steps, ars, verify, shareLinks))
}

func (h *Handlers) drillStepsPartial(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), acct.ID, drillID, h.databaseScopeCtx(r.Context(), acct.ID, u.ID))
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
	dr, err := h.drills.GetDrill(r.Context(), acct.ID, drillID, h.databaseScopeCtx(r.Context(), acct.ID, u.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dr.EvidencePath == nil || *dr.EvidencePath == "" {
		http.Error(w, "evidence not yet generated", http.StatusNotFound)
		return
	}
	body, err := h.evidence.Read(r.Context(), acct.ID, *dr.EvidencePath)
	if err != nil {
		if errors.Is(err, evidence.ErrShredded) {
			http.Error(w, "evidence is no longer available", http.StatusGone)
			return
		}
		http.Error(w, "read evidence: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "evidence.downloaded",
		TargetKind: "drill", TargetID: dr.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+branding.Slug+`-`+dr.ID.String()+`.pdf"`)
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
