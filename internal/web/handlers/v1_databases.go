package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/assertions"
	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
)

// apiDatabase is the /v1 representation of a database target.
type apiDatabase struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	SourceKind string         `json:"source_kind"`
	SourceURI  string         `json:"source_uri"`
	Assertions []apiAssertion `json:"assertions"`
	CreatedAt  time.Time      `json:"created_at"`
}

type apiAssertion struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Config map[string]any `json:"config"`
}

func toAPIAssertion(a drill.Assertion) apiAssertion {
	var cfg map[string]any
	_ = json.Unmarshal(a.Config, &cfg)
	return apiAssertion{ID: a.ID.String(), Kind: a.Kind, Config: cfg}
}

func toAPIDatabase(t drill.Target, asserts []drill.Assertion) apiDatabase {
	out := apiDatabase{
		ID:         t.ID.String(),
		Name:       t.Name,
		SourceKind: t.SourceKind,
		SourceURI:  t.SourceURI,
		Assertions: make([]apiAssertion, 0, len(asserts)),
		CreatedAt:  t.CreatedAt,
	}
	for _, a := range asserts {
		out.Assertions = append(out.Assertions, toAPIAssertion(a))
	}
	return out
}

func (h *Handlers) v1ListDatabases(w http.ResponseWriter, r *http.Request) {
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
	// Fetch limit+1 to detect whether another page exists.
	targets, err := h.drills.ListTargetsPage(r.Context(), acct.ID, afterAt, afterID, limit+1)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not list databases")
		return
	}

	meta := listMeta{}
	if len(targets) > limit {
		last := targets[limit-1]
		meta.NextCursor = encodeCursor(pageCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		targets = targets[:limit]
	}
	ids := make([]uuid.UUID, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	assertsByTarget, err := h.drills.ListAssertionsForTargets(r.Context(), ids)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not list databases")
		return
	}
	out := make([]apiDatabase, 0, len(targets))
	for _, t := range targets {
		out = append(out, toAPIDatabase(t, assertsByTarget[t.ID]))
	}
	meta.Count = len(out)
	writeData(w, http.StatusOK, out, meta)
}

func (h *Handlers) v1GetDatabase(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "database not found")
		return
	}
	t, err := h.drills.GetTarget(r.Context(), acct.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "database not found")
		return
	}
	asserts, err := h.drills.ListTargetAssertions(r.Context(), t.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not load database")
		return
	}
	writeData(w, http.StatusOK, toAPIDatabase(t, asserts), nil)
}

type createDatabaseReq struct {
	Name       string               `json:"name"`
	SourceURI  string               `json:"source_uri"`
	Assertions []createAssertionReq `json:"assertions"`
}

type createAssertionReq struct {
	Kind   string         `json:"kind"`
	Config map[string]any `json:"config"`
}

func (h *Handlers) v1CreateDatabase(w http.ResponseWriter, r *http.Request) {
	acct, _ := auth.CurrentAccountFromContext(r.Context())
	key, _ := apiKeyFromContext(r.Context())

	var req createDatabaseReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.SourceURI = strings.TrimSpace(req.SourceURI)
	if req.Name == "" || req.SourceURI == "" {
		writeAPIError(w, http.StatusBadRequest, "validation",
			"name and source_uri are required")
		return
	}
	// Validate every assertion up front so a bad one doesn't leave a target
	// with a half-applied assertion set.
	for i, a := range req.Assertions {
		if err := assertions.ValidateConfig(a.Kind, a.Config); err != nil {
			writeAPIError(w, http.StatusBadRequest, "validation",
				fmt.Sprintf("assertions[%d]: %s", i, err.Error()))
			return
		}
	}
	if _, err := os.Stat(req.SourceURI); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation",
			"source_uri not found on the server: "+req.SourceURI)
		return
	}

	t, err := h.drills.CreateTarget(r.Context(), drill.Target{
		AccountID:       acct.ID,
		CreatedByUserID: key.CreatedByUserID,
		Name:            req.Name,
		SourceKind:      "postgres_dump_local",
		SourceURI:       req.SourceURI,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not create database")
		return
	}
	stored := make([]drill.Assertion, 0, len(req.Assertions))
	for _, a := range req.Assertions {
		raw, _ := json.Marshal(a.Config)
		created, err := h.drills.CreateAssertion(r.Context(), t.ID, a.Kind, raw)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal", "could not attach assertion")
			return
		}
		stored = append(stored, created)
	}
	writeData(w, http.StatusCreated, toAPIDatabase(t, stored), nil)
}

// decodeJSONBody strictly decodes a JSON request body.
func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}
