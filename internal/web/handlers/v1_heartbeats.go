package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/heartbeat"
)

// apiHeartbeat is the /v1 (and /mobile) representation of a backup check-in
// monitor. overdue is computed: the monitor is past its deadline but the
// minute sweeper hasn't flipped it to "down" yet.
type apiHeartbeat struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	Status        string     `json:"status"`
	Overdue       bool       `json:"overdue"`
	PeriodSeconds int        `json:"period_seconds"`
	GraceSeconds  int        `json:"grace_seconds"`
	LastPingAt    *time.Time `json:"last_ping_at"`
	ExpectedBy    *time.Time `json:"expected_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toAPIHeartbeat(h heartbeat.Heartbeat, now time.Time) apiHeartbeat {
	return apiHeartbeat{
		ID:            h.ID.String(),
		Name:          h.Name,
		Slug:          h.Slug,
		Status:        string(h.Status),
		Overdue:       h.Overdue(now),
		PeriodSeconds: h.PeriodSeconds,
		GraceSeconds:  h.GraceSeconds,
		LastPingAt:    h.LastPingAt,
		ExpectedBy:    h.ExpectedBy,
		CreatedAt:     h.CreatedAt,
	}
}

// v1ListHeartbeats returns the account's backup check-in monitors. Accounts
// hold few monitors, so the full set is returned (no pagination).
func (h *Handlers) v1ListHeartbeats(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	hbs, err := h.heartbeats.List(r.Context(), acct.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not list heartbeats")
		return
	}
	now := time.Now()
	out := make([]apiHeartbeat, 0, len(hbs))
	for _, hb := range hbs {
		out = append(out, toAPIHeartbeat(hb, now))
	}
	writeData(w, http.StatusOK, out, listMeta{Count: len(out)})
}

// v1GetHeartbeat returns one monitor by id.
func (h *Handlers) v1GetHeartbeat(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "heartbeat not found")
		return
	}
	hb, err := h.heartbeats.Get(r.Context(), acct.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "heartbeat not found")
		return
	}
	writeData(w, http.StatusOK, toAPIHeartbeat(hb, time.Now()), nil)
}
