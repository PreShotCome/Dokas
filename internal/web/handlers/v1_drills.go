package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
)

// apiDrill is the /v1 representation of a drill (list view).
type apiDrill struct {
	ID          string     `json:"id"`
	DatabaseID  string     `json:"database_id"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Error       *string    `json:"error"`
	HasEvidence bool       `json:"has_evidence"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toAPIDrill(d drill.Drill) apiDrill {
	return apiDrill{
		ID:          d.ID.String(),
		DatabaseID:  d.TargetID.String(),
		Status:      string(d.Status),
		StartedAt:   d.StartedAt,
		CompletedAt: d.CompletedAt,
		Error:       d.Error,
		HasEvidence: d.EvidencePath != nil && *d.EvidencePath != "",
		CreatedAt:   d.CreatedAt,
	}
}

// apiDrillDetail adds the step list and assertion results.
type apiDrillDetail struct {
	apiDrill
	Steps      []apiStep            `json:"steps"`
	Assertions []apiAssertionResult `json:"assertions"`
}

type apiStep struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Error       *string    `json:"error"`
}

type apiAssertionResult struct {
	Kind     string          `json:"kind"`
	Expected json.RawMessage `json:"expected"`
	Actual   json.RawMessage `json:"actual"`
	Passed   bool            `json:"passed"`
}

func (h *Handlers) v1ListDrills(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	limit, cursor, err := parsePageParams(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var afterAt *time.Time
	var afterID *uuid.UUID
	if cursor != nil {
		afterAt, afterID = &cursor.CreatedAt, &cursor.ID
	}
	drills, err := h.drills.ListDrillsPage(r.Context(), acct.ID, afterAt, afterID, limit+1)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not list drills")
		return
	}
	meta := listMeta{}
	if len(drills) > limit {
		last := drills[limit-1]
		meta.NextCursor = encodeCursor(pageCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		drills = drills[:limit]
	}
	out := make([]apiDrill, 0, len(drills))
	for _, d := range drills {
		out = append(out, toAPIDrill(d))
	}
	meta.Count = len(out)
	writeData(w, http.StatusOK, out, meta)
}

func (h *Handlers) v1GetDrill(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	d, err := h.drills.GetDrill(r.Context(), acct.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	steps, _ := h.drills.ListSteps(r.Context(), d.ID)
	ars, _ := h.drills.ListAssertions(r.Context(), d.ID)

	detail := apiDrillDetail{apiDrill: toAPIDrill(d)}
	for _, s := range steps {
		detail.Steps = append(detail.Steps, apiStep{
			Name: string(s.Name), Status: string(s.Status),
			StartedAt: s.StartedAt, CompletedAt: s.CompletedAt, Error: s.Error,
		})
	}
	for _, a := range ars {
		detail.Assertions = append(detail.Assertions, apiAssertionResult{
			Kind: a.Kind, Expected: json.RawMessage(a.Expected),
			Actual: json.RawMessage(a.Actual), Passed: a.Passed,
		})
	}
	writeData(w, http.StatusOK, detail, nil)
}

type createDrillReq struct {
	DatabaseID string `json:"database_id"`
}

func (h *Handlers) v1CreateDrill(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	key, _ := apiKeyFromContext(r.Context())

	var req createDrillReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	targetID, err := uuid.Parse(req.DatabaseID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation", "database_id must be a valid UUID")
		return
	}
	target, err := h.drills.GetTarget(r.Context(), acct.ID, targetID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "database not found")
		return
	}

	// Use the request's Idempotency-Key as the drill idempotency key too,
	// so even two concurrent POSTs with the same key collapse to one drill
	// (the v1Idempotency middleware stores responses but doesn't serialize
	// concurrent first-time requests).
	idemKey := r.Header.Get("Idempotency-Key")
	drillID, _, err := h.drills.CreateDrillIdempotent(r.Context(), acct.ID, key.CreatedByUserID, target.ID, idemKey)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not create drill")
		return
	}
	if err := h.orch.EnqueueDrill(r.Context(), drillID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not enqueue drill")
		return
	}
	d, err := h.drills.GetDrill(r.Context(), acct.ID, drillID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "drill created but not readable")
		return
	}
	writeData(w, http.StatusCreated, toAPIDrill(d), nil)
}

func (h *Handlers) v1GetEvidence(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	d, err := h.drills.GetDrill(r.Context(), acct.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	if d.EvidencePath == nil || *d.EvidencePath == "" {
		writeAPIError(w, http.StatusNotFound, "no_evidence", "evidence not yet generated")
		return
	}
	f, err := h.evidence.Open(r.Context(), *d.EvidencePath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not open evidence")
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not read evidence")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="restore-drill-`+d.ID.String()+`.pdf"`)
	_, _ = w.Write(body)
}
