package handlers

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/account"
	"github.com/preshotcome/dokaz/internal/assertions"
	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// sampleDump is a tiny PostgreSQL custom-format dump (one `public.events`
// table) embedded so a new user can download a known-good backup and run
// their first drill against it without finding their own database. It's a
// copy of testdata/fixtures/tiny.dump — the same artifact the e2e-smoke
// harness exercises, so the onboarding path and the CI path stay aligned.
//
//go:embed sample.dump
var sampleDump []byte

// onboardingSampleDump serves the embedded fixture as a file download. It's
// public — the whole point is to hand a frictionless first artifact to
// someone who hasn't connected a real source yet. Cached for a day; the
// fixture only changes when we redeploy.
func (h *Handlers) onboardingSampleDump(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="sample.dump"`)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(sampleDump)
}

// materializeSample writes the embedded sample dump to a file the drill runner
// can read (inside the configured source directory) and returns its absolute
// path. It is idempotent and self-healing: if a correctly-sized file is
// already present it returns cheaply, otherwise it (re)writes it atomically.
//
// This must be safe to call on every sample drill, not just at target
// creation: the source directory is ephemeral on the deployed host (only the
// evidence volume persists), so the file vanishes on each redeploy while the
// target row survives in the database. The embedded dump in the binary is the
// real source of truth — disk is just a cache the runner can stat.
func (h *Handlers) materializeSample() (string, error) {
	return MaterializeSample(h.sourceDir)
}

// MaterializeSample writes the embedded sample dump under sourceDir and
// returns its absolute path. Exported so the server can call it once at
// startup — that guarantees the dump is on the local disk of every machine
// that runs a drill worker, before any job runs, regardless of which process
// first created the sample target. See materializeSample's doc for the
// idempotent/self-healing/atomic-write behaviour.
func MaterializeSample(sourceDir string) (string, error) {
	dir, err := filepath.Abs(filepath.Join(sourceDir, "_"+branding.Slug+"_sample"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "sample.dump")
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(sampleDump)) {
		return path, nil // already present and intact
	}
	// Write to a temp file and rename so a concurrent drill never reads a
	// half-written dump.
	tmp, err := os.CreateTemp(dir, "sample-*.dump.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // harmless no-op once the rename succeeds
	if _, err := tmp.Write(sampleDump); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}

// ensureSampleTarget returns the account's sample (demo) target, creating it
// — with a default table_exists assertion on the fixture's public.events
// table — the first time. Idempotent and race-tolerant (a unique index
// guarantees one sample target per account).
func (h *Handlers) ensureSampleTarget(ctx context.Context, accountID, userID uuid.UUID) (drill.Target, error) {
	// Always re-materialize the embedded dump first — before checking for an
	// existing target. The target row persists in the DB but the file on disk
	// does not survive a redeploy, so writing only at creation time left the
	// first post-deploy drill failing with "no such file". The path is
	// deterministic, so a pre-existing target already points at it.
	path, err := h.materializeSample()
	if err != nil {
		return drill.Target{}, err
	}

	if t, err := h.drills.GetSampleTarget(ctx, accountID); err == nil {
		return t, nil
	} else if !errors.Is(err, drill.ErrNotFound) {
		return drill.Target{}, err
	}

	t, err := h.drills.CreateTarget(ctx, drill.Target{
		AccountID:       accountID,
		CreatedByUserID: userID,
		Name:            "Sample dataset (demo)",
		SourceKind:      "postgres_dump_local",
		SourceURI:       path,
		IsSample:        true,
	})
	if err != nil {
		// Lost a race to another request? The unique index rejected us — the
		// sample now exists, so return it.
		if t2, e2 := h.drills.GetSampleTarget(ctx, accountID); e2 == nil {
			return t2, nil
		}
		return drill.Target{}, err
	}

	cfg, _ := json.Marshal(map[string]any{"table": "events"})
	if _, err := h.drills.CreateAssertion(ctx, t.ID, assertions.KindTableExists, cfg); err != nil {
		h.logger().Warn("sample target: default assertion not added", "err", err)
	}
	return t, nil
}

// runSampleDrill is the free demo: it ensures the account's sample target
// exists and runs a drill against the built-in dataset. Allowed on every
// plan — it never touches a customer's own backup, so it carries no abuse
// risk and needs no paid plan.
func (h *Handlers) runSampleDrill(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	// Sample drills count against the per-day drill quota — otherwise a
	// trial account can hammer the sample endpoint in a loop and starve the
	// shared queue for every paying customer. The cap is intentionally
	// generous on paid plans; it's abuse-facing.
	if err := enforceDrillQuota(r.Context(), h.drills, lc.Account); err != nil {
		if qe, ok := asDrillQuotaError(err); ok {
			http.Error(w, qe.Error()+". Try again tomorrow or upgrade for a higher cap.", http.StatusTooManyRequests)
			return
		}
		h.logger().Warn("drill quota check failed — allowing", "err", err)
	}
	t, err := h.ensureSampleTarget(r.Context(), lc.Account.ID, lc.User.ID)
	if err != nil {
		h.logger().Error("ensure sample target", "account_id", lc.Account.ID, "err", err)
		http.Error(w, "could not prepare the sample drill", http.StatusInternalServerError)
		return
	}

	drillID, reused, err := h.drills.CreateDrillIdempotent(
		r.Context(), lc.Account.ID, lc.User.ID, t.ID, uuid.NewString())
	if err != nil {
		http.Error(w, "create drill: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !reused {
		if err := h.orch.EnqueueDrill(r.Context(), drillID, drillInsertOpts(lc.Account.Plan)); err != nil {
			http.Error(w, "enqueue: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "drill.sample_run",
		TargetKind: "drill", TargetID: drillID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/drills/"+drillID.String(), http.StatusSeeOther)
}

// canDrillReal reports whether an account may currently connect / drill its
// own real backup. Paid plans always can; an *active* trial can — the
// LimitsFor(PlanTrial).Databases cap (1) is what stops them stacking a
// production fleet on the trial. A lapsed trial cannot: the trial window is
// over, they need to subscribe.
func canDrillReal(a *account.Account) bool {
	if a == nil {
		return false
	}
	if account.IsPaid(a.Plan) {
		return true
	}
	return account.TrialActive(*a)
}

// blockFreeRealWeb renders the upgrade page and returns true when a browser
// request from a lapsed-trial or otherwise-ineligible account tries to add or
// drill a real backup. Active trials pass through; the sample dataset is
// always exempt (callers check IsSample before calling this).
func (h *Handlers) blockFreeRealWeb(w http.ResponseWriter, r *http.Request) bool {
	acct, ok := auth.CurrentAccountFromContext(r.Context())
	if !ok || canDrillReal(acct) {
		return false
	}
	w.WriteHeader(http.StatusPaymentRequired)
	render(w, r, templates.PaidRequired(h.layoutCtx(r)))
	return true
}

// v1BlockFreeReal writes a 402 plan_required and returns true when an API
// request from a lapsed-trial or otherwise-ineligible account tries to add or
// drill a real backup.
func (h *Handlers) v1BlockFreeReal(w http.ResponseWriter, r *http.Request) bool {
	acct, ok := auth.CurrentAccountFromContext(r.Context())
	if !ok || canDrillReal(acct) {
		return false
	}
	writeAPIError(w, http.StatusPaymentRequired, "plan_required",
		"trial has ended; subscribe to drill your own backups")
	return true
}
