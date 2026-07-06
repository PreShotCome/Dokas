// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/dokaz/internal/auth"
	"github.com/preshotcome/dokaz/internal/branding"
	"github.com/preshotcome/dokaz/internal/drill"
	"github.com/preshotcome/dokaz/internal/evidence"
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
	// SourceHash is the hex SHA-256 of the dump we fetched and drilled.
	// Echoes the value embedded in the signed evidence PDF — a customer
	// re-hashing the dump they hold proves it's the exact bytes we ran.
	SourceHash *string   `json:"source_hash"`
	CreatedAt  time.Time `json:"created_at"`
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
		SourceHash:  d.SourceHash,
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

// apiStepLog is one row of GET /v1/drills/{id}/logs — the captured
// stdout+stderr of a step's subprocess (today: only restore).
type apiStepLog struct {
	Step         string  `json:"step"`
	Snippet      *string `json:"snippet"`
	SHA256       *string `json:"sha256"`    // hash of the FULL output, not the snippet
	Truncated    *bool   `json:"truncated"` // true when snippet < full output
	SnippetBytes int     `json:"snippet_bytes"`
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
	drills, err := h.drills.ListDrillsPage(r.Context(), acct.ID, drill.ScopeAll(), afterAt, afterID, limit+1)
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
	d, err := h.drills.GetDrill(r.Context(), acct.ID, id, drill.ScopeAll())
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
	target, err := h.drills.GetTarget(r.Context(), acct.ID, targetID, drill.ScopeAll())
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "database not found")
		return
	}

	// Drilling a real backup needs a paid plan (or an active trial); the
	// sample is always allowed.
	if !target.IsSample && h.v1BlockFreeReal(w, r) {
		return
	}
	// Per-day drill cap — same policy as the web + sample paths, since a
	// scripted API loop is the more common abuse vector.
	if err := enforceDrillQuota(r.Context(), h.drills, acct); err != nil {
		if qe, ok := asDrillQuotaError(err); ok {
			writeAPIError(w, http.StatusTooManyRequests, "drill_quota_exceeded", qe.Error())
			return
		}
		h.logger().Warn("drill quota check failed — allowing", "err", err)
	}
	// Dump-size cap. Real dumps only; sample skips.
	if !target.IsSample {
		if err := enforceDumpSize(target.SourceKind, target.SourceURI, acct); err != nil {
			if de, ok := asDumpTooLargeError(err); ok {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "dump_too_large", de.Error())
				return
			}
			h.logger().Warn("dump size check failed — allowing", "err", err)
		}
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
	if err := h.orch.EnqueueDrill(r.Context(), drillID, drillInsertOpts(acct)); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not enqueue drill")
		return
	}
	d, err := h.drills.GetDrill(r.Context(), acct.ID, drillID, drill.ScopeAll())
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
	d, err := h.drills.GetDrill(r.Context(), acct.ID, id, drill.ScopeAll())
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	if d.EvidencePath == nil || *d.EvidencePath == "" {
		writeAPIError(w, http.StatusNotFound, "no_evidence", "evidence not yet generated")
		return
	}
	body, err := h.evidence.Read(r.Context(), acct.ID, *d.EvidencePath)
	if err != nil {
		if errors.Is(err, evidence.ErrShredded) {
			writeAPIError(w, http.StatusGone, "evidence_unavailable", "evidence is no longer available")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not read evidence")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+branding.Slug+`-`+d.ID.String()+`.pdf"`)
	_, _ = w.Write(body)
}

// apiSignature is the detached signature shape served over the API: the
// fields a third-party verifier (dokaz-verify) needs to re-prove the
// PDF, plus a retention horizon so the customer knows the window.
type apiSignature struct {
	Algorithm   string    `json:"algorithm"`
	PublicKeyID string    `json:"public_key_id"`
	Value       string    `json:"value"`      // base64 Ed25519 signature
	PDFSHA256   string    `json:"pdf_sha256"` // hex digest the signature covers
	SignedAt    time.Time `json:"signed_at"`
	RetainUntil time.Time `json:"retain_until"`
}

// v1GetLogs returns the captured subprocess output for each step of a
// drill — today only the restore step produces output. Each row carries
// a snippet, the SHA-256 of the FULL output (not the snippet), and a
// truncated flag. The hash lets an auditor re-run the same tool against
// the same dump and verify the snippet is a true prefix.
func (h *Handlers) v1GetLogs(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	if _, err := h.drills.GetDrill(r.Context(), acct.ID, id, drill.ScopeAll()); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	steps, err := h.drills.ListSteps(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not load steps")
		return
	}
	logs := make([]apiStepLog, 0, len(steps))
	for _, s := range steps {
		if s.OutputSnippet == nil && s.OutputSHA256 == nil {
			continue
		}
		entry := apiStepLog{
			Step:      string(s.Name),
			Snippet:   s.OutputSnippet,
			SHA256:    s.OutputSHA256,
			Truncated: s.OutputTruncated,
		}
		if s.OutputSnippet != nil {
			entry.SnippetBytes = len(*s.OutputSnippet)
		}
		logs = append(logs, entry)
	}
	writeData(w, http.StatusOK, logs, nil)
}

// v1GetSignature returns the detached signature record for a drill as
// JSON. Pair the bytes with the evidence PDF (/drills/{id}/evidence)
// and feed both to `dokaz-verify` along with the published public
// key — the verifier needs no Dokaz-specific code.
func (h *Handlers) v1GetSignature(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	// Authorize: drill must belong to the authenticated account.
	if _, err := h.drills.GetDrill(r.Context(), acct.ID, id, drill.ScopeAll()); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "drill not found")
		return
	}
	rec, err := h.evidence.GetSignature(r.Context(), id)
	if errors.Is(err, evidence.ErrNoSignature) {
		writeAPIError(w, http.StatusNotFound, "no_signature", "signature not yet recorded for this drill")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not load signature")
		return
	}
	writeData(w, http.StatusOK, apiSignature{
		Algorithm:   rec.Algorithm,
		PublicKeyID: rec.PublicKeyID,
		Value:       rec.Value,
		PDFSHA256:   rec.PDFSHA256,
		SignedAt:    rec.SignedAt,
		RetainUntil: rec.RetainUntil,
	}, nil)
}
