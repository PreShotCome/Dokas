package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/audit"
	"github.com/preshotcome/anything/internal/heartbeat"
	"github.com/preshotcome/anything/internal/web/templates"
)

// --- /heartbeats (authenticated dashboard) ---

func (h *Handlers) heartbeatsList(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	hbs, err := h.heartbeats.List(r.Context(), lc.Account.ID)
	if err != nil {
		render(w, r, templates.HeartbeatsError(lc, "Could not load check-ins."))
		return
	}
	render(w, r, templates.HeartbeatsPage(lc, hbs, h.baseURL))
}

func (h *Handlers) heartbeatNewPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, templates.HeartbeatNewForm(h.layoutCtx(r), templates.HeartbeatFormValues{}, ""))
}

func (h *Handlers) heartbeatCreate(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	v := templates.HeartbeatFormValues{
		Name:        strings.TrimSpace(r.PostFormValue("name")),
		PeriodValue: strings.TrimSpace(r.PostFormValue("period_value")),
		PeriodUnit:  strings.TrimSpace(r.PostFormValue("period_unit")),
		GraceValue:  strings.TrimSpace(r.PostFormValue("grace_value")),
		GraceUnit:   strings.TrimSpace(r.PostFormValue("grace_unit")),
	}

	if v.Name == "" {
		h.renderHBFormError(w, r, lc, v, "A name is required.")
		return
	}
	periodSecs, ok := durationSeconds(v.PeriodValue, v.PeriodUnit)
	if !ok || periodSecs <= 0 {
		h.renderHBFormError(w, r, lc, v, "Enter a positive expected period.")
		return
	}
	graceSecs := 0
	if v.GraceValue != "" {
		graceSecs, ok = durationSeconds(v.GraceValue, v.GraceUnit)
		if !ok || graceSecs < 0 {
			h.renderHBFormError(w, r, lc, v, "Grace must be zero or more.")
			return
		}
	}

	hb, err := h.heartbeats.Create(r.Context(), heartbeat.Heartbeat{
		AccountID:       lc.Account.ID,
		CreatedByUserID: lc.User.ID,
		Name:            v.Name,
		PeriodSeconds:   periodSecs,
		GraceSeconds:    graceSecs,
	})
	if err != nil {
		http.Error(w, "create monitor: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "heartbeat.created",
		TargetKind: "heartbeat", TargetID: hb.ID.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
		Metadata: map[string]any{"name": hb.Name, "period_seconds": periodSecs},
	})
	http.Redirect(w, r, "/heartbeats/"+hb.ID.String(), http.StatusSeeOther)
}

func (h *Handlers) renderHBFormError(w http.ResponseWriter, r *http.Request, lc templates.LayoutCtx, v templates.HeartbeatFormValues, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	render(w, r, templates.HeartbeatNewForm(lc, v, msg))
}

func (h *Handlers) heartbeatDetail(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	hb, err := h.heartbeats.Get(r.Context(), lc.Account.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pings, _ := h.heartbeats.ListPings(r.Context(), hb.ID, 50)
	render(w, r, templates.HeartbeatDetail(lc, hb, pings, h.baseURL))
}

// heartbeatStatusPartial is the HTMX-polled status fragment on the detail page.
func (h *Handlers) heartbeatStatusPartial(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	hb, err := h.heartbeats.Get(r.Context(), lc.Account.ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, templates.HeartbeatStatusCard(hb))
}

func (h *Handlers) heartbeatPause(w http.ResponseWriter, r *http.Request) {
	h.heartbeatStateChange(w, r, "heartbeat.paused", h.heartbeats.Pause)
}

func (h *Handlers) heartbeatResume(w http.ResponseWriter, r *http.Request) {
	h.heartbeatStateChange(w, r, "heartbeat.resumed", h.heartbeats.Resume)
}

func (h *Handlers) heartbeatDelete(w http.ResponseWriter, r *http.Request) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.heartbeats.Delete(r.Context(), lc.Account.ID, id); err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "delete: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: "heartbeat.deleted",
		TargetKind: "heartbeat", TargetID: id.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/heartbeats", http.StatusSeeOther)
}

// heartbeatStateChange factors the pause/resume handlers: both load by id,
// run a store mutation, audit, and redirect back to the detail page.
func (h *Handlers) heartbeatStateChange(w http.ResponseWriter, r *http.Request, action string,
	fn func(ctx context.Context, accountID, id uuid.UUID) error) {
	lc := h.layoutCtx(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := fn(r.Context(), lc.Account.ID, id); err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "update: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.audit.Record(r.Context(), audit.Event{
		AccountID: &lc.Account.ID, ActorID: &lc.User.ID, Action: action,
		TargetKind: "heartbeat", TargetID: id.String(),
		IP: audit.ClientIP(r), UserAgent: r.UserAgent(),
	})
	http.Redirect(w, r, "/heartbeats/"+id.String(), http.StatusSeeOther)
}

// --- /ping/{token} (public, unauthenticated ingest) ---

// ping is the public check-in endpoint. The token in the path is the only
// credential — it identifies the monitor and authorises the ping. CSRF-exempt
// (see csrf.New in main) and rate-limited per source IP. Both GET and POST are
// accepted so a bare `curl` works.
func (h *Handlers) ping(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	kind := heartbeat.KindPing
	switch chi.URLParam(r, "action") {
	case "":
		kind = heartbeat.KindPing
	case "fail":
		kind = heartbeat.KindFail
	case "start":
		kind = heartbeat.KindStart
	default:
		http.NotFound(w, r)
		return
	}

	ip, ua := audit.FromRequest(r)
	hb, transitioned, err := h.heartbeats.RecordPing(r.Context(), token, kind, ip, ua)
	if err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			http.Error(w, "unknown monitor", http.StatusNotFound)
			return
		}
		http.Error(w, "ping failed", http.StatusInternalServerError)
		return
	}

	// Only a real up/down edge is worth a webhook + audit row; routine pings
	// that keep an already-up monitor up are silent.
	if transitioned {
		event := heartbeat.EventUp
		if hb.Status == heartbeat.StatusDown {
			event = heartbeat.EventDown
		}
		data := heartbeat.EventData(hb)
		if h.webhookDispatch != nil {
			_ = h.webhookDispatch.Dispatch(r.Context(), hb.AccountID, event, data)
		}
		acct := hb.AccountID
		_ = h.audit.Record(r.Context(), audit.Event{
			AccountID: &acct, Action: event,
			TargetKind: "heartbeat", TargetID: hb.ID.String(),
			IP: ip, UserAgent: ua, Metadata: data,
		})
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// durationSeconds converts a numeric value + unit ("minutes"/"hours"/"days")
// into seconds. Returns ok=false on a malformed value, an unknown unit, or a
// result beyond a year (a sane upper bound that also guards against overflow).
func durationSeconds(value, unit string) (int, bool) {
	n, err := atoiInRange(value, 0, 1_000_000)
	if err != nil {
		return 0, false
	}
	var per int
	switch unit {
	case "minutes":
		per = 60
	case "hours":
		per = 3600
	case "days":
		per = 86400
	default:
		return 0, false
	}
	secs := n * per
	if secs > 366*86400 {
		return 0, false
	}
	return secs, true
}
