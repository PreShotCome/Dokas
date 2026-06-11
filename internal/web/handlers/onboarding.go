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

	"github.com/preshotcome/vesta/internal/account"
	"github.com/preshotcome/vesta/internal/assertions"
	"github.com/preshotcome/vesta/internal/audit"
	"github.com/preshotcome/vesta/internal/auth"
	"github.com/preshotcome/vesta/internal/branding"
	"github.com/preshotcome/vesta/internal/drill"
	"github.com/preshotcome/vesta/internal/web/templates"
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
// path. Idempotent: it only writes the file once.
func (h *Handlers) materializeSample() (string, error) {
	dir, err := filepath.Abs(filepath.Join(h.sourceDir, "_"+branding.Slug+"_sample"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "sample.dump")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.WriteFile(path, sampleDump, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ensureSampleTarget returns the account's sample (demo) target, creating it
// — with a default table_exists assertion on the fixture's public.events
// table — the first time. Idempotent and race-tolerant (a unique index
// guarantees one sample target per account).
func (h *Handlers) ensureSampleTarget(ctx context.Context, accountID, userID uuid.UUID) (drill.Target, error) {
	if t, err := h.drills.GetSampleTarget(ctx, accountID); err == nil {
		return t, nil
	} else if !errors.Is(err, drill.ErrNotFound) {
		return drill.Target{}, err
	}

	path, err := h.materializeSample()
	if err != nil {
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
		if err := h.orch.EnqueueDrill(r.Context(), drillID); err != nil {
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

// blockFreeRealWeb renders the upgrade page and returns true when a browser
// request from a free/trial account tries to add or drill a real backup.
// The sample dataset is exempt — callers check IsSample before calling this.
func (h *Handlers) blockFreeRealWeb(w http.ResponseWriter, r *http.Request) bool {
	if acct, ok := auth.CurrentAccountFromContext(r.Context()); ok && !account.IsPaid(acct.Plan) {
		w.WriteHeader(http.StatusPaymentRequired)
		render(w, r, templates.PaidRequired(h.layoutCtx(r)))
		return true
	}
	return false
}

// v1BlockFreeReal writes a 402 plan_required and returns true when an API
// request from a free/trial account tries to add or drill a real backup.
func (h *Handlers) v1BlockFreeReal(w http.ResponseWriter, r *http.Request) bool {
	if acct, ok := auth.CurrentAccountFromContext(r.Context()); ok && !account.IsPaid(acct.Plan) {
		writeAPIError(w, http.StatusPaymentRequired, "plan_required",
			"free accounts can drill the sample dataset only; subscribe to drill your own backups")
		return true
	}
	return false
}
