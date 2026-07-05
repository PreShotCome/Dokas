package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/audit"
	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/evidence"
	"github.com/preshotcome/dokaz/internal/sharelink"
	"github.com/preshotcome/dokaz/internal/web/templates"
)

// sharePage is the public no-account view of a drill: hero (target + status),
// four stat tiles, pipeline, assertions, and the mono signature receipt. No
// nav chrome, no site cookies — the auditor sees only what they were sent.
func (h *Handlers) sharePage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link, err := h.share.Resolve(r.Context(), token)
	if err != nil {
		if errors.Is(err, sharelink.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			render(w, r, templates.ShareGone(h.layoutCtx(r)))
			return
		}
		http.Error(w, "share lookup failed", http.StatusInternalServerError)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), link.AccountID, link.DrillID, drill.ScopeAll())
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, templates.ShareGone(h.layoutCtx(r)))
		return
	}
	target, err := h.drills.GetTarget(r.Context(), link.AccountID, dr.TargetID, drill.ScopeAll())
	if err != nil {
		http.Error(w, "share resolve failed", http.StatusInternalServerError)
		return
	}
	steps, _ := h.drills.ListSteps(r.Context(), dr.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), dr.ID)

	var verify evidence.VerifyResult
	if h.evidence != nil {
		verify, _ = h.evidence.Verify(r.Context(), dr.ID, link.AccountID, "")
	}

	render(w, r, templates.SharePage(h.layoutCtx(r), dr, target, steps, ars, verify, link.Label, link.ExpiresAt, token))
}

// shareDownload streams the evidence PDF for a valid share token. Same auth
// as the page: possession of the token is authority.
func (h *Handlers) shareDownload(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link, err := h.share.Resolve(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dr, err := h.drills.GetDrill(r.Context(), link.AccountID, link.DrillID, drill.ScopeAll())
	if err != nil || dr.EvidencePath == nil || *dr.EvidencePath == "" {
		http.NotFound(w, r)
		return
	}
	body, err := h.evidence.Read(r.Context(), link.AccountID, *dr.EvidencePath)
	if err != nil {
		http.Error(w, "evidence unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+branding.Slug+`-`+dr.ID.String()+`.pdf"`)
	_, _ = w.Write(body)
}

// shareCreate is the account-scoped POST that mints a new share link for a
// drill and returns the one-time token to the operator. Auditable.
func (h *Handlers) shareCreate(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid drill id", http.StatusBadRequest)
		return
	}
	// Ownership check: the drill must belong to the current account AND be
	// visible in the caller's team scope — a member must not mint a public
	// share link for another team's evidence (issue #29).
	dr, err := h.drills.GetDrill(r.Context(), acct.ID, drillID, h.databaseScopeCtx(r.Context(), acct.ID, u.ID))
	if err != nil || dr.Status != drill.StatusSucceeded {
		http.Error(w, "drill not shareable (only successful drills can be shared)", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(r.PostFormValue("label"))
	// TTL defaults to sharelink.DefaultTTL (30 days). Callers can request a
	// shorter horizon via ttl_days=N (1..90).
	ttl := sharelink.DefaultTTL
	if v := strings.TrimSpace(r.PostFormValue("ttl_days")); v != "" {
		if d, err := parseTTLDays(v); err == nil {
			ttl = d
		}
	}
	m, err := h.share.Create(r.Context(), dr.ID, acct.ID, u.ID, label, ttl)
	if err != nil {
		http.Error(w, "could not mint share link", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "drill.share_created",
		TargetKind: "drill", TargetID: dr.ID.String(),
		Metadata: map[string]any{"link_id": m.Link.ID.String(), "label": label, "expires_at": m.Link.ExpiresAt},
		IP:       audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	// Absolute share URL — this is what the operator copies + sends.
	shareURL := h.baseURL + "/s/" + m.Token
	render(w, r, templates.ShareCreated(h.layoutCtx(r), dr, m.Link, shareURL))
}

// shareRevoke revokes a share link.
func (h *Handlers) shareRevoke(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	linkID, err := uuid.Parse(chi.URLParam(r, "link_id"))
	if err != nil {
		http.Error(w, "invalid link id", http.StatusBadRequest)
		return
	}
	drillID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid drill id", http.StatusBadRequest)
		return
	}
	if _, err := h.drills.GetDrill(r.Context(), acct.ID, drillID, h.databaseScopeCtx(r.Context(), acct.ID, u.ID)); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.share.Revoke(r.Context(), linkID); err != nil {
		http.Error(w, "could not revoke", http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &acct.ID, ActorID: &u.ID, Action: "drill.share_revoked",
		TargetKind: "drill", TargetID: drillID.String(),
		Metadata: map[string]any{"link_id": linkID.String()},
		IP:       audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/drills/"+drillID.String(), http.StatusSeeOther)
}

// parseTTLDays clamps to 1..90 days.
func parseTTLDays(s string) (time.Duration, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
		if n > 90 {
			n = 90
		}
	}
	if n < 1 {
		n = 1
	}
	return time.Duration(n) * 24 * time.Hour, nil
}
