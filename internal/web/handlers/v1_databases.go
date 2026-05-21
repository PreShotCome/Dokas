package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/preshotcome/anything/internal/auth"
	"github.com/preshotcome/anything/internal/drill"
)

// apiDatabase is the /v1 representation of a database target.
type apiDatabase struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	SourceKind string       `json:"source_kind"`
	SourceURI  string       `json:"source_uri"`
	Assertion  apiAssertion `json:"assertion"`
	CreatedAt  time.Time    `json:"created_at"`
}

type apiAssertion struct {
	Table   string `json:"table"`
	MinRows int    `json:"min_rows"`
}

func toAPIDatabase(t drill.Target) apiDatabase {
	return apiDatabase{
		ID:         t.ID.String(),
		Name:       t.Name,
		SourceKind: t.SourceKind,
		SourceURI:  t.SourceURI,
		Assertion:  apiAssertion{Table: t.AssertionTable, MinRows: t.AssertionMinRows},
		CreatedAt:  t.CreatedAt,
	}
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
	out := make([]apiDatabase, 0, len(targets))
	for _, t := range targets {
		out = append(out, toAPIDatabase(t))
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
	writeData(w, http.StatusOK, toAPIDatabase(t), nil)
}

type createDatabaseReq struct {
	Name             string `json:"name"`
	SourceURI        string `json:"source_uri"`
	AssertionTable   string `json:"assertion_table"`
	AssertionMinRows *int   `json:"assertion_min_rows"`
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
	req.AssertionTable = strings.TrimSpace(req.AssertionTable)
	if req.Name == "" || req.SourceURI == "" || req.AssertionTable == "" {
		writeAPIError(w, http.StatusBadRequest, "validation",
			"name, source_uri, and assertion_table are required")
		return
	}
	minRows := 1
	if req.AssertionMinRows != nil {
		if *req.AssertionMinRows < 0 {
			writeAPIError(w, http.StatusBadRequest, "validation",
				"assertion_min_rows must be non-negative")
			return
		}
		minRows = *req.AssertionMinRows
	}
	if _, err := os.Stat(req.SourceURI); err != nil {
		writeAPIError(w, http.StatusBadRequest, "validation",
			"source_uri not found on the server: "+req.SourceURI)
		return
	}

	t, err := h.drills.CreateTarget(r.Context(), drill.Target{
		AccountID:        acct.ID,
		CreatedByUserID:  key.CreatedByUserID,
		Name:             req.Name,
		SourceKind:       "postgres_dump_local",
		SourceURI:        req.SourceURI,
		AssertionTable:   req.AssertionTable,
		AssertionMinRows: minRows,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "could not create database")
		return
	}
	writeData(w, http.StatusCreated, toAPIDatabase(t), nil)
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
